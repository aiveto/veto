package runctx

import (
	"maps"
	"sort"
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
		Role    string
		Content string
	}

	Pack struct {
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

// The index is cut first. Turns, the description, and pending parameters follow.
func (b *Builder) limit(p *Pack) {
	recount(p)
	if p.Bytes <= b.MaxBytes {
		return
	}
	room := b.MaxBytes - (p.Bytes - len(p.Index))
	index, cut := fitIndex(p.Index, room)
	p.Index = index
	if cut {
		p.Truncated = true
	}
	recount(p)
	if p.Bytes <= b.MaxBytes {
		return
	}
	p.Truncated = true
	trimTurns(p, b.MaxBytes)
	trimDetail(p, b.MaxBytes)
	trimPending(p, b.MaxBytes)
	recount(p)
}

func recount(p *Pack) {
	p.Bytes = len(p.Rules) + len(p.Index) + len(p.DescribedDetail)
	for _, t := range p.Turns {
		p.Bytes += len(t.Role) + len(t.Content)
	}
	for _, rel := range p.Related {
		p.Bytes += len(rel)
	}
	p.Bytes += pendingBytes(p.PendingConfirmation)
}

func trimTurns(p *Pack, max int) {
	if p.Bytes <= max || len(p.Turns) == 0 {
		return
	}
	p.Turns = cloneTurns(p.Turns)
	for i := range p.Turns {
		if p.Bytes <= max {
			return
		}
		content := p.Turns[i].Content
		if content == "" {
			continue
		}
		keep := len(content) - (p.Bytes - max)
		if keep < 0 {
			keep = 0
		}
		next := prefixBytes(content, keep)
		p.Bytes -= len(content) - len(next)
		p.Turns[i].Content = next
	}
}

func trimDetail(p *Pack, max int) {
	if p.Bytes <= max || p.DescribedDetail == "" {
		return
	}
	keep := len(p.DescribedDetail) - (p.Bytes - max)
	if keep < 0 {
		keep = 0
	}
	next := prefixBytes(p.DescribedDetail, keep)
	p.Bytes -= len(p.DescribedDetail) - len(next)
	p.DescribedDetail = next
}

func trimPending(p *Pack, max int) {
	pend := p.PendingConfirmation
	if pend == nil || p.Bytes <= max || len(pend.Params) == 0 {
		return
	}
	detachPending(p)
	pend = p.PendingConfirmation
	keys := make([]string, 0, len(pend.Params))
	for k := range pend.Params {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(pend.Params[keys[i]]) != len(pend.Params[keys[j]]) {
			return len(pend.Params[keys[i]]) > len(pend.Params[keys[j]])
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		if p.Bytes <= max {
			return
		}
		value := pend.Params[k]
		if value == "" {
			continue
		}
		keep := len(value) - (p.Bytes - max)
		if keep < 0 {
			keep = 0
		}
		next := prefixBytes(value, keep)
		p.Bytes -= len(value) - len(next)
		pend.Params[k] = next
	}
}

func detachPending(p *Pack) {
	src := p.PendingConfirmation
	if src == nil {
		return
	}
	cp := *src
	if src.Params != nil {
		cp.Params = maps.Clone(src.Params)
	}
	p.PendingConfirmation = &cp
}

func cloneTurns(in []Turn) []Turn {
	out := make([]Turn, len(in))
	copy(out, in)
	return out
}

func pendingBytes(pend *policy.PendingConfirmation) int {
	if pend == nil {
		return 0
	}
	n := len(pend.ID) + len(pend.OperationID)
	for k, v := range pend.Params {
		n += len(k) + len(v)
	}
	return n
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
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role == "user" {
			return strings.TrimSpace(turns[i].Content)
		}
	}
	return ""
}
