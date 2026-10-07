package flow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/aiveto/veto/catalog"
)

// Teach fills bindings for a flow that has a question. A flow without a question is returned as it was parsed.
func Teach(def *Definition, cat *catalog.Catalog) (*Definition, error) {
	if def == nil {
		return nil, fmt.Errorf("task is missing")
	}
	if strings.TrimSpace(def.Question) == "" {
		return def, nil
	}
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return nil, fmt.Errorf("task needs a name")
	}
	if cat == nil {
		return nil, fmt.Errorf("task %s: catalog is missing", name)
	}
	if cat.ByID(name) != nil {
		return nil, fmt.Errorf("task %s uses an operation id", name)
	}
	answer, err := answerFields(name, def.Answer)
	if err != nil {
		return nil, err
	}
	if len(def.Steps) == 0 {
		return nil, fmt.Errorf("task %s needs a step", name)
	}
	out := *def
	out.Name = name
	out.Question = strings.TrimSpace(def.Question)
	out.Answer = answer
	out.Steps = slices.Clone(def.Steps)
	for i := range out.Steps {
		op := cat.ByID(out.Steps[i].Operation)
		if op == nil {
			return nil, fmt.Errorf("task %s: unknown operation %s", name, out.Steps[i].Operation)
		}
		if op.Kind != catalog.KindRead {
			return nil, fmt.Errorf("task %s: %s is not read-only", name, op.ID)
		}
		if i == 0 {
			continue
		}
		prev := cat.ByID(out.Steps[i-1].Operation)
		bound, err := bind(name, *prev, *op, out.Steps[i-1], cat.Links)
		if err != nil {
			return nil, err
		}
		out.Steps[i-1] = bound
	}
	last := cat.ByID(out.Steps[len(out.Steps)-1].Operation)
	if err := requireAnswers(name, *last, out.Answer); err != nil {
		return nil, err
	}
	return &out, nil
}

// Binds lists each value the task copies, as source field to target parameter.
func (d *Definition) Binds() []string {
	if d == nil || len(d.Steps) < 2 {
		return nil
	}
	var out []string
	for i := 1; i < len(d.Steps); i++ {
		prev := d.Steps[i-1]
		if prev.Output == "" {
			continue
		}
		to := prev.To
		if to == "" {
			to = prev.Output
		}
		out = append(out, prev.Operation+" "+prev.Output+" -> "+d.Steps[i].Operation+" "+to)
	}
	return out
}

// IndexLine is the task sentence search and the context pack share.
func (d *Definition) IndexLine() string {
	if d == nil {
		return ""
	}
	head := "task " + d.Name + ": " + d.Question + ". answer: " + strings.Join(d.Answer, ", ")
	binds := d.Binds()
	if len(binds) == 0 {
		return head
	}
	return head + " | " + strings.Join(binds, " | ")
}

// Lines returns one index line per task, sorted by name.
func Lines(flows map[string]*Definition) []string {
	if len(flows) == 0 {
		return nil
	}
	names := make([]string, 0, len(flows))
	for name, def := range flows {
		if def == nil || strings.TrimSpace(def.Question) == "" {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, flows[name].IndexLine())
	}
	return out
}

// Find returns tasks whose name, question, or answer matches the query.
func Find(flows map[string]*Definition, query string) []*Definition {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" || len(flows) == 0 {
		return nil
	}
	var names []string
	for name, def := range flows {
		if def == nil || strings.TrimSpace(def.Question) == "" {
			continue
		}
		head, _, _ := strings.Cut(strings.ToLower(def.IndexLine()), " | ")
		if strings.Contains(head, q) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	out := make([]*Definition, 0, len(names))
	for _, name := range names {
		out = append(out, flows[name])
	}
	return out
}

func answerFields(task string, fields []string) ([]string, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("task %s needs an answer field", task)
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			return nil, fmt.Errorf("task %s needs an answer field", task)
		}
		out = append(out, field)
	}
	return out, nil
}

func requireAnswers(task string, op catalog.Operation, fields []string) error {
	if len(op.ResponseFields) == 0 {
		return fmt.Errorf("task %s: %s does not list response fields", task, op.ID)
	}
	var missing []string
	for _, field := range fields {
		if !slices.Contains(op.ResponseFields, field) {
			missing = append(missing, field)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("task %s can no longer read %s from %s", task, strings.Join(missing, ", "), op.ID)
}

func bind(task string, from, to catalog.Operation, step Step, links []catalog.OpLink) (Step, error) {
	if strings.TrimSpace(step.Output) == "" {
		found := linksBetween(from.ID, to.ID, links)
		if len(found) == 0 {
			return Step{}, fmt.Errorf("task %s: no relation binds %s to %s", task, from.ID, to.ID)
		}
		if len(found) > 1 {
			return Step{}, fmt.Errorf("task %s: %s has more than one relation to %s; set output", task, from.ID, to.ID)
		}
		note := found[0].Note
		step.Output = note[strings.LastIndex(note, ".")+1:]
		param, err := targetParam(to, step.Output)
		if err != nil {
			return Step{}, fmt.Errorf("task %s: %w", task, err)
		}
		step.To = param
	}
	return checkBind(task, from, to, step)
}

func linksBetween(from, to string, links []catalog.OpLink) []catalog.OpLink {
	var found []catalog.OpLink
	for _, link := range links {
		if link.From != from || link.To != to || !strings.Contains(link.Note, ".") {
			continue
		}
		found = append(found, link)
	}
	return found
}

func checkBind(task string, from, to catalog.Operation, step Step) (Step, error) {
	field := strings.TrimSpace(step.Output)
	if len(from.ResponseFields) == 0 {
		return Step{}, fmt.Errorf("task %s: %s does not list response fields, so %s cannot be checked", task, from.ID, field)
	}
	if !slices.Contains(from.ResponseFields, field) {
		return Step{}, fmt.Errorf("task %s can no longer obtain %s from %s", task, field, from.ID)
	}
	param := strings.TrimSpace(step.To)
	if param == "" {
		param = field
	}
	if !hasParam(to, param) {
		return Step{}, fmt.Errorf("task %s: %s has no parameter %s", task, to.ID, param)
	}
	step.Output = field
	step.To = param
	return step, nil
}

func targetParam(op catalog.Operation, field string) (string, error) {
	var path []string
	for _, p := range op.Params {
		if p.In != "path" && p.In != "query" {
			continue
		}
		if p.Name == field {
			return p.Name, nil
		}
		if p.Required && p.In == "path" {
			path = append(path, p.Name)
		}
	}
	if len(path) == 1 {
		return path[0], nil
	}
	return "", fmt.Errorf("%s has no parameter for %s", op.ID, field)
}

func hasParam(op catalog.Operation, name string) bool {
	for _, p := range op.Params {
		if p.Name == name && (p.In == "path" || p.In == "query") {
			return true
		}
	}
	return false
}

// TaskRegressions reports a task the baseline could teach and the current lines cannot.
func TaskRegressions(base, next []string) []string {
	before := parseTaskLines(base)
	after := parseTaskLines(next)
	var out []string
	for name, old := range before {
		cur, ok := after[name]
		if !ok {
			out = append(out, "task "+name+" was removed")
			continue
		}
		for _, bind := range old.binds {
			if slices.Contains(cur.binds, bind) {
				continue
			}
			from, field, _, _ := bindParts(bind)
			if from == "" || field == "" {
				out = append(out, "task "+name+" lost binding "+bind)
				continue
			}
			out = append(out, "task "+name+" can no longer obtain "+field+" from "+from)
		}
		from := cur.last
		if from == "" {
			from = old.last
		}
		for _, field := range old.answer {
			if slices.Contains(cur.answer, field) {
				continue
			}
			if from == "" {
				out = append(out, "task "+name+" can no longer read "+field)
				continue
			}
			out = append(out, "task "+name+" can no longer read "+field+" from "+from)
		}
	}
	slices.Sort(out)
	return out
}

type taskLine struct {
	answer []string
	binds  []string
	last   string
}

func parseTaskLines(lines []string) map[string]taskLine {
	out := map[string]taskLine{}
	for _, line := range lines {
		name, fact, ok := parseTaskLine(line)
		if !ok || name == "" {
			continue
		}
		out[name] = fact
	}
	return out
}

func parseTaskLine(line string) (string, taskLine, bool) {
	const prefix = "task "
	if !strings.HasPrefix(line, prefix) {
		return "", taskLine{}, false
	}
	rest := strings.TrimPrefix(line, prefix)
	name, rest, ok := strings.Cut(rest, ":")
	if !ok {
		return "", taskLine{}, false
	}
	name = strings.TrimSpace(name)
	head, bindsPart, _ := strings.Cut(rest, " | ")
	var fact taskLine
	if _, answer, ok := strings.Cut(head, ". answer: "); ok {
		for _, field := range strings.Split(answer, ",") {
			field = strings.TrimSpace(field)
			if field != "" {
				fact.answer = append(fact.answer, field)
			}
		}
	}
	if bindsPart != "" {
		for _, bind := range strings.Split(bindsPart, " | ") {
			bind = strings.TrimSpace(bind)
			if bind == "" {
				continue
			}
			fact.binds = append(fact.binds, bind)
			_, _, to, _ := bindParts(bind)
			if to != "" {
				fact.last = to
			}
		}
	}
	return name, fact, true
}

func bindParts(bind string) (from, field, to, param string) {
	left, right, ok := strings.Cut(bind, " -> ")
	if !ok {
		return "", "", "", ""
	}
	from, field, _ = strings.Cut(left, " ")
	to, param, _ = strings.Cut(right, " ")
	return strings.TrimSpace(from), strings.TrimSpace(field), strings.TrimSpace(to), strings.TrimSpace(param)
}
