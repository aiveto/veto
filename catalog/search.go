package catalog

import (
	"strings"
)

// Match describes one search hit. Related are the graph neighbors veto already walked.
type Match struct {
	Operation Operation
	Related   []string
}

// Search finds operations whose id, description, group, or synonyms contain query.
func Search(cat *Catalog, query string, synonyms map[string][]string) []Match {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var out []Match
	for _, op := range cat.Operations {
		if containsFold(op.ID, q) || containsFold(op.Description, q) || containsFold(op.Group, q) || containsFold(op.Name, q) || usesSchema(cat, op.ID, q) || relationMatch(cat, op.ID, q) {
			out = append(out, hit(cat, op))
			continue
		}
		for _, syn := range synonyms[op.ID] {
			if containsFold(syn, q) || containsFold(q, syn) {
				out = append(out, hit(cat, op))
				break
			}
		}
	}
	return out
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
