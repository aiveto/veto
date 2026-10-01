package bundle_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiveto/veto/bundle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundleRejectsClientSecret(t *testing.T) {
	t.Cleanup(bundle.Release)
	dir := t.TempDir()
	body := "contracts:\n  - orders.yaml\nauth:\n  job:\n    client_secret: super-secret-value\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bundle.yaml"), []byte(body), 0o644))
	_, err := bundle.Load(dir)
	require.Error(t, err)
	assert.ErrorContains(t, err, "client_secret")

	zipPath := filepath.Join(t.TempDir(), "secret.zip")
	require.NoError(t, zipDir(zipPath, dir))
	_, err = bundle.Load(zipPath)
	require.Error(t, err)
	assert.ErrorContains(t, err, "client_secret")
}

func TestBundleRejectsTokenURLAndBaseURL(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "token url",
			body: "auth:\n  job:\n    token_url: https://idp.example/token\n    client_id: job\n",
			want: "token_url",
		},
		{
			name: "base url",
			body: "server: https://orders.example\ncontracts:\n  - orders.yaml\n",
			want: "environment base URL",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "bundle.yaml"), []byte(tc.body), 0o644))
			_, err := bundle.Load(dir)
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestBundleAllowsClientSecretEnvName(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile("../testdata/orders.yaml")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "orders.yaml"), data, 0o644))
	body := "contracts:\n  - orders.yaml\nauth:\n  job:\n    client_secret_env: VETO_SECRET\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bundle.yaml"), []byte(body), 0o644))
	loaded, err := bundle.Load(dir)
	require.NoError(t, err)
	t.Cleanup(loaded.Close)
	assert.Empty(t, loaded.Config.Auth)
	require.Len(t, loaded.Config.Contracts, 1)
	assert.True(t, strings.HasSuffix(loaded.Config.Contracts[0], "orders.yaml"))
}

func TestBundleZipReadsTheInnerDirectory(t *testing.T) {
	parent := t.TempDir()
	inner := filepath.Join(parent, "orders-customers")
	require.NoError(t, os.MkdirAll(filepath.Join(inner, "cases"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(inner, "bundle.yaml"), []byte("cases:\n  - cases\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(inner, "cases", "one.yaml"), []byte("name: catalog\ninput: surface\n"), 0o644))
	zipPath := filepath.Join(t.TempDir(), "bundle.zip")
	require.NoError(t, zipDir(zipPath, parent))
	loaded, err := bundle.Load(zipPath)
	require.NoError(t, err)
	t.Cleanup(loaded.Close)
	require.Len(t, loaded.Config.Cases, 1)
	assert.True(t, strings.HasSuffix(loaded.Config.Cases[0], "cases"))
}

func zipDir(dest, root string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	w := zip.NewWriter(f)
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if d.IsDir() {
			if name == "." {
				return nil
			}
			_, err = w.Create(name + "/")
			return err
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		entry, err := w.Create(name)
		if err != nil {
			return err
		}
		_, err = entry.Write(body)
		return err
	})
	if err != nil {
		w.Close()
		f.Close()
		return err
	}
	if err := w.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
