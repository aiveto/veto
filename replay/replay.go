package replay

import (
	"fmt"
	"sort"
	"strings"

	"github.com/aiveto/veto/telemetry"
)

// kept is the only attributes replay prints while redaction is on.
var kept = map[string]bool{
	"operation.id": true,
	"decision":     true,
	"http.method":  true,
	"http.status":  true,
	"approval.id":  true,
	"flow.name":    true,
	"tools":        true,
}

type (
	// Step is one recorded span after redaction.
	Step struct {
		Name  string
		Attrs map[string]string
	}

	// View is a run read back from traces.
	View struct {
		Steps []Step
	}
)

// FromSpans builds a view. When redact is set, only the allowlisted attributes are kept.
func FromSpans(spans []telemetry.Span, redact bool) View {
	ordered := append([]telemetry.Span(nil), spans...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Start.Equal(ordered[j].Start) {
			return ordered[i].Name < ordered[j].Name
		}
		return ordered[i].Start.Before(ordered[j].Start)
	})
	view := View{Steps: make([]Step, 0, len(ordered))}
	for _, sp := range ordered {
		attrs := map[string]string{}
		for k, v := range sp.Attrs {
			if redact && !kept[k] {
				continue
			}
			attrs[k] = v
		}
		view.Steps = append(view.Steps, Step{Name: sp.Name, Attrs: attrs})
	}
	return view
}

// String renders one line per step, attributes sorted by key.
func (v View) String() string {
	var b strings.Builder
	for _, s := range v.Steps {
		b.WriteString(s.Name)
		keys := make([]string, 0, len(s.Attrs))
		for k := range s.Attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, " %s=%s", k, s.Attrs[k])
		}
		b.WriteByte('\n')
	}
	return b.String()
}
