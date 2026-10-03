// Package runctx builds the context pack for one turn.
package runctx

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/semantics"
)

const defaultRules = "Use capabilities_search, then capabilities_describe, then capabilities_invoke."

type (
	notes interface {
		Note(operationID string) semantics.Note
		AllSynonyms() map[string][]string
	}

	Turn struct {
		Role    string `json:"Role"`
		Content string `json:"Content"`
	}

	Pack struct {
		Rules                string                      `json:"Rules"`
		Index                string                      `json:"Index"`
		Turns                []Turn                      `json:"Turns"`
		DescribedOperationID string                      `json:"DescribedOperationID"`
		DescribedDetail      string                      `json:"DescribedDetail"`
		Related              []string                    `json:"Related"`
		PendingConfirmation  *policy.PendingConfirmation `json:"PendingConfirmation"`
		Bytes                int                         `json:"Bytes"`
		Truncated            bool                        `json:"Truncated"`
	}

	Builder struct {
		MaxBytes int
	}
)

func NewBuilder(maxBytes int) *Builder {
	if maxBytes <= 0 {
		maxBytes = 8192
	}
	return &Builder{MaxBytes: maxBytes}
}

// Build does not embed the raw OpenAPI document.
func (b *Builder) Build(cat *catalog.Catalog, turns []Turn, described *catalog.Operation, sem notes, pending *policy.PendingConfirmation) Pack {
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
	b.limit(&p)
	return p
}

// The index is cut first. Related lines are dropped whole. A pending approval that does not fit is omitted.
func (b *Builder) limit(p *Pack) {
	p.Bytes = len(p.Serialize())
	if p.Bytes <= b.MaxBytes {
		return
	}
	p.Truncated = true
	fullIndex := p.Index
	shrinkIndex(p, b.MaxBytes)
	for len(p.Related) > 0 && len(p.Serialize()) > b.MaxBytes {
		p.Related = p.Related[:len(p.Related)-1]
	}
	if p.Index != fullIndex {
		p.Index = fullIndex
		if len(p.Serialize()) > b.MaxBytes {
			shrinkIndex(p, b.MaxBytes)
		}
	}
	trimTurns(p, b.MaxBytes)
	trimDetail(p, b.MaxBytes)
	if len(p.Serialize()) > b.MaxBytes {
		p.PendingConfirmation = nil
	}
	trimFloor(p, b.MaxBytes)
	p.Bytes = len(p.Serialize())
}

func shrinkIndex(p *Pack, limit int) {
	overhead := len(p.Serialize()) - len(p.Index)
	room := max(limit-overhead, 0)
	index, cut := fitIndex(p.Index, room)
	p.Index = index
	if cut {
		p.Truncated = true
	}
}

func trimTurns(p *Pack, limit int) {
	if len(p.Serialize()) <= limit || len(p.Turns) == 0 {
		return
	}
	p.Turns = cloneTurns(p.Turns)
	for i := range p.Turns {
		if len(p.Serialize()) <= limit {
			return
		}
		content := p.Turns[i].Content
		overflow := len(p.Serialize()) - limit
		keep := max(len(content)-overflow, 0)
		p.Turns[i].Content = prefixBytes(content, keep)
	}
}

func trimDetail(p *Pack, limit int) {
	if len(p.Serialize()) <= limit || p.DescribedDetail == "" {
		return
	}
	overflow := len(p.Serialize()) - limit
	keep := max(len(p.DescribedDetail)-overflow, 0)
	p.DescribedDetail = prefixBytes(p.DescribedDetail, keep)
}

func trimFloor(p *Pack, limit int) {
	for len(p.Serialize()) > limit && p.Rules != "" {
		overflow := len(p.Serialize()) - limit
		keep := max(len(p.Rules)-overflow, 0)
		p.Rules = prefixBytes(p.Rules, keep)
	}
	for len(p.Serialize()) > limit && p.DescribedOperationID != "" {
		overflow := len(p.Serialize()) - limit
		keep := max(len(p.DescribedOperationID)-overflow, 0)
		p.DescribedOperationID = prefixBytes(p.DescribedOperationID, keep)
	}
	if len(p.Serialize()) <= limit || len(p.Turns) == 0 {
		return
	}
	p.Turns = nil
}

func cloneTurns(in []Turn) []Turn {
	out := make([]Turn, len(in))
	copy(out, in)
	return out
}

func prefixBytes(s string, n int) string {
	if n >= len(s) {
		return s
	}
	if n <= 0 {
		return ""
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func fitIndex(index string, budget int) (string, bool) {
	if budget < 0 {
		budget = 0
	}
	if len(index) <= budget {
		return index, false
	}
	parts := strings.Split(index, "; ")
	for len(parts) > 0 && len(strings.Join(parts, "; ")) > budget {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return "", true
	}
	return strings.Join(parts, "; "), true
}

// Serialize returns a human-readable pack for tests and debugging.
func (p Pack) Serialize() string {
	var parts []string
	if p.Rules != "" {
		parts = append(parts, "rules: "+p.Rules)
	}
	if p.Index != "" {
		parts = append(parts, "index: "+p.Index)
	}
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
		parts = append(parts, "pending_confirmation: "+policy.ConfirmSentence(p.PendingConfirmation.OperationID, p.PendingConfirmation.Params))
	}
	return strings.Join(parts, "\n")
}

func ContainsRawSpec(s string) bool {
	return strings.Contains(s, "openapi:") || strings.Contains(s, "\"openapi\"")
}

func selectedIndex(cat *catalog.Catalog, turns []Turn, described *catalog.Operation, sem notes) string {
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
	var hits []catalog.Match
	if query := lastUser(turns); query != "" {
		var syn map[string][]string
		if sem != nil {
			syn = sem.AllSynonyms()
		}
		hits = catalog.Search(cat, query, syn)
	}
	for _, m := range hits {
		add(m.Operation.ID)
	}
	if described != nil {
		add(described.ID)
	}
	for _, m := range hits {
		for _, rel := range m.Related {
			add(rel)
		}
	}
	if described != nil {
		for _, rel := range cat.Graph.Related(described.ID) {
			add(rel)
		}
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		op := cat.ByID(id)
		if op == nil {
			continue
		}
		note := ""
		if sem != nil {
			note = sem.Note(id).Text()
		}
		parts = append(parts, OperationLine(cat, *op, note))
	}
	return strings.Join(parts, "; ")
}

func OperationLine(cat *catalog.Catalog, op catalog.Operation, note string) string {
	var b strings.Builder
	b.WriteString(op.ID)
	if note != "" {
		b.WriteByte(' ')
		b.WriteString(note)
	}
	for _, p := range op.Params {
		b.WriteByte(' ')
		b.WriteString(p.Name)
		b.WriteByte(' ')
		b.WriteString(p.In)
		if p.Required || p.In == "path" {
			b.WriteString(" required")
		}
	}
	for _, field := range relationFields(cat, op) {
		b.WriteByte(' ')
		b.WriteString(field)
	}
	return b.String()
}

func relationFields(cat *catalog.Catalog, op catalog.Operation) []string {
	if cat == nil {
		return nil
	}
	have := map[string]bool{}
	for _, f := range op.ResponseFields {
		have[f] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, e := range cat.Graph.Edges {
		if e.From != op.ID || e.Kind != catalog.EdgeRelates {
			continue
		}
		field := e.Note
		if i := strings.LastIndex(field, "."); i >= 0 {
			field = field[i+1:]
		}
		if field == "" || !have[field] || seen[field] {
			continue
		}
		seen[field] = true
		out = append(out, field)
	}
	return out
}

func lastUser(turns []Turn) string {
	for _, turn := range slices.Backward(turns) {
		if turn.Role == "user" {
			return strings.TrimSpace(turn.Content)
		}
	}
	return ""
}
