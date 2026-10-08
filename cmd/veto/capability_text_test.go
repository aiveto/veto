package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchNamesTheQuery(t *testing.T) {
	err := searchArgs(nil, nil)
	require.ErrorContains(t, err, "veto search orders")
	require.NoError(t, searchArgs(nil, []string{"orders"}))
}

func TestPresentSearchPrintsCallLines(t *testing.T) {
	text, ok := presentSearch([]byte(`[{"id":"orders.list","call":"GET /orders orders.list"}]`))
	require.True(t, ok)
	assert.Equal(t, "GET /orders orders.list\n", text)
	empty, ok := presentSearch([]byte("[]"))
	require.True(t, ok)
	assert.Equal(t, "no operations\n", empty)
	none, ok := presentSearch([]byte("null"))
	require.True(t, ok)
	assert.Equal(t, "no operations\n", none)
}

func TestPresentDescribeNamesRequiredParams(t *testing.T) {
	body := []byte(`{"call":"orders.get Returns the order. id path required","relation":"Order.customerId identifies customers.get","operation":{"ID":"orders.get","Method":"GET","PathTemplate":"/orders/{id}","ResponseFields":["customerId","id"],"Params":[{"Name":"id","In":"path","Required":true},{"Name":"limit","In":"query","Required":false}]}}`)
	text, ok := presentDescribe(body)
	require.True(t, ok)
	assert.Equal(t, "GET /orders/{id}\norders.get Returns the order. id path required\nid required\ncustomerId id\nOrder.customerId identifies customers.get\n", text)
}

func TestPresentInvokeNamesTheStage(t *testing.T) {
	text, ok := presentInvoke([]byte(`{"status":"error","operation_id":"orders.list","why":"missing auth","error":"bearerAuth is unset"}`))
	require.True(t, ok)
	assert.Equal(t, "orders.list: missing auth\n", text)
	missing, ok := presentInvoke([]byte(`{"status":"error","operation_id":"orders.get","why":"parameter","code":"missing_param","error":"operation orders.get: id required"}`))
	require.True(t, ok)
	assert.Equal(t, "orders.get: id required\n", missing)
	okText, ok := presentInvoke([]byte(`{"status":"ok","operation_id":"orders.list","body":"{\"data\":[]}"}`))
	require.True(t, ok)
	assert.Equal(t, "orders.list: ok\n{\"data\":[]}\n", okText)
	denied, ok := presentInvoke([]byte(`{"status":"unauthorized","operation_id":"orders.list","http_status":401,"code":"unauthorized","why":"upstream"}`))
	require.True(t, ok)
	assert.Equal(t, "orders.list: upstream 401 unauthorized\n", denied)
}

func TestInvokeOperationFlagFillsTheID(t *testing.T) {
	args, err := invokeArgs(nil, "orders.get", []string{"id=10482"})
	require.NoError(t, err)
	in, err := decodeInvoke(args)
	require.NoError(t, err)
	assert.Equal(t, "orders.get", in.OperationID)
	assert.Equal(t, "10482", in.Params["id"])

	same, err := invokeArgs([]string{"orders.list"}, "orders.list", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"orders.list"}, same)
	_, err = invokeArgs([]string{"orders.list"}, "orders.get", nil)
	require.Error(t, err)
	_, err = invokeArgs(nil, "orders.list", []string{"id"})
	require.Error(t, err)
}

func TestPreviewAcceptsTheOperationID(t *testing.T) {
	id, err := previewOperation([]string{"orders.list"}, "")
	require.NoError(t, err)
	assert.Equal(t, "orders.list", id)
	id, err = previewOperation(nil, "orders.get")
	require.NoError(t, err)
	assert.Equal(t, "orders.get", id)
	_, err = previewOperation(nil, "")
	require.ErrorContains(t, err, "veto preview orders.list")
	_, err = previewOperation([]string{"orders.list"}, "orders.get")
	require.Error(t, err)
}
