// Package bundle loads contracts, relations, and check cases. The deployment stays outside.
package bundle

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/internal/yamlfile"
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
func (l *Loaded) Close() error {
	if l == nil {
		return nil
	}
	mu.Lock()
	if l.closed {
		mu.Unlock()
		return nil
	}
	l.closed = true
	temp := l.temp
	held = dropHeld(held, l)
	mu.Unlock()
	if temp == "" {
		return nil
	}
	if err := os.RemoveAll(temp); err != nil {
		return fmt.Errorf("remove bundle: %w", err)
	}
	return nil
}

// Release removes every extracted archive still held.
func Release() error {
	mu.Lock()
	all := append([]*Loaded(nil), held...)
	held = nil
	mu.Unlock()
	var err error
	for _, l := range all {
		err = errors.Join(err, l.Close())
	}
	return err
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
			if rerr := os.RemoveAll(temp); rerr != nil {
				err = errors.Join(err, fmt.Errorf("remove bundle temp: %w", rerr))
			}
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
	if err := rejectSecrets(data); err != nil {
		return fail(err)
	}
	doc, err := parseManifest(data)
	if err != nil {
		return fail(err)
	}
	cfg, err := materialize(located, doc)
	if err != nil {
		return fail(err)
	}
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
		return "", "", errors.New("bundle is a directory or a zip archive")
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
	var magic [4]byte
	_, readErr := io.ReadFull(f, magic[:])
	closeErr := f.Close()
	if readErr != nil || closeErr != nil {
		return false
	}
	return magic[0] == 'P' && magic[1] == 'K'
}

func extractZip(src string) (dest string, err error) {
	r, err := zip.OpenReader(src)
	if err != nil {
		return "", fmt.Errorf("read bundle: %w", err)
	}
	defer func() {
		cerr := r.Close()
		if err != nil || cerr == nil {
			return
		}
		err = fmt.Errorf("read bundle: %w", cerr)
		if dest == "" {
			return
		}
		if rerr := os.RemoveAll(dest); rerr != nil {
			err = errors.Join(err, fmt.Errorf("remove bundle temp: %w", rerr))
		}
	}()
	if len(r.File) > 4096 {
		return "", errors.New("bundle archive is too large")
	}
	dest, err = os.MkdirTemp("", "veto-bundle-")
	if err != nil {
		return "", err
	}
	fail := func(cause error) (string, error) {
		if rerr := os.RemoveAll(dest); rerr != nil {
			cause = errors.Join(cause, fmt.Errorf("remove bundle temp: %w", rerr))
		}
		return "", cause
	}
	var total int64
	for _, f := range r.File {
		target, skip, err := zipTarget(dest, f.Name)
		if err != nil {
			return fail(err)
		}
		if skip {
			continue
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(f.Name, "/") {
			if err := os.MkdirAll(target, 0o750); err != nil {
				return fail(err)
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fail(errors.New("bundle archive contains a symlink"))
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return fail(err)
		}
		n, err := writeZipFile(f, target, total)
		if err != nil {
			return fail(err)
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

func writeZipFile(f *zip.File, target string, total int64) (n int64, err error) {
	size := f.UncompressedSize64
	limit := uint64(maxBundleBytes)
	if total < 0 || size > limit || uint64(total) > limit-size {
		return 0, errors.New("bundle archive is too large")
	}
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := rc.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
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
		return 0, errors.New("bundle archive is too large")
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
	return "", errors.New("bundle manifest not found")
}

func findManifest(root string) (string, error) {
	for _, name := range []string{"bundle.yaml", "bundle.yml", "veto.yaml", "veto.yml"} {
		candidate := filepath.Join(root, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", errors.New("bundle manifest not found")
}

type manifest struct {
	Contracts     []string `yaml:"contracts"`
	RelationsFile string   `yaml:"relations_file"`
	Cases         []string `yaml:"cases"`
}

func parseManifest(data []byte) (manifest, error) {
	if err := yamlfile.Prepare(data); err != nil {
		return manifest{}, fmt.Errorf("parse bundle: %w", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return manifest{}, fmt.Errorf("parse bundle: %w", err)
	}
	if err := manifestFields(&node); err != nil {
		return manifest{}, err
	}
	var doc manifest
	if err := node.Decode(&doc); err != nil {
		return manifest{}, fmt.Errorf("parse bundle: %w", err)
	}
	return doc, nil
}

func manifestFields(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = n.Content[0]
	}
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return errors.New("bundle manifest must be a mapping")
	}
	known := map[string]bool{"contracts": true, "relations_file": true, "cases": true}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		if !known[key] {
			return fmt.Errorf("unsupported bundle field %q", key)
		}
	}
	return nil
}

func materialize(root string, doc manifest) (config.File, error) {
	cfg := config.Defaults()
	var err error
	cfg.Contracts, err = confineAll(root, doc.Contracts)
	if err != nil {
		return config.File{}, err
	}
	cfg.RelationsFile, err = confineOne(root, doc.RelationsFile)
	if err != nil {
		return config.File{}, err
	}
	cfg.Cases, err = confineAll(root, doc.Cases)
	if err != nil {
		return config.File{}, err
	}
	return cfg, nil
}

func confineAll(root string, refs []string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]string, len(refs))
	for i, ref := range refs {
		abs, err := confine(root, ref)
		if err != nil {
			return nil, err
		}
		out[i] = abs
	}
	return out, nil
}

func confineOne(root, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", nil
	}
	return confine(root, ref)
}

func confine(root, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", errors.New("bundle path is empty")
	}
	if filepath.IsAbs(ref) {
		return "", fmt.Errorf("bundle path %q escapes the bundle", ref)
	}
	rel := filepath.Clean(ref)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("bundle path %q escapes the bundle", ref)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = resolved
	}
	cur := rootAbs
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(cur, part)
		info, err := os.Lstat(next)
		if err != nil {
			if os.IsNotExist(err) {
				cur = next
				continue
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(next)
			if err != nil {
				return "", err
			}
			if !inside(rootAbs, resolved) {
				return "", fmt.Errorf("bundle path %q escapes the bundle", ref)
			}
			cur = resolved
			continue
		}
		cur = next
	}
	if !inside(rootAbs, cur) {
		return "", fmt.Errorf("bundle path %q escapes the bundle", ref)
	}
	info, err := os.Lstat(cur)
	if err == nil && info.IsDir() {
		if err := walkLinks(rootAbs, cur); err != nil {
			return "", err
		}
	}
	return cur, nil
}

func walkLinks(root, dir string) error {
	return filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			return err
		}
		if !inside(root, resolved) {
			return fmt.Errorf("bundle path %q escapes the bundle", p)
		}
		return nil
	})
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func scanExtra(root, manifest string, contracts []string) (err error) {
	skip := map[string]bool{manifest: true}
	for _, name := range contracts {
		skip[name] = true
	}
	fsys, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := fsys.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	return filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !isYAML(d.Name()) || skip[p] {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		data, err := fsys.ReadFile(filepath.ToSlash(rel))
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
	if err := yamlfile.Prepare(data); err != nil {
		return fmt.Errorf("parse bundle: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse bundle: %w", err)
	}
	return walkSecrets(&doc)
}

func walkSecrets(n *yaml.Node) error {
	return walkSecretsSeen(n, map[*yaml.Node]struct{}{})
}

func walkSecretsSeen(n *yaml.Node, active map[*yaml.Node]struct{}) error {
	if n == nil {
		return nil
	}
	if _, seen := active[n]; seen {
		return errors.New("bundle yaml alias cycle")
	}
	active[n] = struct{}{}
	defer delete(active, n)
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			if err := walkSecretsSeen(child, active); err != nil {
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
				return errors.New("bundle contains an environment base URL")
			}
			if err := walkSecretsSeen(n.Content[i+1], active); err != nil {
				return err
			}
		}
	case yaml.AliasNode:
		return walkSecretsSeen(n.Alias, active)
	case yaml.ScalarNode:
		return nil
	}
	return nil
}

func normKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "-", "_"))
}
