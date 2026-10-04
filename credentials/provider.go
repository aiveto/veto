// Package credentials is how an embedder supplies material for one upstream call.
// The binary's sources stay in config: env, login, client_credentials, invoke, command, and token_exchange.
package credentials

import (
	"context"
	"net/http"
	"time"
)

type (
	// Provider obtains credential material for one request. It does not know the company's auth scheme.
	Provider interface {
		Resolve(ctx context.Context, in Request) (Credential, error)
	}

	// ProviderFunc lets a closure satisfy Provider.
	ProviderFunc func(ctx context.Context, in Request) (Credential, error)

	// Request is the call that needs a credential.
	Request struct {
		OperationID string
		Method      string
		URL         string
		Scheme      string
		UserToken   string
		Scopes      []string
		Audience    string
	}

	// Credential is applied immediately before HTTP.
	// Sign is optional and runs after the request is built.
	Credential struct {
		Headers   map[string]string
		Query     map[string]string
		ExpiresAt time.Time
		Sign      func(*http.Request) error
	}
)

func (f ProviderFunc) Resolve(ctx context.Context, in Request) (Credential, error) {
	return f(ctx, in)
}
