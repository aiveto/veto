package openapi

import (
	"sort"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/getkin/kin-openapi/openapi3"
)

// operationSecurity returns OR groups. Schemes inside one group are all sent.
// An empty security array is a public operation and returns nil.
func operationSecurity(doc *openapi3.T, op *openapi3.Operation) [][]catalog.Auth {
	if doc == nil || op == nil {
		return nil
	}
	var reqs openapi3.SecurityRequirements
	if op.Security != nil {
		reqs = *op.Security
	} else {
		reqs = doc.Security
	}
	if len(reqs) == 0 {
		return nil
	}
	out := make([][]catalog.Auth, 0, len(reqs))
	for _, req := range reqs {
		names := make([]string, 0, len(req))
		for name := range req {
			names = append(names, name)
		}
		sort.Strings(names)
		group := make([]catalog.Auth, 0, len(names))
		for _, name := range names {
			group = append(group, mapScheme(doc, name, req[name]))
		}
		out = append(out, group)
	}
	return out
}

func mapScheme(doc *openapi3.T, name string, scopes []string) catalog.Auth {
	scheme := securityScheme(doc, name)
	if scheme == nil {
		return catalog.Auth{Name: name, Kind: "unsupported"}
	}
	copied := cloneScopes(scopes)
	switch {
	case strings.EqualFold(scheme.Type, "http") && strings.EqualFold(scheme.Scheme, "bearer"):
		return catalog.Auth{Name: name, Header: "Authorization", Kind: "bearer", Scopes: copied}
	case strings.EqualFold(scheme.Type, "apiKey") && strings.EqualFold(scheme.In, "header"):
		return catalog.Auth{Name: name, Header: scheme.Name, Kind: "apiKey", Scopes: copied}
	case strings.EqualFold(scheme.Type, "apiKey") && strings.EqualFold(scheme.In, "query"):
		return catalog.Auth{Name: name, Query: scheme.Name, Kind: "apiKey", Scopes: copied}
	case strings.EqualFold(scheme.Type, "oauth2"):
		return catalog.Auth{Name: name, Header: "Authorization", Kind: "oauth2", Scopes: copied}
	default:
		return catalog.Auth{Name: name, Kind: "unsupported"}
	}
}

func cloneScopes(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func securityScheme(doc *openapi3.T, name string) *openapi3.SecurityScheme {
	if doc.Components == nil {
		return nil
	}
	ref := doc.Components.SecuritySchemes[name]
	if ref == nil {
		return nil
	}
	return ref.Value
}
