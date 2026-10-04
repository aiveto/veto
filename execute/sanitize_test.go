package execute_test

import (
	"context"
	"testing"

	"github.com/aiveto/veto/execute"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDraftKeepsLargeIntegersAndRedactsSecrets(t *testing.T) {
	const id = "9007199254740993"
	const secret = "s3cret-value"
	cat := loadSpec(t, bodySpec)
	draft, err := (execute.Client{BaseURL: "http://127.0.0.1:9"}).Draft(context.Background(), cat.ByID("orders.create"), map[string]string{
		"body": `{"name":"kit","count":` + id + `,"password":"` + secret + `"}`,
	})
	require.NoError(t, err)
	assert.Contains(t, draft.Body, `"count":`+id)
	assert.NotContains(t, draft.Body, "9007199254740992")
	assert.Contains(t, draft.Body, "REDACTED")
	assert.NotContains(t, draft.Body, secret)
}
