package eval

import (
	"slices"
	"sort"
)

type CaseExpect struct {
	Name                 string   `json:"name"`
	Operation            string   `json:"operation"`
	ConfirmationRequired bool     `json:"confirmation_required"`
	NoHTTP               bool     `json:"no_http"`
	PackContains         []string `json:"pack_contains,omitempty"`
	PackExcludes         []string `json:"pack_excludes,omitempty"`
	Related              []string `json:"related,omitempty"`
}

func Expects(cases []*Case) []CaseExpect {
	out := make([]CaseExpect, 0, len(cases))
	for _, c := range cases {
		if c == nil {
			continue
		}
		out = append(out, CaseExpect{
			Name:                 c.Name,
			Operation:            c.Expect.OperationID,
			ConfirmationRequired: c.Expect.ConfirmationRequired,
			NoHTTP:               c.Expect.NoHTTP,
			PackContains:         slices.Clone(c.Expect.PackContains),
			PackExcludes:         slices.Clone(c.Expect.PackExcludes),
			Related:              slices.Clone(c.Expect.Related),
		})
	}
	return out
}

func Drift(base, next []CaseExpect) []string {
	byName := make(map[string]CaseExpect, len(next))
	for _, c := range next {
		byName[c.Name] = c
	}
	var out []string
	for _, b := range base {
		n, ok := byName[b.Name]
		if !ok {
			out = append(out, "eval case "+b.Name+" was removed")
			continue
		}
		if n.Operation != b.Operation || n.ConfirmationRequired != b.ConfirmationRequired || n.NoHTTP != b.NoHTTP || !slices.Equal(n.PackContains, b.PackContains) || !slices.Equal(n.PackExcludes, b.PackExcludes) || !slices.Equal(n.Related, b.Related) {
			out = append(out, "eval case "+b.Name+" changed expectation")
		}
	}
	sort.Strings(out)
	return out
}
