package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmptySecurityAlternativeIsReady(t *testing.T) {
	creds := auth.New(auth.Options{Dir: t.TempDir()})
	assert.True(t, groupReady(nil, creds))
	op := catalog.Operation{
		ID: "ping",
		Requirements: [][]catalog.Auth{
			{},
			{{Name: "bearerAuth", Kind: "bearer", Header: "Authorization"}},
		},
	}
	assert.Empty(t, missingAuth(op, creds))
	blocked := authBlockers(&catalog.Catalog{Operations: []catalog.Operation{op}}, creds)
	assert.Empty(t, blocked)
}

func TestPingTreatsNon2xxAsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	cat := &catalog.Catalog{Operations: []catalog.Operation{{ID: "ping", BaseURL: srv.URL}}}
	lines := pingServers(t.Context(), srv.Client(), cat)
	require.NotEmpty(t, lines)
	assert.Contains(t, lines[0], "502")
}

func TestRepeatedMissingAuthCollapses(t *testing.T) {
	got := collapseAuth([]string{"customers.list: missing auth bearerAuth", "orders.list: missing auth bearerAuth"})
	assert.Equal(t, []string{"missing auth bearerAuth"}, got)
	assert.Equal(t, []string{"ping: missing auth bearerAuth"}, collapseAuth([]string{"ping: missing auth bearerAuth"}))
	mixed := collapseAuth([]string{"a: missing auth bearerAuth", "b: missing auth basic"})
	assert.Equal(t, []string{"a: missing auth bearerAuth", "b: missing auth basic"}, mixed)
}

func TestStdioRejectsStdoutTraces(t *testing.T) {
	require.Error(t, stdioTraceConflict(true, "stdout"))
	assert.NoError(t, stdioTraceConflict(true, "otlp"))
	assert.NoError(t, stdioTraceConflict(false, "stdout"))
}
