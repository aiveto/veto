// Package auth places a credential on an upstream call.
// Operators of the binary use login, client credentials, env, invoke, or a command.
// Source is for code that embeds veto as a library.
package auth

import (
	"context"
	"time"
)

type (
	// Source mints headers for one scheme. The binary does not ask operators to implement it.
	Source interface {
		Token(ctx context.Context, in Input) (Output, error)
	}

	Input struct {
		OperationID string
		Method      string
		URL         string
		Scheme      string
		UserToken   string
		Scopes      []string
		Audience    string
	}

	Output struct {
		Headers   map[string]string
		ExpiresAt time.Time
	}

	// Scheme is one configured security scheme. Secrets are not stored here.
	Scheme struct {
		Name                   string
		Source                 string
		Env                    string
		ClientID               string
		ClientSecretEnv        string
		AuthorizationURL       string
		TokenURL               string
		Issuer                 string
		DeviceAuthorizationURL string
		RedirectURL            string
		Scopes                 []string
		Audience               string
		Header                 string
		Command                []string
		Timeout                time.Duration
	}
)
