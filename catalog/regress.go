package catalog

import (
	"fmt"
	"sort"
	"strings"
)

type OpFact struct {
	Confirmation bool     `json:"confirmation"`
	Destructive  bool     `json:"destructive"`
	Referenced   bool     `json:"referenced"`
	Callable     *bool    `json:"callable,omitempty"`
	Permissions  []string `json:"permissions,omitempty"`
}

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
		perms := append([]string(nil), op.Permissions...)
		sort.Strings(perms)
		out[op.ID] = OpFact{
			Confirmation: op.RequiresConfirmation,
			Destructive:  op.SideEffect == SideEffectDestructive || op.Kind == KindDelete,
			Referenced:   ref[op.ID],
			Callable:     boolPtr(op.Exposure != ExposureDiscovery),
			Permissions:  perms,
		}
	}
	return out
}

func boolPtr(v bool) *bool {
	return &v
}

// confirmationChanged lists operations whose agent.yaml confirmation field changed on purpose.
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
		if fact.Callable != nil && !*fact.Callable && cur.Callable != nil && *cur.Callable {
			out = append(out, "operation "+id+" became callable")
		}
		if lost := removedPermissions(fact.Permissions, cur.Permissions); len(lost) > 0 {
			out = append(out, fmt.Sprintf("operation %s lost permission %s", id, strings.Join(lost, ", ")))
		}
	}
	sort.Strings(out)
	return out
}

func removedPermissions(base, next []string) []string {
	have := map[string]bool{}
	for _, p := range next {
		have[p] = true
	}
	var lost []string
	seen := map[string]bool{}
	for _, p := range base {
		if p == "" || have[p] || seen[p] {
			continue
		}
		seen[p] = true
		lost = append(lost, p)
	}
	sort.Strings(lost)
	return lost
}
