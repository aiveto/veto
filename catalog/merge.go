package catalog

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type (
	// Relation asserts that a schema field identifies another operation.
	Relation struct {
		Schema string `yaml:"schema"`
		Field  string `yaml:"field"`
		To     string `yaml:"to"`
	}

	relationFile struct {
		Relations []Relation `yaml:"relations"`
	}
)

// Merge copies operations from each catalog into one. Duplicate ids are an error.
func Merge(parts ...*Catalog) (*Catalog, error) {
	out := &Catalog{Title: "catalog"}
	seen := map[string]bool{}
	for _, part := range parts {
		if part == nil {
			continue
		}
		for _, op := range part.Operations {
			if seen[op.ID] {
				return nil, fmt.Errorf("duplicate operation %q", op.ID)
			}
			seen[op.ID] = true
			out.Operations = append(out.Operations, op)
		}
		out.Links = append(out.Links, part.Links...)
		out.Uses = append(out.Uses, part.Uses...)
	}
	if out.Title == "catalog" && len(parts) == 1 && parts[0] != nil && parts[0].Title != "" {
		out.Title = parts[0].Title
	}
	out.Finalize()
	return out, nil
}

// LoadRelations reads a relations file.
func LoadRelations(path string) ([]Relation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read relations: %w", err)
	}
	var f relationFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse relations: %w", err)
	}
	return f.Relations, nil
}

// ApplyRelations adds an edge from every operation that uses the schema to the target operation.
// A field name in a spec does not create an edge. This declaration does.
func ApplyRelations(cat *Catalog, rels []Relation) error {
	if cat == nil {
		return fmt.Errorf("missing catalog")
	}
	cat.Finalize()
	for _, r := range rels {
		if r.Schema == "" || r.Field == "" || r.To == "" {
			return fmt.Errorf("relation needs schema, field, and to")
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
