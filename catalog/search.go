package catalog

import (
	"sort"
	"strings"
)

const hitLimit = 8

type (
	Match struct {
		Operation Operation `json:"Operation"`
		Related   []string  `json:"Related"`
	}

	scored struct {
		op    Operation
		score int
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
	var hits []scored
	for _, op := range cat.Operations {
		s := scoreOp(cat, op, q, synonyms[op.ID])
		if s <= 0 {
			continue
		}
		hits = append(hits, scored{op: op, score: s})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].op.ID < hits[j].op.ID
	})
	return pageHits(cat, hits, offset, limit)
}

func pageHits(cat *Catalog, hits []scored, offset, limit int) []Match {
	if limit <= 0 {
		limit = hitLimit
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(hits) {
		return nil
	}
	end := min(offset+limit, len(hits))
	out := make([]Match, 0, end-offset)
	for _, h := range hits[offset:end] {
		out = append(out, hit(cat, h.op))
	}
	return out
}

func scoreOp(cat *Catalog, op Operation, q string, syns []string) int {
	id := strings.ToLower(op.ID)
	if id == q || strings.ToLower(op.Name) == q {
		return 1000
	}
	score := 0
	for _, syn := range syns {
		if strings.EqualFold(syn, q) {
			score += 800
		}
	}
	if containsFold(op.ID, q) || containsFold(op.Name, q) || containsFold(op.Description, q) || containsFold(op.Group, q) {
		score += 100
	}
	for _, tag := range op.Tags {
		if containsFold(tag, q) || containsFold(q, tag) {
			score += 100
		}
	}
	if usesSchema(cat, op.ID, q) || relationMatch(cat, op.ID, q) {
		score += 40
	}
	for tok := range strings.FieldsSeq(q) {
		if len(tok) < 3 {
			continue
		}
		if containsFold(op.ID, tok) || containsFold(op.Name, tok) || containsFold(op.Description, tok) || containsFold(op.Group, tok) {
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

func hit(cat *Catalog, op Operation) Match {
	return Match{Operation: op, Related: cat.Graph.Related(op.ID)}
}

func relationMatch(cat *Catalog, operationID, q string) bool {
	for _, e := range cat.Graph.Edges {
		if e.From != operationID || (e.Kind != EdgeLinks && e.Kind != EdgeRelates) {
			continue
		}
		if containsFold(e.Note, q) || containsFold(e.To, q) {
			return true
		}
	}
	return false
}

func usesSchema(cat *Catalog, operationID, q string) bool {
	for _, u := range cat.Uses {
		if u.OperationID == operationID && containsFold(u.Name, q) {
			return true
		}
	}
	return false
}

func containsFold(hay, needle string) bool {
	return strings.Contains(strings.ToLower(hay), needle)
}
