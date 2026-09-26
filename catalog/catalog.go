package catalog

// Kind classifies what an operation does to server state.
type Kind string

const (
	KindRead   Kind = "read"
	KindCreate Kind = "create"
	KindUpdate Kind = "update"
	KindDelete Kind = "delete"
	KindAction Kind = "action"
)

// SideEffect describes how invasive an operation is.
type SideEffect string

const (
	SideEffectNone        SideEffect = "none"
	SideEffectWrite       SideEffect = "write"
	SideEffectDestructive SideEffect = "destructive"
)

// Exposure is how an operation may appear beyond search, describe, and invoke.
const (
	ExposureDirect    = "direct"
	ExposureGrouped   = "grouped"
	ExposureDiscovery = "discovery-only"
)

// Param is one request parameter from the contract.
type Param struct {
	Name        string
	In          string // path, query, header
	Required    bool
	Description string
	Schema      string
}

// Operation is one callable capability from the contract catalog.
type Operation struct {
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
}

// OpLink is an OpenAPI link from one operation to another.
type OpLink struct {
	From string
	To   string
	Note string
}

// SchemaUse records a component schema referenced by an operation.
type SchemaUse struct {
	OperationID string
	Name        string
}

// Catalog holds all operations and the capability graph for one contract.
type Catalog struct {
	Title      string
	Version    string
	Operations []Operation
	Links      []OpLink
	Uses       []SchemaUse
	Graph      Graph
	byID       map[string]*Operation
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

// Finalize builds indexes and the capability graph after load.
func (c *Catalog) Finalize() {
	c.index()
	c.Graph = BuildGraph(c.Operations, c.Links, c.Uses)
}
