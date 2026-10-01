package main

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/aiveto/veto/bundle"
	"github.com/aiveto/veto/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundleCheckRunsOrdersAndCustomers(t *testing.T) {
	t.Cleanup(bundle.Release)
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
	t.Cleanup(bundle.Release)
	dir := ordersCustomersBundle(t)
	cfgPath := filepath.Join(t.TempDir(), "veto.yaml")
	body := fmt.Sprintf(`bundle: %s
auth:
  bearerAuth: ORDER_TOKEN
`, strconv.Quote(dir))
	require.NoError(t, os.WriteFile(cfgPath, []byte(body), 0o644))
	cfg, contracts, relations, _, err := resolve(cfgPath, nil, "", "")
	require.NoError(t, err)
	assert.Equal(t, "ORDER_TOKEN", cfg.Auth["bearerAuth"].Env)
	assert.Empty(t, cfg.Server)
	assert.Empty(t, cfg.TokenDir)
	require.Len(t, contracts, 2)
	assert.NotEmpty(t, relations)
	require.Len(t, cfg.Cases, 1)
	_, _, err = buildLoop(nil, cfgPath, "", "", "")
	require.NoError(t, err)
	checked := checkCmd{}
	checked.config = cfgPath
	require.NoError(t, runChecked(checked))
}

func ordersCustomersBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
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
		dest := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
		require.NoError(t, os.WriteFile(dest, data, 0o644))
	}
	manifest := `relations_file: relations.yaml
contracts:
  - orders.yaml
  - customers.yaml
cases:
  - cases
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bundle.yaml"), []byte(manifest), 0o644))
	return dir
}

func operationIDs(cat *catalog.Catalog) []string {
	ids := make([]string, 0, len(cat.Operations))
	for _, op := range cat.Operations {
		ids = append(ids, op.ID)
	}
	return ids
}

func zipTree(dest, root string) error {
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
