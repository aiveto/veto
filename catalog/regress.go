package catalog

import "sort"

// OpFact is one operation as veto check compares it across revisions.
type OpFact struct {
	Confirmation bool `json:"confirmation"`
	Destructive  bool `json:"destructive"`
	Referenced   bool `json:"referenced"`
}

// Facts records confirmation, destructive calls, and relation or link endpoints.
func Facts(cat *Catalog) map[string]OpFact {
	out := map[string]OpFact{}
	if cat == nil {
		return out
	}
	ref := map[string]bool{}
	for _, e := range cat.Graph.Edges {
		if e.Kind != EdgeLinks && e.Kind != EdgeRelates {
			continue
		}
		ref[e.From] = true
		ref[e.To] = true
	}
	for _, op := range cat.Operations {
		out[op.ID] = OpFact{
			Confirmation: op.RequiresConfirmation,
			Destructive:  op.SideEffect == SideEffectDestructive || op.Kind == KindDelete,
			Referenced:   ref[op.ID],
		}
	}
	return out
}

// SurfaceRegressions reports joined operations that disappeared and destructive
// calls that lost confirmation. confirmationChanged lists operations whose
// agent.yaml confirmation field changed on purpose.
func SurfaceRegressions(base, next map[string]OpFact, confirmationChanged map[string]bool) []string {
	var out []string
	for id, fact := range base {
		cur, ok := next[id]
		if fact.Referenced && !ok {
			out = append(out, "operation "+id+" referenced by a relation or link was removed")
			continue
		}
		if !ok {
			continue
		}
		if fact.Destructive && fact.Confirmation && !cur.Confirmation && !confirmationChanged[id] {
			out = append(out, "operation "+id+" lost confirmation")
		}
	}
	sort.Strings(out)
	return out
}
