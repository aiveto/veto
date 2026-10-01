package openapi

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-openapi/jsonpointer"
	"gopkg.in/yaml.v3"
)

var pathNoun = regexp.MustCompile(`^/([a-zA-Z0-9_-]+)`)

type rawLink struct {
	from         string
	operationID  string
	operationRef string
	params       map[string]string
}

// Load reads an OpenAPI 3.0 or 3.1 document. Callbacks and webhooks are rejected.
// A document with either is not a complete catalog, so load fails instead of dropping them.
func Load(ctx context.Context, path string) (*catalog.Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read contract: %w", err)
	}
	if err := rejectDocument(data); err != nil {
		return nil, err
	}
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("parse openapi: %w", err)
	}
	if err := doc.Validate(ctx); err != nil {
		return nil, fmt.Errorf("validate openapi: %w", err)
	}

	cat := &catalog.Catalog{
		Title:   doc.Info.Title,
		Version: doc.Info.Version,
	}
	if cat.Title == "" {
		cat.Title = "api"
	}

	var raw []rawLink
	seenUse := map[string]bool{}
	drafts := draftOperations(doc)
	usedID := map[string]bool{}
	for _, d := range drafts {
		servers := operationServers(doc, d.item, d.op)
		operation := mapOperation(d.method, d.path, d.group, servers, d.item, d.op)
		natural := operation.ID
		id, collided := takeID(natural, usedID)
		operation.ID = id
		if collided {
			operation.IDCollision = natural
		}
		operation.Requirements = operationSecurity(doc, d.op)
		if len(operation.Requirements) > 0 {
			operation.Auth = operation.Requirements[0]
		}
		cat.Operations = append(cat.Operations, operation)
		collectUses(operation.ID, d.item, d.op, &cat.Uses, seenUse)
		raw = append(raw, collectLinks(operation.ID, d.op)...)
	}
	links, err := resolveLinks(doc, cat.Operations, raw)
	if err != nil {
		return nil, err
	}
	cat.Links = links
	for i := range cat.Operations {
		for _, l := range links {
			if l.From == cat.Operations[i].ID && l.To == cat.Operations[i].ID && len(l.Params) > 0 {
				cat.Operations[i].Page = l.Params
			}
		}
	}
	cat.Finalize()
	return cat, nil
}

func rejectDocument(data []byte) error {
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse openapi: %w", err)
	}
	if nonEmpty(root["webhooks"]) {
		return fmt.Errorf("webhooks are not loaded")
	}
	paths, _ := root["paths"].(map[string]any)
	for path, item := range paths {
		ops, _ := item.(map[string]any)
		for method, op := range ops {
			body, _ := op.(map[string]any)
			if nonEmpty(body["callbacks"]) {
				return fmt.Errorf("callbacks on %s %s are not loaded", strings.ToUpper(method), path)
			}
		}
	}
	return nil
}

func nonEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case map[string]any:
		return len(t) > 0
	case []any:
		return len(t) > 0
	default:
		return true
	}
}

type drafted struct {
	method string
	path   string
	group  string
	item   *openapi3.PathItem
	op     *openapi3.Operation
}

func draftOperations(doc *openapi3.T) []drafted {
	if doc == nil || doc.Paths == nil {
		return nil
	}
	var out []drafted
	for path, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		group := pathGroup(path)
		for method, op := range item.Operations() {
			if op == nil {
				continue
			}
			out = append(out, drafted{method: method, path: path, group: group, item: item, op: op})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].path != out[j].path {
			return out[i].path < out[j].path
		}
		return out[i].method < out[j].method
	})
	return out
}

func takeID(id string, used map[string]bool) (string, bool) {
	if !used[id] {
		used[id] = true
		return id, false
	}
	for n := 2; ; n++ {
		next := fmt.Sprintf("%s.%d", id, n)
		if !used[next] {
			used[next] = true
			return next, true
		}
	}
}

func pathGroup(path string) string {
	m := pathNoun.FindStringSubmatch(path)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func serverList(doc *openapi3.T) []catalog.Server {
	if doc == nil {
		return nil
	}
	return serversFrom(doc.Servers)
}

func operationServers(doc *openapi3.T, item *openapi3.PathItem, op *openapi3.Operation) []catalog.Server {
	if op != nil && op.Servers != nil {
		if list := serversFrom(*op.Servers); len(list) > 0 {
			return list
		}
	}
	if item != nil {
		if list := serversFrom(item.Servers); len(list) > 0 {
			return list
		}
	}
	return serverList(doc)
}

func serversFrom(list openapi3.Servers) []catalog.Server {
	var out []catalog.Server
	for _, s := range list {
		if s == nil || s.URL == "" {
			continue
		}
		out = append(out, catalog.Server{URL: s.URL, Name: s.Description})
	}
	return out
}

func fallbackID(method, path, group string) string {
	method = strings.ToLower(method)
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		name := group
		if name == "" {
			name = "root"
		}
		return name + "." + method
	}
	var b strings.Builder
	for i, part := range strings.Split(trimmed, "/") {
		if i > 0 {
			b.WriteByte('.')
		}
		if len(part) >= 2 && part[0] == '{' && part[len(part)-1] == '}' {
			b.WriteString("by.")
			b.WriteString(part[1 : len(part)-1])
			continue
		}
		b.WriteString(part)
	}
	b.WriteByte('.')
	b.WriteString(method)
	return strings.ReplaceAll(b.String(), " ", ".")
}

func mapOperation(method, path, group string, servers []catalog.Server, item *openapi3.PathItem, op *openapi3.Operation) catalog.Operation {
	id := op.OperationID
	fallback := false
	if strings.TrimSpace(id) == "" {
		id = fallbackID(method, path, group)
		fallback = true
	}
	id = strings.ReplaceAll(id, " ", ".")
	summary := strings.TrimSpace(op.Summary)
	name := summary
	if name == "" {
		name = id
	}
	desc := op.Description
	if desc == "" {
		desc = name
	}
	kind := kindFromMethod(strings.ToUpper(method))
	side, confirm := sideEffectFor(id, name, strings.ToUpper(method))
	params := mergeParams(item, op)
	base := ""
	if len(servers) > 0 {
		base = servers[0].URL
	}

	respSummary := ""
	if op.Responses != nil {
		if r := op.Responses.Status(200); r != nil && r.Value != nil && r.Value.Description != nil {
			respSummary = *r.Value.Description
		}
	}

	return catalog.Operation{
		ID:                   id,
		Name:                 name,
		Summary:              summary,
		IDFallback:           fallback,
		Description:          desc,
		Group:                group,
		Kind:                 kind,
		Method:               strings.ToUpper(method),
		PathTemplate:         path,
		Params:               params,
		ResponseSummary:      respSummary,
		SideEffect:           side,
		RequiresConfirmation: confirm,
		BaseURL:              base,
		Servers:              servers,
		Tags:                 append([]string(nil), op.Tags...),
		ResponseFields:       responseFields(op),
	}
}

func mergeParams(item *openapi3.PathItem, op *openapi3.Operation) []catalog.Param {
	type key struct {
		in   string
		name string
	}
	var order []key
	merged := map[key]*openapi3.Parameter{}
	add := func(list openapi3.Parameters) {
		for _, p := range list {
			if p == nil || p.Value == nil || p.Value.Name == "" {
				continue
			}
			k := key{in: p.Value.In, name: p.Value.Name}
			if _, ok := merged[k]; !ok {
				order = append(order, k)
			}
			merged[k] = p.Value
		}
	}
	if item != nil {
		add(item.Parameters)
	}
	if op != nil {
		add(op.Parameters)
	}
	var out []catalog.Param
	for _, k := range order {
		out = append(out, mapParam(merged[k]))
	}
	if op != nil {
		if body, ok := bodyParam(op); ok {
			out = append(out, body)
		}
	}
	return out
}

func mapParam(pv *openapi3.Parameter) catalog.Param {
	style, explode := "", false
	if sm, err := pv.SerializationMethod(); err == nil {
		style, explode = sm.Style, sm.Explode
	}
	return catalog.Param{
		Name:        pv.Name,
		In:          pv.In,
		Required:    pv.Required,
		Description: pv.Description,
		Schema:      schemaJSON(pv.Schema),
		Default:     schemaDefault(pv.Schema),
		Style:       style,
		Explode:     explode,
	}
}

func kindFromMethod(method string) catalog.Kind {
	switch method {
	case "GET", "HEAD":
		return catalog.KindRead
	case "POST":
		return catalog.KindCreate
	case "PUT", "PATCH":
		return catalog.KindUpdate
	case "DELETE":
		return catalog.KindDelete
	default:
		return catalog.KindAction
	}
}

func sideEffectFor(id, name, method string) (catalog.SideEffect, bool) {
	lower := strings.ToLower(id + " " + name)
	if method == "DELETE" || strings.Contains(lower, "delete") {
		return catalog.SideEffectDestructive, true
	}
	switch method {
	case "POST", "PUT", "PATCH":
		return catalog.SideEffectWrite, false
	default:
		return catalog.SideEffectNone, false
	}
}

func linkParamExprs(link *openapi3.Link) map[string]string {
	if link == nil || len(link.Parameters) == 0 {
		return nil
	}
	out := make(map[string]string, len(link.Parameters))
	for name, raw := range link.Parameters {
		s, ok := raw.(string)
		if !ok || s == "" {
			continue
		}
		out[name] = s
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectLinks(from string, op *openapi3.Operation) []rawLink {
	if op.Responses == nil {
		return nil
	}
	var out []rawLink
	for _, ref := range op.Responses.Map() {
		if ref == nil || ref.Value == nil {
			continue
		}
		for _, link := range ref.Value.Links {
			if link == nil || link.Value == nil {
				continue
			}
			out = append(out, rawLink{
				from:         from,
				operationID:  link.Value.OperationID,
				operationRef: link.Value.OperationRef,
				params:       linkParamExprs(link.Value),
			})
		}
	}
	return out
}

func resolveLinks(doc *openapi3.T, ops []catalog.Operation, raw []rawLink) ([]catalog.OpLink, error) {
	known := map[string]bool{}
	for _, op := range ops {
		known[op.ID] = true
	}
	var out []catalog.OpLink
	for _, l := range raw {
		to := l.operationID
		if to != "" {
			if !known[to] {
				return nil, fmt.Errorf("link from %s: unknown operationId %q", l.from, to)
			}
		} else {
			id, err := resolveOperationRef(doc, ops, l.operationRef)
			if err != nil {
				return nil, fmt.Errorf("link from %s: %w", l.from, err)
			}
			to = id
		}
		out = append(out, catalog.OpLink{From: l.from, To: to, Params: l.params})
	}
	return out, nil
}

func resolveOperationRef(doc *openapi3.T, ops []catalog.Operation, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("link has no operationId or operationRef")
	}
	if i := strings.IndexByte(ref, '#'); i > 0 {
		return "", fmt.Errorf("operationRef %q points outside this document", ref)
	}
	ptr, err := jsonpointer.New(strings.TrimPrefix(ref, "#"))
	if err != nil {
		return "", fmt.Errorf("operationRef %q: %w", ref, err)
	}
	toks := ptr.DecodedTokens()
	if len(toks) != 3 || toks[0] != "paths" || toks[1] == "" || toks[2] == "" {
		return "", fmt.Errorf("operationRef %q is not a path operation", ref)
	}
	path := toks[1]
	method := strings.ToUpper(toks[2])
	var item *openapi3.PathItem
	if doc.Paths != nil {
		item = doc.Paths.Find(path)
	}
	if item != nil && item.GetOperation(method) != nil {
		for _, op := range ops {
			if op.Method == method && op.PathTemplate == path {
				return op.ID, nil
			}
		}
	}
	return "", fmt.Errorf("operationRef %q does not resolve", ref)
}

func collectUses(opID string, item *openapi3.PathItem, op *openapi3.Operation, uses *[]catalog.SchemaUse, seen map[string]bool) {
	var names []string
	visited := map[*openapi3.SchemaRef]bool{}
	var params openapi3.Parameters
	if item != nil {
		params = append(params, item.Parameters...)
	}
	if op != nil {
		params = append(params, op.Parameters...)
	}
	for _, p := range params {
		if p != nil && p.Value != nil {
			collectSchema(p.Value.Schema, &names, visited)
		}
	}
	if op != nil && op.RequestBody != nil && op.RequestBody.Value != nil {
		collectContent(op.RequestBody.Value.Content, &names, visited)
	}
	if op != nil && op.Responses != nil {
		for _, ref := range op.Responses.Map() {
			if ref != nil && ref.Value != nil {
				collectContent(ref.Value.Content, &names, visited)
			}
		}
	}
	for _, name := range names {
		key := opID + "\x00" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		*uses = append(*uses, catalog.SchemaUse{OperationID: opID, Name: name})
	}
}

func collectContent(content openapi3.Content, names *[]string, seen map[*openapi3.SchemaRef]bool) {
	for _, mt := range content {
		if mt != nil {
			collectSchema(mt.Schema, names, seen)
		}
	}
}

func collectSchema(s *openapi3.SchemaRef, names *[]string, seen map[*openapi3.SchemaRef]bool) {
	if s == nil || seen[s] {
		return
	}
	seen[s] = true
	if s.Ref != "" {
		if name := schemaName(s.Ref); name != "" {
			*names = append(*names, name)
		}
	}
	if s.Value == nil {
		return
	}
	collectSchema(s.Value.Items, names, seen)
	for _, p := range s.Value.Properties {
		collectSchema(p, names, seen)
	}
	for _, sub := range s.Value.AllOf {
		collectSchema(sub, names, seen)
	}
	for _, sub := range s.Value.OneOf {
		collectSchema(sub, names, seen)
	}
	for _, sub := range s.Value.AnyOf {
		collectSchema(sub, names, seen)
	}
}

func schemaName(ref string) string {
	const marker = "/schemas/"
	i := strings.LastIndex(ref, marker)
	if i < 0 {
		return ""
	}
	name := ref[i+len(marker):]
	if slash := strings.Index(name, "/"); slash >= 0 {
		name = name[:slash]
	}
	return name
}
