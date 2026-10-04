package capability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/semantics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONSessionSearchDescribeInvoke(t *testing.T) {
	orders, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	customers, err := openapi.Load(context.Background(), "../testdata/customers.yaml")
	require.NoError(t, err)
	cat, err := catalog.Merge(orders, customers)
	require.NoError(t, err)
	relData, err := os.ReadFile("../testdata/relations.yaml")
	require.NoError(t, err)
	rels, err := catalog.ParseRelations(relData)
	require.NoError(t, err)
	require.NoError(t, catalog.ApplyRelations(cat, rels))
	sem := semantics.New(cat)
	loop, err := agent.New(cat, sem, execute.Client{})
	require.NoError(t, err)
	calls := loop.Runtime()
	srv := &capability.Server{Catalog: cat, Semantics: sem, Calls: &calls}

	in := strings.NewReader(`{"search":{"query":"retire order 123"}}
{"describe":{"operation_id":"orders.get"}}
{"invoke":{"operation_id":"orders.delete","params":{"id":"123"}},"caller":"ada"}
`)
	var out bytes.Buffer
	require.NoError(t, capability.RunJSON(context.Background(), srv, in, &out))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 3)

	var hits []capability.SearchHit
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &hits))
	require.NotEmpty(t, hits)
	assert.Equal(t, "orders.delete", hits[0].ID)

	assert.Contains(t, lines[1], "Order.customerId identifies customers.get")

	var res capability.InvokeResult
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &res))
	assert.Equal(t, "confirmation_required", res.Status)
	assert.Equal(t, "held until you approve", res.Why)
	assert.False(t, res.HTTP)
	assert.Equal(t, "ada", res.Caller)
}

func TestJSONSessionRejectsTwoCapabilities(t *testing.T) {
	srv := &capability.Server{}
	var out bytes.Buffer
	err := capability.RunJSON(context.Background(), srv, strings.NewReader(`{"search":{"query":"x"},"describe":{"operation_id":"orders.get"}}`+"\n"), &out)
	require.Error(t, err)
	assert.Contains(t, out.String(), "exactly one of search, describe, invoke")
}
