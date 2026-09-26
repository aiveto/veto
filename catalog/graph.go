package catalog

const (
	NodeResource  NodeKind = "resource"
	NodeOperation NodeKind = "operation"
	NodeSchema    NodeKind = "schema"

	EdgeOwns    = "owns"
	EdgeUses    = "uses"
	EdgeLinks   = "links"
	EdgeRelates = "relates"
)

type (
	// NodeKind distinguishes resource hubs, operations, and component schemas.
	NodeKind string

	// Node is one vertex in the capability graph.
	Node struct {
		ID   string
		Name string
		Kind NodeKind
	}

	// Edge links two nodes. Owns is resource to operation. Uses is operation to schema. Links is operation to operation.
	Edge struct {
		From string
		To   string
		Kind string
		Note string
	}

	// Graph connects resources to their operations.
	Graph struct {
		Nodes []Node
		Edges []Edge
	}
)

// BuildGraph derives resource, operation, and schema nodes, plus link edges.
func BuildGraph(ops []Operation, links []OpLink, uses []SchemaUse) Graph {
	resources := map[string]bool{}
	schemas := map[string]bool{}
	known := map[string]bool{}
	var nodes []Node
	var edges []Edge

	for _, op := range ops {
		known[op.ID] = true
		if op.Group != "" && !resources[op.Group] {
			resources[op.Group] = true
			nodes = append(nodes, Node{ID: op.Group, Name: op.Group, Kind: NodeResource})
		}
		nodes = append(nodes, Node{ID: op.ID, Name: op.Name, Kind: NodeOperation})
		if op.Group != "" {
			edges = append(edges, Edge{From: op.Group, To: op.ID, Kind: EdgeOwns})
		}
	}
	for _, u := range uses {
		if !known[u.OperationID] || u.Name == "" {
			continue
		}
		if !schemas[u.Name] {
			schemas[u.Name] = true
			nodes = append(nodes, Node{ID: u.Name, Name: u.Name, Kind: NodeSchema})
		}
		edges = append(edges, Edge{From: u.OperationID, To: u.Name, Kind: EdgeUses})
	}
	for _, l := range links {
		if !known[l.From] || !known[l.To] || l.From == l.To {
			continue
		}
		kind := EdgeLinks
		if l.Note != "" {
			kind = EdgeRelates
		}
		edges = append(edges, Edge{From: l.From, To: l.To, Kind: kind, Note: l.Note})
	}
	return Graph{Nodes: nodes, Edges: edges}
}

// OperationsForResource returns operation ids linked from the resource.
func (g Graph) OperationsForResource(resource string) []string {
	var out []string
	for _, e := range g.Edges {
		if e.From == resource {
			out = append(out, e.To)
		}
	}
	return out
}

// Related returns operation ids linked from operationID.
func (g Graph) Related(operationID string) []string {
	var out []string
	for _, e := range g.Edges {
		if (e.Kind == EdgeLinks || e.Kind == EdgeRelates) && e.From == operationID {
			out = append(out, e.To)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

// Schemas returns component schema names used by operationID.
func (g Graph) Schemas(operationID string) []string {
	var out []string
	for _, e := range g.Edges {
		if e.Kind == EdgeUses && e.From == operationID {
			out = append(out, e.To)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}
