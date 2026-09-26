package runctx

import (
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/semantics"
)

const defaultRules = "Use capabilities_search, then capabilities_describe, then capabilities_invoke."

// Turn is one conversation message.
type Turn struct {
	Role    string
	Content string
}

// Pack is the bounded context given to the model.
type Pack struct {
	Rules                string
	Index                string
	Turns                []Turn
	DescribedOperationID string
	DescribedDetail      string
	Related              []string
	PendingConfirmation  *policy.PendingConfirmation
	Bytes                int
	Truncated            bool
}

// Builder constructs context packs from run inputs.
type Builder struct {
	MaxBytes int
}

// NewBuilder creates a pack builder with a byte budget.
func NewBuilder(maxBytes int) *Builder {
	if maxBytes <= 0 {
		maxBytes = 8192
	}
	return &Builder{MaxBytes: maxBytes}
}

// Build assembles a pack without embedding the raw OpenAPI document.
func (b *Builder) Build(cat *catalog.Catalog, turns []Turn, described *catalog.Operation, sem semantics.Provider, pending *policy.PendingConfirmation) Pack {
	p := Pack{
		Rules: defaultRules,
		Index: selectedIndex(cat, turns, described, sem),
		Turns: turns,
	}
	if described != nil {
		p.DescribedOperationID = described.ID
		n := sem.Note(described.ID)
		p.DescribedDetail = described.Method + " " + described.PathTemplate + ": " + n.Text()
		for _, e := range cat.Graph.Edges {
			if e.From != described.ID || (e.Kind != catalog.EdgeLinks && e.Kind != catalog.EdgeRelates) {
				continue
			}
			line := e.To
			if e.Note != "" {
				line += " " + e.Note
			}
			p.Related = append(p.Related, line)
		}
	}
	if pending != nil {
		p.PendingConfirmation = pending
	}
	p.Bytes = len(p.Rules) + len(p.Index)
	for _, t := range p.Turns {
		p.Bytes += len(t.Role) + len(t.Content)
	}
	p.Bytes += len(p.DescribedDetail)
	for _, rel := range p.Related {
		p.Bytes += len(rel)
	}
	if p.Bytes > b.MaxBytes {
		p.Truncated = true
		over := p.Bytes - b.MaxBytes
		if over < len(p.Index) {
			p.Index = truncate(p.Index, len(p.Index)-over)
		} else {
			p.Index = ""
		}
		p.Bytes = b.MaxBytes
	}
	return p
}

// Serialize returns a human-readable pack for tests and debugging.
func (p Pack) Serialize() string {
	var parts []string
	parts = append(parts, "rules: "+p.Rules)
	parts = append(parts, "index: "+p.Index)
	for _, t := range p.Turns {
		parts = append(parts, t.Role+": "+t.Content)
	}
	if p.DescribedOperationID != "" {
		parts = append(parts, "described: "+p.DescribedOperationID+" "+p.DescribedDetail)
	}
	for _, rel := range p.Related {
		parts = append(parts, "related: "+rel)
	}
	if p.PendingConfirmation != nil {
		parts = append(parts, "pending_confirmation: "+p.PendingConfirmation.OperationID)
	}
	return strings.Join(parts, "\n")
}

// ContainsRawSpec reports whether s looks like an OpenAPI root document.
func ContainsRawSpec(s string) bool {
	return strings.Contains(s, "openapi:") || strings.Contains(s, "\"openapi\"")
}

func selectedIndex(cat *catalog.Catalog, turns []Turn, described *catalog.Operation, sem semantics.Provider) string {
	if cat == nil {
		return ""
	}
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if id == "" || seen[id] || cat.ByID(id) == nil {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if query := lastUser(turns); query != "" {
		var syn map[string][]string
		if sem != nil {
			syn = sem.AllSynonyms()
		}
		for _, m := range catalog.Search(cat, query, syn) {
			add(m.Operation.ID)
			for _, rel := range m.Related {
				add(rel)
			}
		}
	}
	if described != nil {
		add(described.ID)
		for _, rel := range cat.Graph.Related(described.ID) {
			add(rel)
		}
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		line := id
		if sem != nil {
			if text := sem.Note(id).Text(); text != "" {
				line = id + " " + text
			}
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "; ")
}

func lastUser(turns []Turn) string {
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role == "user" {
			return strings.TrimSpace(turns[i].Content)
		}
	}
	return ""
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max]
}
