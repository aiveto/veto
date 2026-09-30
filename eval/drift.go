package eval

import "sort"

type CaseExpect struct {
	Name                 string `json:"name"`
	Operation            string `json:"operation"`
	ConfirmationRequired bool   `json:"confirmation_required"`
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
		if n.Operation != b.Operation || n.ConfirmationRequired != b.ConfirmationRequired {
			out = append(out, "eval case "+b.Name+" changed operation or confirmation")
		}
	}
	sort.Strings(out)
	return out
}
