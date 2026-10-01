package bundle

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aiveto/veto/config"
	"gopkg.in/yaml.v3"
)

const maxBundleBytes int64 = 64 << 20

// Loaded is a directory of contracts, relations, and check cases, or a zip of that directory.
// Close removes a zip that was extracted for this process.
type Loaded struct {
	Config config.File

	temp   string
	closed bool
}

var (
	mu   sync.Mutex
	held []*Loaded
)

// Close removes an extracted archive. A directory bundle stays where it is.
func (l *Loaded) Close() {
	if l == nil {
		return
	}
	mu.Lock()
	if l.closed {
		mu.Unlock()
		return
	}
	l.closed = true
	temp := l.temp
	held = dropHeld(held, l)
	mu.Unlock()
	if temp != "" {
		os.RemoveAll(temp)
	}
}

// Release removes every extracted archive still held.
func Release() {
	mu.Lock()
	all := append([]*Loaded(nil), held...)
	held = nil
	mu.Unlock()
	for _, l := range all {
		l.Close()
	}
}

func hold(l *Loaded) {
	if l == nil || l.temp == "" {
		return
	}
	mu.Lock()
	held = append(held, l)
	mu.Unlock()
}

func dropHeld(all []*Loaded, l *Loaded) []*Loaded {
	out := make([]*Loaded, 0, len(all))
	for _, item := range all {
		if item != l {
			out = append(out, item)
		}
	}
	return out
}

// Load reads a bundle directory or zip the same way config.Load reads a file that points at those paths.
func Load(bundlePath string) (*Loaded, error) {
	loaded, err := load(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("load bundle: %w", err)
	}
	return loaded, nil
}

func load(bundlePath string) (*Loaded, error) {
	root, temp, err := open(bundlePath)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Loaded, error) {
		if temp != "" {
			os.RemoveAll(temp)
		}
		return nil, err
	}
	located, err := locate(root)
	if err != nil {
		return fail(err)
	}
	manifest, err := findManifest(located)
	if err != nil {
		return fail(err)
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		return fail(fmt.Errorf("read bundle: %w", err))
	}
	// config.Load drops keys it does not know, including client_secret.
	if err := rejectSecrets(data); err != nil {
		return fail(err)
	}
	cfg, err := config.Load(manifest)
	if err != nil {
		return fail(err)
	}
	stripDeployment(&cfg)
	if err := scanExtra(located, manifest, cfg.Contracts); err != nil {
		return fail(err)
	}
	loaded := &Loaded{Config: cfg, temp: temp}
	hold(loaded)
	return loaded, nil
}

func open(bundlePath string) (string, string, error) {
	info, err := os.Stat(bundlePath)
	if err != nil {
		return "", "", fmt.Errorf("read bundle: %w", err)
	}
	if info.IsDir() {
		abs, err := filepath.Abs(bundlePath)
		if err != nil {
			return "", "", err
		}
		return abs, "", nil
	}
	if !isZip(bundlePath) {
		return "", "", fmt.Errorf("bundle is a directory or a zip archive")
	}
	dest, err := extractZip(bundlePath)
	if err != nil {
		return "", "", err
	}
	return dest, dest, nil
}

func isZip(bundlePath string) bool {
	if strings.EqualFold(filepath.Ext(bundlePath), ".zip") {
		return true
	}
	f, err := os.Open(bundlePath)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	return magic[0] == 'P' && magic[1] == 'K'
}

func extractZip(src string) (string, error) {
	r, err := zip.OpenReader(src)
	if err != nil {
		return "", fmt.Errorf("read bundle: %w", err)
	}
	defer r.Close()
	if len(r.File) > 4096 {
		return "", fmt.Errorf("bundle archive is too large")
	}
	dest, err := os.MkdirTemp("", "veto-bundle-")
	if err != nil {
		return "", err
	}
	var total int64
	for _, f := range r.File {
		target, skip, err := zipTarget(dest, f.Name)
		if err != nil {
			os.RemoveAll(dest)
			return "", err
		}
		if skip {
			continue
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(f.Name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				os.RemoveAll(dest)
				return "", err
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			os.RemoveAll(dest)
			return "", fmt.Errorf("bundle archive contains a symlink")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			os.RemoveAll(dest)
			return "", err
		}
		n, err := writeZipFile(f, target, total)
		if err != nil {
			os.RemoveAll(dest)
			return "", err
		}
		total += n
	}
	return dest, nil
}

func zipTarget(dest, name string) (string, bool, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean("/" + name)
	if clean == "/" || clean == "." {
		return "", true, nil
	}
	if clean == "/.." || strings.HasPrefix(clean, "/../") {
		return "", false, fmt.Errorf("bundle path %q escapes the archive", name)
	}
	rel := strings.TrimPrefix(clean, "/")
	target := filepath.Join(dest, filepath.FromSlash(rel))
	back, err := filepath.Rel(dest, target)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", false, fmt.Errorf("bundle path %q escapes the archive", name)
	}
	return target, false, nil
}

func writeZipFile(f *zip.File, target string, total int64) (int64, error) {
	if int64(f.UncompressedSize64) > maxBundleBytes || total+int64(f.UncompressedSize64) > maxBundleBytes {
		return 0, fmt.Errorf("bundle archive is too large")
	}
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(out, io.LimitReader(rc, maxBundleBytes-total+1))
	closeErr := out.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if total+n > maxBundleBytes {
		return 0, fmt.Errorf("bundle archive is too large")
	}
	return n, nil
}

func locate(root string) (string, error) {
	if _, err := findManifest(root); err == nil {
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		name := e.Name()
		if name == "__MACOSX" || name == ".DS_Store" {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, name)
		}
	}
	if len(dirs) == 1 {
		child := filepath.Join(root, dirs[0])
		if _, err := findManifest(child); err == nil {
			return child, nil
		}
	}
	return "", fmt.Errorf("bundle manifest not found")
}

func findManifest(root string) (string, error) {
	for _, name := range []string{"bundle.yaml", "bundle.yml", "veto.yaml", "veto.yml"} {
		candidate := filepath.Join(root, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("bundle manifest not found")
}

func stripDeployment(cfg *config.File) {
	cfg.Auth = nil
	cfg.TokenDir = ""
	cfg.Server = ""
	cfg.Callers = nil
	cfg.Bundle = ""
}

func scanExtra(root, manifest string, contracts []string) error {
	skip := map[string]bool{manifest: true}
	for _, name := range contracts {
		skip[name] = true
	}
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isYAML(d.Name()) || skip[p] {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := rejectSecrets(data); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		return nil
	})
}

func isYAML(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml":
		return true
	default:
		return false
	}
}

var secretKeys = map[string]string{
	"client_secret": "client_secret",
	"clientsecret":  "client_secret",
	"token_url":     "token_url",
	"tokenurl":      "token_url",
	"access_token":  "access_token",
	"accesstoken":   "access_token",
	"refresh_token": "refresh_token",
	"refreshtoken":  "refresh_token",
	"password":      "password",
	"api_key":       "api_key",
	"apikey":        "api_key",
	"private_key":   "private_key",
	"privatekey":    "private_key",
	"credentials":   "credentials",
	"credential":    "credential",
	"secret":        "secret",
	"id_token":      "id_token",
	"idtoken":       "id_token",
}

var baseURLKeys = map[string]struct{}{
	"server":   {},
	"base_url": {},
	"baseurl":  {},
}

func rejectSecrets(data []byte) error {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse bundle: %w", err)
	}
	return walkSecrets(&doc)
}

func walkSecrets(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			if err := walkSecrets(child); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := normKey(n.Content[i].Value)
			if name, ok := secretKeys[key]; ok {
				return fmt.Errorf("bundle contains %s", name)
			}
			if _, ok := baseURLKeys[key]; ok {
				return fmt.Errorf("bundle contains an environment base URL")
			}
			if err := walkSecrets(n.Content[i+1]); err != nil {
				return err
			}
		}
	case yaml.AliasNode:
		return walkSecrets(n.Alias)
	}
	return nil
}

func normKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", "_"))
}
