package catalog

import (
	"fmt"
	"sort"
)

const (
	KindRead   Kind = "read"
	KindCreate Kind = "create"
	KindUpdate Kind = "update"
	KindDelete Kind = "delete"
	KindAction Kind = "action"

	SideEffectNone        SideEffect = "none"
	SideEffectWrite       SideEffect = "write"
	SideEffectDestructive SideEffect = "destructive"

	// ExposureDirect is a pinned tool. ExposureGrouped is one tool for the resource.
	// ExposureDiscovery stays on search and describe.
	ExposureDirect    = "direct"
	ExposureGrouped   = "grouped"
	ExposureDiscovery = "discovery-only"
)

type (
	// Kind classifies what an operation does to server state.
	Kind string

	// SideEffect describes how invasive an operation is.
	SideEffect string

	// Server is one URL from the contract. The first is used when no name is selected.
	Server struct {
		URL  string
		Name string
	}

	// Auth is one security scheme an operation applies. The secret stays outside the catalog.
	Auth struct {
		Name   string
		Header string
		Kind   string
	}

	// Param is one request parameter from the contract.
	Param struct {
		Name        string
		In          string // path, query, header, body
		Required    bool
		Description string
		Schema      string
	}

	// Operation is one callable capability from the contract catalog.
	Operation struct {
		ID                   string
		Name                 string
		Description          string
		Group                string
		Kind                 Kind
		Method               string
		PathTemplate         string
		Params               []Param
		ResponseSummary      string
		SideEffect           SideEffect
		RequiresConfirmation bool
		Permissions          []string
		Idempotency          string
		Retry                string
		Exposure             string
		BaseURL              string
		Servers              []Server
		Page                 map[string]string
		Auth                 []Auth
		Tags                 []string
		ResponseFields       []string
	}

	// OpLink is an OpenAPI link or a declared relation from one operation to another.
	// Params maps a target parameter to a response expression. Note is the relation field.
	OpLink struct {
		From   string
		To     string
		Note   string
		Params map[string]string
	}

	// SchemaUse records a component schema referenced by an operation.
	SchemaUse struct {
		OperationID string
		Name        string
	}

	// Catalog holds the operations and the capability graph for every loaded contract.
	Catalog struct {
		Title      string
		Version    string
		Operations []Operation
		Links      []OpLink
		Uses       []SchemaUse
		Graph      Graph
		byID       map[string]*Operation
	}
)

// BodyParam returns the JSON body parameter, when the contract declares one.
func (op Operation) BodyParam() (Param, bool) {
	for _, p := range op.Params {
		if p.In == "body" {
			return p, true
		}
	}
	return Param{}, false
}

// ByID returns the operation for id, or nil.
func (c *Catalog) ByID(id string) *Operation {
	if c.byID == nil {
		c.index()
	}
	return c.byID[id]
}

// IndexLine returns a one-line summary for context packs.
func (c *Catalog) IndexLine() string {
	var b []byte
	for i, op := range c.Operations {
		if i > 0 {
			b = append(b, ';')
		}
		b = append(b, op.ID...)
	}
	return string(b)
}

func (c *Catalog) index() {
	c.byID = make(map[string]*Operation, len(c.Operations))
	for i := range c.Operations {
		c.byID[c.Operations[i].ID] = &c.Operations[i]
	}
}

// SelectServer points every operation at the named server.
// An empty name keeps the first server URL.
func (c *Catalog) SelectServer(name string) error {
	if c == nil || name == "" {
		return nil
	}
	for i := range c.Operations {
		op := &c.Operations[i]
		if len(op.Servers) <= 1 {
			continue
		}
		var url string
		for _, s := range op.Servers {
			if s.Name == name || s.URL == name {
				url = s.URL
			}
		}
		if url == "" {
			return fmt.Errorf("operation %s has no server %q", op.ID, name)
		}
		op.BaseURL = url
	}
	c.index()
	return nil
}

// Finalize sorts operations by id, then builds indexes and the capability graph.
func (c *Catalog) Finalize() {
	sort.Slice(c.Operations, func(i, j int) bool { return c.Operations[i].ID < c.Operations[j].ID })
	c.index()
	c.Graph = BuildGraph(c.Operations, c.Links, c.Uses)
}
