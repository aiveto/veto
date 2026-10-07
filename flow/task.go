package flow

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/aiveto/veto/catalog"
)

// Teach fills bindings for a flow that has a question. A flow without a question is returned as it was parsed.
func Teach(def *Definition, cat *catalog.Catalog) (*Definition, error) {
	if def == nil {
		return nil, errors.New("task is missing")
	}
	if strings.TrimSpace(def.Question) == "" {
		return def, nil
	}
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return nil, errors.New("task needs a name")
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
			from, field, _ := bindParts(bind)
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
		for field := range strings.SplitSeq(answer, ",") {
			field = strings.TrimSpace(field)
			if field != "" {
				fact.answer = append(fact.answer, field)
			}
		}
	}
	if bindsPart != "" {
		for bind := range strings.SplitSeq(bindsPart, " | ") {
			bind = strings.TrimSpace(bind)
			if bind == "" {
				continue
			}
			fact.binds = append(fact.binds, bind)
			_, _, to := bindParts(bind)
			if to != "" {
				fact.last = to
			}
		}
	}
	return name, fact, true
}

func bindParts(bind string) (from, field, to string) {
	left, right, ok := strings.Cut(bind, " -> ")
	if !ok {
		return "", "", ""
	}
	from, field, _ = strings.Cut(left, " ")
	to, _, _ = strings.Cut(right, " ")
	return strings.TrimSpace(from), strings.TrimSpace(field), strings.TrimSpace(to)
}

// Suggest returns a read-only task the catalog can already teach.
func Suggest(cat *catalog.Catalog) *Definition {
	reads := readOps(cat)
	if len(reads) == 0 {
		return nil
	}
	type pair struct{ from, to string }
	var pairs []pair
	seen := map[string]bool{}
	for _, link := range cat.Links {
		if !strings.Contains(link.Note, ".") {
			continue
		}
		key := link.From + "\x00" + link.To
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(linksBetween(link.From, link.To, cat.Links)) != 1 {
			continue
		}
		if !hasReadFields(cat, link.From) || !hasReadFields(cat, link.To) {
			continue
		}
		pairs = append(pairs, pair{link.From, link.To})
	}
	slices.SortFunc(pairs, func(a, b pair) int {
		return strings.Compare(a.from+" "+a.to, b.from+" "+b.to)
	})
	for _, p := range pairs {
		if def := teachable(cat, p.from, p.to); def != nil {
			return def
		}
	}
	for _, op := range reads {
		if def := teachable(cat, op.ID); def != nil {
			return def
		}
	}
	return nil
}

func readOps(cat *catalog.Catalog) []catalog.Operation {
	if cat == nil {
		return nil
	}
	var out []catalog.Operation
	for _, op := range cat.Operations {
		if op.Kind == catalog.KindRead && len(op.ResponseFields) > 0 {
			out = append(out, op)
		}
	}
	slices.SortFunc(out, func(a, b catalog.Operation) int {
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func hasReadFields(cat *catalog.Catalog, id string) bool {
	op := cat.ByID(id)
	return op != nil && op.Kind == catalog.KindRead && len(op.ResponseFields) > 0
}

func teachable(cat *catalog.Catalog, ids ...string) *Definition {
	last := cat.ByID(ids[len(ids)-1])
	if last == nil || len(last.ResponseFields) == 0 {
		return nil
	}
	fields := slices.Clone(last.ResponseFields)
	slices.Sort(fields)
	steps := make([]Step, len(ids))
	for i, id := range ids {
		steps[i] = Step{Operation: id}
	}
	question := strings.TrimSpace(last.Summary)
	if question == "" {
		question = "Read " + last.ID
	}
	def := &Definition{
		Name:     taskName(cat, last.ID),
		Question: question,
		Answer:   []string{fields[0]},
		Steps:    steps,
	}
	if _, err := Teach(def, cat); err != nil {
		return nil
	}
	return def
}

func taskName(cat *catalog.Catalog, id string) string {
	name := "read-" + strings.NewReplacer(".", "-", "/", "-").Replace(id)
	if cat.ByID(name) != nil {
		return "task-" + name
	}
	return name
}
