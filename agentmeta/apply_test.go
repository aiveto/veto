package agentmeta_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/agentmeta"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverlaySetsPermissionsAndCanRequireConfirmation(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	f, err := agentmeta.Load("../testdata/agent.yaml")
	require.NoError(t, err)
	require.NoError(t, agentmeta.Apply(cat, f))
	del := cat.ByID("orders.delete")
	require.NotNil(t, del)
	assert.Equal(t, "discovery-only", cat.ByID("orders.list").Exposure)
	assert.Equal(t, []string{"order.delete"}, del.Permissions)
	assert.True(t, del.RequiresConfirmation)

	yes := true
	no := false
	destructive := "destructive"
	read := cat.ByID("orders.get")
	require.NoError(t, agentmeta.Apply(cat, agentmeta.File{Operations: []agentmeta.Entry{{
		Operation:    "orders.get",
		Confirmation: &yes,
	}}}))
	d, err := policy.Builtin{}.Check(context.Background(), read)
	require.NoError(t, err)
	assert.Equal(t, policy.DecisionConfirmationNeeded, d)
	require.NoError(t, agentmeta.Apply(cat, agentmeta.File{Operations: []agentmeta.Entry{{
		Operation:    "orders.delete",
		Confirmation: &no,
	}}}))
	assert.Equal(t, "destructive", string(del.SideEffect))
	d, err = policy.Builtin{}.Check(context.Background(), del)
	require.NoError(t, err)
	assert.Equal(t, policy.DecisionAllow, d)

	list := cat.ByID("orders.list")
	require.NoError(t, agentmeta.Apply(cat, agentmeta.File{Operations: []agentmeta.Entry{{
		Operation:  "orders.list",
		SideEffect: &destructive,
	}}}))
	d, err = policy.Builtin{}.Check(context.Background(), list)
	require.NoError(t, err)
	assert.Equal(t, policy.DecisionConfirmationNeeded, d)
}

func TestUnknownOperationRefused(t *testing.T) {
	cat, err := openapi.Load(context.Background(), "../testdata/orders.yaml")
	require.NoError(t, err)
	err = agentmeta.Apply(cat, agentmeta.File{Operations: []agentmeta.Entry{{Operation: "missing"}}})
	assert.Error(t, err)
}
