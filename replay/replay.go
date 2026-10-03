// Package replay reads OpenTelemetry spans. User text stays out unless asked.
package replay

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/aiveto/veto/telemetry"
)

type (
	Step struct {
		Name  string            `json:"name"`
		Attrs map[string]string `json:"attrs,omitempty"`
	}

	View struct {
		Steps []Step `json:"steps"`
	}
)

// When redact is set, only the allowlisted attributes are kept.
func FromSpans(spans []telemetry.Span, redact bool) View {
	ordered := append([]telemetry.Span(nil), spans...)
	slices.SortFunc(ordered, func(a, b telemetry.Span) int {
		if c := a.Start.Compare(b.Start); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	view := View{Steps: make([]Step, 0, len(ordered))}
	for _, sp := range ordered {
		attrs := map[string]string{}
		for k, v := range sp.Attrs {
			if redact && !telemetry.Allowed(k) {
				continue
			}
			attrs[k] = v
		}
		view.Steps = append(view.Steps, Step{Name: sp.Name, Attrs: attrs})
	}
	return view
}

func (v View) String() string {
	var b strings.Builder
	for _, s := range v.Steps {
		b.WriteString(s.Name)
		for _, k := range slices.Sorted(maps.Keys(s.Attrs)) {
			fmt.Fprintf(&b, " %s=%s", k, s.Attrs[k])
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func Save(path string, view View) error {
	data, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return fmt.Errorf("write trace: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write trace: %w", err)
	}
	return nil
}

func Load(path string) (View, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return View{}, fmt.Errorf("read trace: %w", err)
	}
	var view View
	if err := json.Unmarshal(data, &view); err != nil {
		return View{}, fmt.Errorf("parse trace: %w", err)
	}
	return view, nil
}
