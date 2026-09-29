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

	// ExposureDirect is a pinned tool. ExposureGrouped is one tool for the resource. ExposureDiscovery stays on search and describe.
	ExposureDirect    = "direct"
	ExposureGrouped   = "grouped"
	ExposureDiscovery = "discovery-only"
)

type (
	Kind string

	SideEffect string

	Server struct {
		URL  string
		Name string
	}

	Auth struct {
		Name   string
		Header string
		Kind   string
	}

	Param struct {
		Name        string
		In          string // path, query, header, body
		Required    bool
		Description string
		Schema      string
	}

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

	OpLink struct {
		From   string
		To     string
		Note   string
		Params map[string]string
	}

	SchemaUse struct {
		OperationID string
		Name        string
	}

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

func (op Operation) BodyParam() (Param, bool) {
	for _, p := range op.Params {
		if p.In == "body" {
			return p, true
		}
	}
	return Param{}, false
}

func (c *Catalog) ByID(id string) *Operation {
	if c.byID == nil {
		c.index()
	}
	return c.byID[id]
}

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

func (c *Catalog) Finalize() {
	sort.Slice(c.Operations, func(i, j int) bool { return c.Operations[i].ID < c.Operations[j].ID })
	c.index()
	c.Graph = BuildGraph(c.Operations, c.Links, c.Uses)
}

func (c *Catalog) Joins() []string {
	if c == nil {
		return nil
	}
	var lines []string
	for _, e := range c.Graph.Edges {
		if e.Kind != EdgeLinks && e.Kind != EdgeRelates {
			continue
		}
		label := e.Note
		if label == "" {
			label = e.Kind
		}
		lines = append(lines, fmt.Sprintf("%s --[%s]--> %s", e.From, label, e.To))
	}
	sort.Strings(lines)
	return lines
}
