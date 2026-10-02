package generate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/aiveto/veto/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionFromUsesTheVetoModule(t *testing.T) {
	host := &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/app", Version: "v1.2.3"},
		Deps: []*debug.Module{{Path: vetoModulePath, Version: "v0.4.1"}},
	}
	got, err := versionFrom(host)
	require.NoError(t, err)
	assert.Equal(t, "v0.4.1", got)

	own := &debug.BuildInfo{Main: debug.Module{Path: vetoModulePath, Version: "v0.9.0"}}
	got, err = versionFrom(own)
	require.NoError(t, err)
	assert.Equal(t, "v0.9.0", got)

	replaced := &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/app", Version: "v3.0.0"},
		Deps: []*debug.Module{{
			Path:    vetoModulePath,
			Version: "v0.4.1",
			Replace: &debug.Module{Path: "../veto", Version: ""},
		}},
	}
	got, err = versionFrom(replaced)
	require.NoError(t, err)
	assert.Equal(t, "v0.4.1", got)

	when := time.Date(2026, 10, 2, 3, 12, 0, 0, time.UTC)
	devel := &debug.BuildInfo{
		Main: debug.Module{Path: vetoModulePath, Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "14b6ddcabcdeffff"},
			{Key: "vcs.time", Value: when.Format(time.RFC3339)},
		},
	}
	got, err = versionFrom(devel)
	require.NoError(t, err)
	assert.Equal(t, "v0.0.0-20261002031200-14b6ddcabcde", got)
	assert.NotEqual(t, "v0.0.0", got)

	bare := &debug.BuildInfo{Main: debug.Module{Path: vetoModulePath, Version: "(devel)"}}
	_, err = versionFrom(bare)
	require.ErrorIs(t, err, errNoModuleVersion)

	missing := &debug.BuildInfo{Main: debug.Module{Path: "example.com/app", Version: "v1.2.3"}}
	_, err = versionFrom(missing)
	require.ErrorIs(t, err, errNoModuleVersion)
	_, err = versionFrom(nil)
	require.ErrorIs(t, err, errNoModuleVersion)
}

func TestPseudoVersionFollowsThePreviousTag(t *testing.T) {
	when := time.Date(2026, 10, 2, 3, 12, 0, 0, time.UTC)
	assert.Equal(t, "v0.0.0-20261002031200-14b6ddcabcde", pseudoVersion("", when, "14b6ddcabcde"))
	assert.Equal(t, "v0.1.1-0.20261002031200-14b6ddcabcde", pseudoVersion("v0.1.0", when, "14b6ddcabcde"))
	assert.Equal(t, "v0.1.10-0.20261002031200-14b6ddcabcde", pseudoVersion("v0.1.9", when, "14b6ddcabcde"))
}

func TestGeneratedModuleResolvesWithoutAReplace(t *testing.T) {
	cat := &catalog.Catalog{Operations: []catalog.Operation{{
		ID: "orders.get", Method: "GET", PathTemplate: "/orders/{id}",
		Params: []catalog.Param{{Name: "id", In: "path", Required: true}},
	}}}
	cat.Finalize()
	dir := t.TempDir()
	require.NoError(t, Write(dir, "example.com/outside", cat))
	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	require.NoError(t, err)
	ver, err := moduleVersion()
	require.NoError(t, err)
	assert.NotEqual(t, "v0.0.0", ver)
	assert.Contains(t, string(mod), "require github.com/aiveto/veto "+ver+"\n")
	assert.NotContains(t, string(mod), "\nreplace ")

	root, err := filepath.Abs("..")
	require.NoError(t, err)
	proxy := t.TempDir()
	require.NoError(t, writeModuleProxy(t.Context(), proxy, root, ver))

	env := proxyEnv(proxy)
	stdout, stderr := runOut(t, dir, env, "go", "mod", "download", "-json", vetoModulePath)
	var fetched struct {
		Version string `json:"Version"`
		Dir     string `json:"Dir"`
		Error   string `json:"Error"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &fetched), stderr)
	assert.Empty(t, fetched.Error)
	assert.Equal(t, ver, fetched.Version)
	assert.NotEqual(t, root, filepath.Clean(fetched.Dir))
	assert.False(t, strings.HasPrefix(filepath.Clean(fetched.Dir), root+string(filepath.Separator)))

	_, _ = runOut(t, dir, env, "go", "mod", "tidy")
	stdout, _ = runOut(t, dir, env, "go", "run", "./cli", "get", "--help-json")
	assert.Contains(t, stdout, `"operation":"orders.get"`)
}

func runOut(t *testing.T, dir string, env []string, name string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), stderr.String())
	return stdout.String(), stderr.String()
}

func proxyEnv(proxy string) []string {
	skip := map[string]bool{
		"GOPROXY": true, "GOSUMDB": true, "GOPRIVATE": true,
		"GONOSUMDB": true, "GONOPROXY": true, "GOFLAGS": true,
	}
	env := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if skip[key] {
			continue
		}
		env = append(env, entry)
	}
	return append(env,
		"GOPROXY=file://"+proxy+",https://proxy.golang.org,direct",
		"GOSUMDB=off",
		"GOPRIVATE=",
		"GOFLAGS=",
	)
}

func writeModuleProxy(ctx context.Context, proxy, root, version string) error {
	base := filepath.Join(proxy, "github.com", "aiveto", "veto", "@v")
	if err := os.MkdirAll(base, 0o750); err != nil {
		return err
	}
	mod, err := exec.CommandContext(ctx, "git", "-C", root, "show", "HEAD:go.mod").Output()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(base, version+".mod"), mod, 0o600); err != nil {
		return err
	}
	info, err := json.Marshal(map[string]string{
		"Version": version,
		"Time":    time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(base, version+".info"), append(info, '\n'), 0o600); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(base, version+".zip"))
	if err != nil {
		return err
	}
	archived, err := exec.CommandContext(ctx, "git", "-C", root, "archive", "--format=tar", "HEAD").Output()
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	prefix := vetoModulePath + "@" + version + "/"
	tr := tar.NewReader(bytes.NewReader(archived))
	var packErr error
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			packErr = err
			break
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			packErr = err
			break
		}
		w, err := zw.Create(prefix + filepath.ToSlash(hdr.Name))
		if err != nil {
			packErr = err
			break
		}
		if _, err := w.Write(data); err != nil {
			packErr = err
			break
		}
	}
	closeZip := zw.Close()
	closeFile := f.Close()
	if packErr != nil {
		return packErr
	}
	if closeZip != nil {
		return closeZip
	}
	return closeFile
}
