package catalog

import (
	"net/http"
	"slices"
	"strings"
)

// Selection is the part of a contract a deployment exposes; the zero value keeps every operation.
type Selection struct {
	ReadOnly bool
	Tags     []string
	Paths    []string
}

// Keeps reports whether op stays in the catalog.
// Tags and paths are OR within each list and AND across lists that are set.
func (s Selection) Keeps(op Operation) bool {
	if s.ReadOnly && op.Method != http.MethodGet && op.Method != http.MethodHead {
		return false
	}
	if len(s.Tags) > 0 && !hasAny(op.Tags, s.Tags) {
		return false
	}
	if len(s.Paths) > 0 && !underAny(op.PathTemplate, s.Paths) {
		return false
	}
	return true
}

// Select removes every operation s does not keep, with the links and relations that point at it.
func (c *Catalog) Select(s Selection) {
	if c == nil {
		return
	}
	c.Operations = slices.DeleteFunc(c.Operations, func(op Operation) bool { return !s.Keeps(op) })
	c.Finalize()
}

func hasAny(have, want []string) bool {
	for _, tag := range have {
		if slices.Contains(want, tag) {
			return true
		}
	}
	return false
}

func underAny(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if underPath(path, prefix) {
			return true
		}
	}
	return false
}

func underPath(path, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")
	return prefix == "" || path == prefix || strings.HasPrefix(path, prefix+"/")
}
