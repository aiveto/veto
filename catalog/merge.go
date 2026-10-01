package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type (
	Relation struct {
		Schema string `yaml:"schema"`
		Field  string `yaml:"field"`
		To     string `yaml:"to"`
	}

	relationFile struct {
		Relations []Relation `yaml:"relations"`
	}
)

func Merge(parts ...*Catalog) (*Catalog, error) {
	out := &Catalog{Title: "catalog"}
	seen := map[string]bool{}
	dup := map[string]bool{}
	for _, part := range parts {
		if part == nil {
			continue
		}
		for _, op := range part.Operations {
			if seen[op.ID] {
				dup[op.ID] = true
				continue
			}
			seen[op.ID] = true
			out.Operations = append(out.Operations, op)
		}
		out.Links = append(out.Links, part.Links...)
		out.Uses = append(out.Uses, part.Uses...)
	}
	if len(dup) > 0 {
		ids := make([]string, 0, len(dup))
		for id := range dup {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		quoted := make([]string, len(ids))
		for i, id := range ids {
			quoted[i] = fmt.Sprintf("%q", id)
		}
		return nil, fmt.Errorf("duplicate operation %s", strings.Join(quoted, ", "))
	}
	if out.Title == "catalog" && len(parts) == 1 && parts[0] != nil && parts[0].Title != "" {
		out.Title = parts[0].Title
	}
	out.Finalize()
	return out, nil
}

func ParseRelations(data []byte) ([]Relation, error) {
	var f relationFile
	if err := decodeStrict(data, &f); err != nil {
		return nil, fmt.Errorf("parse relations: %w", err)
	}
	return f.Relations, nil
}

func decodeStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	var extra yaml.Node
	err := dec.Decode(&extra)
	if err == nil {
		return errors.New("extra document")
	}
	if !errors.Is(err, io.EOF) {
		return err
	}
	if err := rejectDup(&doc, map[*yaml.Node]struct{}{}); err != nil {
		return err
	}
	known := yaml.NewDecoder(bytes.NewReader(data))
	known.KnownFields(true)
	return known.Decode(out)
}

func rejectDup(n *yaml.Node, active map[*yaml.Node]struct{}) error {
	if n == nil {
		return nil
	}
	if _, seen := active[n]; seen {
		return errors.New("yaml alias cycle")
	}
	active[n] = struct{}{}
	defer delete(active, n)
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			if err := rejectDup(child, active); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		keys := map[string]struct{}{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if _, ok := keys[key]; ok {
				return fmt.Errorf("duplicate field %q", key)
			}
			keys[key] = struct{}{}
			if err := rejectDup(n.Content[i+1], active); err != nil {
				return err
			}
		}
	case yaml.AliasNode:
		return rejectDup(n.Alias, active)
	case yaml.ScalarNode:
		return nil
	}
	return nil
}

// A field name in a spec does not create an edge. This declaration does.
func ApplyRelations(cat *Catalog, rels []Relation) error {
	if cat == nil {
		return errors.New("missing catalog")
	}
	cat.Finalize()
	for _, r := range rels {
		if r.Schema == "" || r.Field == "" || r.To == "" {
			return errors.New("relation needs schema, field, and to")
		}
		if cat.ByID(r.To) == nil {
			return fmt.Errorf("relation to unknown operation %q", r.To)
		}
		var froms []string
		seen := map[string]bool{}
		for _, u := range cat.Uses {
			if u.Name != r.Schema || seen[u.OperationID] {
				continue
			}
			seen[u.OperationID] = true
			froms = append(froms, u.OperationID)
		}
		if len(froms) == 0 {
			return fmt.Errorf("relation schema %q is not used", r.Schema)
		}
		note := r.Schema + "." + r.Field
		for _, from := range froms {
			if from == r.To {
				continue
			}
			cat.Links = append(cat.Links, OpLink{From: from, To: r.To, Note: note})
		}
	}
	cat.Finalize()
	return nil
}
