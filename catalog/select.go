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
func (s Selection) Keeps(op Operation) bool {
	if s.ReadOnly && op.Method != http.MethodGet && op.Method != http.MethodHead {
		return false
	}
	if len(s.Tags) == 0 && len(s.Paths) == 0 {
		return true
	}
	for _, tag := range op.Tags {
		if slices.Contains(s.Tags, tag) {
			return true
		}
	}
	for _, prefix := range s.Paths {
		if underPath(op.PathTemplate, prefix) {
			return true
		}
	}
	return false
}

// Select removes every operation s does not keep, with the links and relations that point at it.
func (c *Catalog) Select(s Selection) {
	if c == nil {
		return
	}
	c.Operations = slices.DeleteFunc(c.Operations, func(op Operation) bool { return !s.Keeps(op) })
	c.Finalize()
}

func underPath(path, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")
	return prefix == "" || path == prefix || strings.HasPrefix(path, prefix+"/")
}
