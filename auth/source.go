// Package auth places a credential on an upstream call.
// Operators of the binary use login, client credentials, token exchange, env, invoke, or a command.
// Library code implements credentials.Provider.
package auth

import "time"

type (
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
		// AuthToken is the token-response field sent as the app credential. Empty means access_token.
		AuthToken string
		// UserToken is the token-response field that names the person. Empty means id_token.
		UserToken string
		// UserHeader is the upstream header that carries the person token.
		UserHeader string
		// Subject is "invoke" or the name of a login scheme whose stored token is exchanged.
		Subject string
		// SubjectTokenType is sent on token exchange. Empty means an access token.
		SubjectTokenType string
	}
)
