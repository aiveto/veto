package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmptySecurityAlternativeIsReady(t *testing.T) {
	assert.True(t, groupReady(nil, nil, ""))
	op := catalog.Operation{
		ID: "ping",
		Requirements: [][]catalog.Auth{
			{},
			{{Name: "bearerAuth", Kind: "bearer", Header: "Authorization"}},
		},
	}
	assert.Empty(t, missingAuth(op, nil, t.TempDir()))
	blocked := authBlockers(&catalog.Catalog{Operations: []catalog.Operation{op}}, config.Sources{}, t.TempDir())
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

func TestStdioRejectsStdoutTraces(t *testing.T) {
	require.Error(t, stdioTraceConflict(true, "stdout"))
	assert.NoError(t, stdioTraceConflict(true, "otlp"))
	assert.NoError(t, stdioTraceConflict(false, "stdout"))
}
