package catalog

import (
	"cmp"
	"slices"
	"strings"
)

const hitLimit = 8

type (
	Match struct {
		Operation Operation `json:"Operation"`
		Related   []string  `json:"Related"`
	}

	scored struct {
		i     int
		score int
	}

	searchText struct {
		id, name, desc, group string
		tags                  []string
		schemas               []string
		rel                   []string
		related               []string
	}
)

// An exact id outranks a synonym. A synonym outranks a weaker substring.
func Search(cat *Catalog, query string, synonyms map[string][]string) []Match {
	return SearchPage(cat, query, synonyms, 0, hitLimit)
}

func SearchPage(cat *Catalog, query string, synonyms map[string][]string, offset, limit int) []Match {
	q := strings.ToLower(strings.TrimSpace(query))
	if cat == nil || q == "" {
		return nil
	}
	if !cat.ready.Load() {
		cat.ensureIndex()
	}
	var hits []scored
	for i, op := range cat.Operations {
		s := scoreOp(cat.search[i], q, synonyms[op.ID])
		if s <= 0 {
			continue
		}
		hits = append(hits, scored{i: i, score: s})
	}
	slices.SortFunc(hits, func(a, b scored) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		return strings.Compare(cat.Operations[a.i].ID, cat.Operations[b.i].ID)
	})
	return pageHits(cat, hits, offset, limit)
}

func pageHits(cat *Catalog, hits []scored, offset, limit int) []Match {
	if limit <= 0 || limit > hitLimit {
		limit = hitLimit
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(hits) {
		return nil
	}
	remain := len(hits) - offset
	if limit > remain {
		limit = remain
	}
	out := make([]Match, 0, limit)
	for _, h := range hits[offset : offset+limit] {
		op := cat.Operations[h.i]
		related := []string{}
		if h.i < len(cat.search) {
			related = slices.Clone(cat.search[h.i].related)
			if related == nil {
				related = []string{}
			}
		}
		out = append(out, Match{Operation: op, Related: related})
	}
	return out
}

func scoreOp(text searchText, q string, syns []string) int {
	if text.id == q || text.name == q {
		return 1000
	}
	score := 0
	for _, syn := range syns {
		if strings.EqualFold(syn, q) {
			score += 800
		}
	}
	if strings.Contains(text.id, q) || strings.Contains(text.name, q) || strings.Contains(text.desc, q) || strings.Contains(text.group, q) {
		score += 100
	}
	for _, tag := range text.tags {
		if strings.Contains(tag, q) || strings.Contains(q, tag) {
			score += 100
		}
	}
	if usesPrepared(text.schemas, q) || usesPrepared(text.rel, q) {
		score += 40
	}
	for tok := range strings.FieldsSeq(q) {
		if len(tok) < 3 {
			continue
		}
		if strings.Contains(text.id, tok) || strings.Contains(text.name, tok) || strings.Contains(text.desc, tok) || strings.Contains(text.group, tok) {
			score += 20
		}
		for _, syn := range syns {
			if strings.EqualFold(syn, tok) {
				score += 30
			}
		}
	}
	return score
}

func usesPrepared(hay []string, q string) bool {
	for _, s := range hay {
		if strings.Contains(s, q) {
			return true
		}
	}
	return false
}

func (c *Catalog) prepareSearch() {
	schemas := map[string][]string{}
	for _, u := range c.Uses {
		if u.Name == "" {
			continue
		}
		schemas[u.OperationID] = append(schemas[u.OperationID], strings.ToLower(u.Name))
	}
	rel := map[string][]string{}
	related := map[string][]string{}
	for _, e := range c.Graph.Edges {
		if e.Kind != EdgeLinks && e.Kind != EdgeRelates {
			continue
		}
		related[e.From] = append(related[e.From], e.To)
		if e.Note != "" {
			rel[e.From] = append(rel[e.From], strings.ToLower(e.Note))
		}
		rel[e.From] = append(rel[e.From], strings.ToLower(e.To))
	}
	c.search = make([]searchText, len(c.Operations))
	for i, op := range c.Operations {
		tags := make([]string, len(op.Tags))
		for j, tag := range op.Tags {
			tags[j] = strings.ToLower(tag)
		}
		ids := related[op.ID]
		if ids == nil {
			ids = []string{}
		}
		c.search[i] = searchText{
			id:      strings.ToLower(op.ID),
			name:    strings.ToLower(op.Name),
			desc:    strings.ToLower(op.Description),
			group:   strings.ToLower(op.Group),
			tags:    tags,
			schemas: schemas[op.ID],
			rel:     rel[op.ID],
			related: ids,
		}
	}
}
