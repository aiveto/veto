package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundleCheckRunsOrdersAndCustomers(t *testing.T) {
	t.Cleanup(releaseBundles)
	dir := ordersCustomersBundle(t)
	cfgPath, err := filepath.Abs("../../testdata/veto.yaml")
	require.NoError(t, err)
	fromConfig, _, err := buildLoop(nil, cfgPath, "", "", "")
	require.NoError(t, err)
	fromBundle, _, err := buildLoopBundle(nil, "", dir, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, operationIDs(fromConfig.Catalog), operationIDs(fromBundle.Catalog))
	assert.Equal(t, fromConfig.Catalog.Joins(), fromBundle.Catalog.Joins())
	require.NoError(t, runChecked(checkCmd{bundle: dir}))

	zipPath := filepath.Join(t.TempDir(), "orders-customers.zip")
	require.NoError(t, zipTree(zipPath, dir))
	require.NoError(t, runChecked(checkCmd{bundle: zipPath}))
}

func TestBundleKeepsDeploymentAuth(t *testing.T) {
	t.Cleanup(releaseBundles)
	dir := ordersCustomersBundle(t)
	cfgPath := filepath.Join(t.TempDir(), "veto.yaml")
	body := fmt.Sprintf(`bundle: %s
auth:
  bearerAuth: ORDER_TOKEN
`, strconv.Quote(dir))
	require.NoError(t, os.WriteFile(cfgPath, []byte(body), 0o600))
	src, err := resolve(cfgPath, nil, "", "")
	cfg, contracts, relations := src.cfg, src.contracts, src.relations
	require.NoError(t, err)
	assert.Equal(t, "ORDER_TOKEN", cfg.Auth["bearerAuth"].Env)
	assert.Empty(t, cfg.Server)
	assert.Empty(t, cfg.TokenDir)
	require.Len(t, contracts, 2)
	assert.NotEmpty(t, relations)
	require.Len(t, cfg.Cases, 1)
	_, _, err = buildLoop(nil, cfgPath, "", "", "")
	require.NoError(t, err)
	checked := checkCmd{
		config: cfgPath}
	require.NoError(t, runChecked(checked))
}

func ordersCustomersBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	fsys, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fsys.Close()) })
	for _, name := range []string{
		"orders.yaml",
		"customers.yaml",
		"relations.yaml",
		"semantics.yaml",
		"agent.yaml",
		"flow.yaml",
		"cases/delete.yaml",
		"cases/neighbor.yaml",
	} {
		data, err := os.ReadFile(filepath.Join("../../testdata", name))
		require.NoError(t, err)
		rel := filepath.ToSlash(name)
		if parent := path.Dir(rel); parent != "." {
			require.NoError(t, fsys.MkdirAll(parent, 0o750))
		}
		require.NoError(t, fsys.WriteFile(rel, data, 0o600))
	}
	manifest := `relations_file: relations.yaml
contracts:
  - orders.yaml
  - customers.yaml
cases:
  - cases
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bundle.yaml"), []byte(manifest), 0o600))
	return dir
}

func operationIDs(cat *catalog.Catalog) []string {
	ids := make([]string, 0, len(cat.Operations))
	for _, op := range cat.Operations {
		ids = append(ids, op.ID)
	}
	return ids
}

func zipTree(dest, root string) (err error) {
	fsys, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := fsys.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
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
		body, err := fsys.ReadFile(name)
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
		return errors.Join(err, w.Close(), f.Close())
	}
	if err := w.Close(); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}
