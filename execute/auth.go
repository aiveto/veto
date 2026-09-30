package execute

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
)

func applyAuth(ctx context.Context, cfg Config, op *catalog.Operation, req *http.Request, force bool) (bool, []string, []string, error) {
	groups := op.Requirements
	if len(groups) == 0 && len(op.Auth) > 0 {
		groups = [][]catalog.Auth{op.Auth}
	}
	if len(groups) == 0 {
		return false, nil, nil, nil
	}
	chosen, err := selectRequirement(ctx, cfg, groups)
	if err != nil {
		return false, nil, nil, fmt.Errorf("operation %s: %w", op.ID, err)
	}
	var refresh bool
	var secrets []string
	var queryKeys []string
	for _, a := range chosen {
		mat, err := material(ctx, cfg, op, a, req.Method, req.URL.String(), force)
		if err != nil {
			return false, nil, nil, fmt.Errorf("operation %s: %w", op.ID, err)
		}
		for k, v := range mat.Headers {
			req.Header.Set(k, v)
		}
		if len(mat.Query) > 0 {
			q := req.URL.Query()
			for k, v := range mat.Query {
				q.Set(k, v)
				queryKeys = append(queryKeys, k)
			}
			req.URL.RawQuery = q.Encode()
		}
		secrets = append(secrets, mat.Secrets...)
		if cfg.Creds != nil && cfg.Creds.Refreshable(a.Name) {
			refresh = true
		}
	}
	return refresh, secrets, queryKeys, nil
}

func selectRequirement(ctx context.Context, cfg Config, groups [][]catalog.Auth) ([]catalog.Auth, error) {
	var why error
	for _, group := range groups {
		if err := requirementError(ctx, cfg, group); err != nil {
			if why == nil {
				why = err
			}
			continue
		}
		return group, nil
	}
	if why == nil {
		why = fmt.Errorf("credential is unset")
	}
	return nil, why
}

func requirementError(ctx context.Context, cfg Config, group []catalog.Auth) error {
	for _, a := range group {
		if a.Kind == "unsupported" || (a.Kind == "apiKey" && a.Header == "" && a.Query == "") {
			return fmt.Errorf("security scheme %s is not supported", a.Name)
		}
		if cfg.Creds != nil && cfg.Creds.Has(a.Name) {
			if !cfg.Creds.Ready(ctx, a) {
				return cfg.Creds.UnsetError(a)
			}
			continue
		}
		if cfg.Auth[a.Name] == "" {
			return fmt.Errorf("%s is unset", a.Name)
		}
	}
	return nil
}

func material(ctx context.Context, cfg Config, op *catalog.Operation, a catalog.Auth, method, endpoint string, force bool) (auth.Material, error) {
	if cfg.Creds != nil && cfg.Creds.Has(a.Name) {
		return cfg.Creds.Material(ctx, op, a, method, endpoint, force)
	}
	val := cfg.Auth[a.Name]
	if val == "" {
		return auth.Material{}, fmt.Errorf("%s is unset", a.Name)
	}
	return legacyMaterial(a, val), nil
}

func legacyMaterial(a catalog.Auth, val string) auth.Material {
	switch a.Kind {
	case "apiKey":
		if a.Query != "" {
			return auth.Material{Query: map[string]string{a.Query: val}, Secrets: []string{val}}
		}
		header := a.Header
		if header == "" {
			header = "Authorization"
		}
		return auth.Material{Headers: map[string]string{header: val}, Secrets: []string{val}}
	default:
		header := a.Header
		if header == "" {
			header = "Authorization"
		}
		sent := val
		if header == "Authorization" {
			sent = "Bearer " + val
		}
		return auth.Material{Headers: map[string]string{header: sent}, Secrets: []string{val, sent}}
	}
}
