package catalog

import (
	"fmt"
	"slices"
	"sort"
	"strings"
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
		URL  string `json:"URL"`
		Name string `json:"Name"`
	}

	// Auth is one scheme in a security requirement.
	// Header is set for header placement. Query is set for an apiKey in query.
	// UserHeader is set when the contract names a separate header for the person token.
	Auth struct {
		Name       string   `json:"Name"`
		Header     string   `json:"Header"`
		Query      string   `json:"Query"`
		Kind       string   `json:"Kind"`
		Scopes     []string `json:"Scopes"`
		UserHeader string   `json:"UserHeader"`
		Type       string   `json:"-"` // OpenAPI security scheme type
		Scheme     string   `json:"-"` // OpenAPI http scheme, such as basic
	}

	Param struct {
		Name        string `json:"Name"`
		In          string `json:"In"` // path, query, header, body
		Required    bool   `json:"Required"`
		Description string `json:"Description"`
		Schema      string `json:"Schema"`
		MediaType   string `json:"MediaType"`
		Default     string `json:"Default"`
		Style       string `json:"Style"`
		Explode     bool   `json:"Explode"`
	}

	Operation struct {
		ID                   string            `json:"ID"`
		Name                 string            `json:"Name"`
		Summary              string            `json:"-"`
		Description          string            `json:"Description"`
		IDFallback           bool              `json:"-"`
		IDCollision          string            `json:"-"`
		Group                string            `json:"Group"`
		Kind                 Kind              `json:"Kind"`
		Method               string            `json:"Method"`
		PathTemplate         string            `json:"PathTemplate"`
		Params               []Param           `json:"Params"`
		ResponseSummary      string            `json:"ResponseSummary"`
		SideEffect           SideEffect        `json:"SideEffect"`
		RequiresConfirmation bool              `json:"RequiresConfirmation"`
		Permissions          []string          `json:"Permissions"`
		Idempotency          string            `json:"Idempotency"`
		Retry                string            `json:"Retry"`
		Exposure             string            `json:"Exposure"`
		BaseURL              string            `json:"BaseURL"`
		Servers              []Server          `json:"Servers"`
		Page                 map[string]string `json:"Page"`
		Auth                 []Auth            `json:"Auth"`
		Requirements         [][]Auth          `json:"Requirements"` // OR of AND groups. Auth is the first group.
		Tags                 []string          `json:"Tags"`
		ResponseFields       []string          `json:"ResponseFields"`
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

// AuthSchemes returns each scheme name once, across every requirement.
func (op Operation) AuthSchemes() []Auth {
	if len(op.Requirements) == 0 {
		return op.Auth
	}
	seen := map[string]bool{}
	var out []Auth
	for _, group := range op.Requirements {
		for _, a := range group {
			if a.Name == "" || seen[a.Name] {
				continue
			}
			seen[a.Name] = true
			out = append(out, a)
		}
	}
	return out
}

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
	slices.SortFunc(c.Operations, func(a, b Operation) int { return strings.Compare(a.ID, b.ID) })
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
