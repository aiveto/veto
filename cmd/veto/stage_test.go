package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithStageNamesTheFinding(t *testing.T) {
	assert.Equal(t, "orders.get: fallback id: catalog", withStage("orders.get: fallback id"))
	assert.Equal(t, "items.delete: write requires approval: held until you approve", withStage("items.delete: write requires approval"))
	assert.Equal(t, "confirmation is off: policy", withStage("confirmation is off"))
	assert.Equal(t, "ORDER_TOKEN is unset: missing auth", withStage("ORDER_TOKEN is unset"))
	assert.Equal(t, "ping: missing auth bearerAuth", withStage("ping: missing auth bearerAuth"))
	assert.Equal(t, "search.find: parameter session cannot be serialized: style matrix", withStage("search.find: parameter session cannot be serialized: style matrix"))
	assert.Equal(t, "ping http://127.0.0.1:1: upstream: dial", withStage("ping http://127.0.0.1:1: upstream: dial"))
	assert.Equal(t, "token url is unset", withStage("token url is unset"))
	assert.Equal(t, "task investigate-charge can no longer obtain invoiceId from orders.get: catalog", withStage("task investigate-charge can no longer obtain invoiceId from orders.get"))
}
