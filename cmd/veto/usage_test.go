package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMissingInputNamesAnExample(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"search"}, "query required, for example: veto search orders"},
		{[]string{"describe"}, "operation required, for example: veto describe orders.get"},
		{[]string{"invoke"}, "operation required, for example: veto invoke orders.list"},
		{[]string{"preview"}, "operation required, for example: veto preview orders.list"},
		{[]string{"init"}, "OpenAPI file required, for example: veto init orders.yaml"},
		{[]string{"run"}, "task required, for example: veto run read-orders-list"},
		{[]string{"approve"}, "approval id required, for example: veto approve pending-id"},
		{[]string{"pack"}, `--message required, for example: veto pack --message "show me orders"`},
		{[]string{"eval"}, "--case required, for example: veto eval --case testdata/delete.yaml"},
		{[]string{"generate"}, "--out and --module required, for example: veto generate --out ./client --module example.com/orders"},
		{[]string{"generate", "--out", "./client"}, "--module required, for example: veto generate --out ./client --module example.com/orders"},
		{[]string{"generate", "--module", "example.com/orders"}, "--out required, for example: veto generate --out ./client --module example.com/orders"},
		{[]string{"replay"}, `message required, for example: veto replay --message "delete order 123"`},
		{[]string{"auth", "set"}, "scheme required, for example: veto auth set --scheme bearerAuth"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			root, err := newRoot()
			require.NoError(t, err)
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			root.SetArgs(tc.args)
			require.EqualError(t, root.Execute(), tc.want)
		})
	}
}

func TestHelpOpensWithWhatVetoIs(t *testing.T) {
	root, err := newRoot()
	require.NoError(t, err)
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"--help"})
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), "refuses a destructive call until confirmation is stored")
	assert.Contains(t, buf.String(), "--config")
}

func TestConfigFlagWorksBeforeTheCommand(t *testing.T) {
	cfg, err := filepath.Abs("../../testdata/veto.yaml")
	require.NoError(t, err)
	root, err := newRoot()
	require.NoError(t, err)
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"--config", cfg, "validate"})
	require.NoError(t, root.Execute())
	validate, _, err := root.Find([]string{"validate"})
	require.NoError(t, err)
	flag := validate.Flags().Lookup("config")
	require.NotNil(t, flag)
	assert.Equal(t, cfg, flag.Value.String())
}

func TestInvokeExitsWhenTheCallDidNotSucceed(t *testing.T) {
	assert.Equal(t, 0, invokeExit([]byte(`{"status":"ok"}`)))
	assert.Equal(t, 1, invokeExit([]byte(`{"status":"confirmation_required","approval_id":"pending"}`)))
	assert.Equal(t, 1, invokeExit([]byte(`{"status":"unauthorized","http_status":401}`)))
}

func TestContractNamesAnExample(t *testing.T) {
	_, err := loadCatalog(nil, "")
	require.EqualError(t, err, contractRequired)
	assert.Contains(t, err.Error(), "veto init orders.yaml")
}

func TestAuthLoginNamesTheConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	_, _, err := configuredScheme("", "")
	require.EqualError(t, err, "config required, for example: veto auth login --config veto.yaml --scheme bearerAuth")
}
