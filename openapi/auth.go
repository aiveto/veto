package openapi

import (
	"sort"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/getkin/kin-openapi/openapi3"
)

func operationAuth(doc *openapi3.T, op *openapi3.Operation) []catalog.Auth {
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
	names := make([]string, 0, len(reqs[0]))
	for name := range reqs[0] {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []catalog.Auth
	for _, name := range names {
		scheme := securityScheme(doc, name)
		if scheme == nil || !strings.EqualFold(scheme.Type, "http") || !strings.EqualFold(scheme.Scheme, "bearer") {
			continue
		}
		out = append(out, catalog.Auth{Name: name, Header: "Authorization", Kind: "bearer"})
	}
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
