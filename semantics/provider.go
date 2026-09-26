package semantics

import (
	"fmt"
	"os"
	"strings"

	"github.com/aiveto/veto/catalog"
	"gopkg.in/yaml.v3"
)

var builtin = map[string][]string{
	"delete": {"remove", "retire", "destroy"},
	"get":    {"fetch", "read", "load"},
	"list":   {"enumerate", "browse"},
	"create": {"add", "new"},
	"update": {"patch", "change"},
}

type (
	// Note holds human text and search synonyms for one operation.
	// Relation is a sentence veto derived from a declared edge, such as
	// "Holding.teamsId identifies teams.get".
	Note struct {
		OperationID string
		Sentence    string
		Synonyms    []string
		Relation    string
	}

	// Provider supplies semantic notes for search and context.
	Provider interface {
		Note(operationID string) Note
		Synonyms(operationID string) []string
		AllSynonyms() map[string][]string
	}

	// Derived builds notes from operation summaries and a small builtin map.
	Derived struct {
		cat   *catalog.Catalog
		notes map[string]Note
	}

	// OverlayEntry is one row in a semantics yaml file.
	OverlayEntry struct {
		OperationID string   `yaml:"operation"`
		Sentence    string   `yaml:"sentence"`
		Synonyms    []string `yaml:"synonyms"`
	}

	// FileOverlay merges yaml overrides onto a base provider.
	FileOverlay struct {
		base  Provider
		notes map[string]Note
	}
)

// Text is the sentence plus the relation, when one is declared.
func (n Note) Text() string {
	if n.Relation == "" || strings.Contains(n.Sentence, n.Relation) {
		if n.Sentence != "" {
			return n.Sentence
		}
		return n.Relation
	}
	if n.Sentence == "" {
		return n.Relation
	}
	return n.Sentence + " " + n.Relation
}

// NewDerived creates a semantics provider from the catalog.
func NewDerived(cat *catalog.Catalog) *Derived {
	d := &Derived{cat: cat, notes: map[string]Note{}}
	for _, op := range cat.Operations {
		syns := deriveSynonyms(op)
		d.notes[op.ID] = Note{
			OperationID: op.ID,
			Sentence:    op.Description,
			Synonyms:    syns,
		}
	}
	return d
}

func deriveSynonyms(op catalog.Operation) []string {
	var out []string
	words := strings.Fields(strings.ToLower(op.Description + " " + op.Name + " " + op.ID))
	seen := map[string]bool{}
	for _, w := range words {
		w = strings.Trim(w, ".,/")
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if extra, ok := builtin[w]; ok {
			for _, e := range extra {
				if !seen[e] {
					seen[e] = true
					out = append(out, e)
				}
			}
		}
	}
	for token, extra := range builtin {
		if strings.Contains(strings.ToLower(op.ID), token) || strings.Contains(strings.ToLower(op.Name), token) {
			for _, e := range extra {
				if !seen[e] {
					seen[e] = true
					out = append(out, e)
				}
			}
		}
	}
	return out
}

func (d *Derived) Note(operationID string) Note {
	n, ok := d.notes[operationID]
	if !ok {
		n = Note{OperationID: operationID}
	}
	n.Relation = relationSentence(d.cat, operationID)
	if n.Relation != "" {
		n.Synonyms = append(append([]string{}, n.Synonyms...), strings.Fields(n.Relation)...)
	}
	return n
}

func relationSentence(cat *catalog.Catalog, operationID string) string {
	if cat == nil {
		return ""
	}
	var parts []string
	for _, e := range cat.Graph.Edges {
		if e.From == operationID && e.Kind == catalog.EdgeRelates && e.Note != "" {
			parts = append(parts, e.Note+" identifies "+e.To)
		}
	}
	return strings.Join(parts, " ")
}

func (d *Derived) Synonyms(operationID string) []string {
	return d.Note(operationID).Synonyms
}

func (d *Derived) AllSynonyms() map[string][]string {
	out := make(map[string][]string, len(d.notes))
	for id := range d.notes {
		out[id] = d.Synonyms(id)
	}
	return out
}

// LoadOverlay reads semantics yaml and wraps base.
func LoadOverlay(path string, base Provider) (*FileOverlay, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read semantics: %w", err)
	}
	var entries []OverlayEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse semantics: %w", err)
	}
	fo := &FileOverlay{base: base, notes: map[string]Note{}}
	for _, e := range entries {
		fo.notes[e.OperationID] = Note{
			OperationID: e.OperationID,
			Sentence:    e.Sentence,
			Synonyms:    e.Synonyms,
		}
	}
	return fo, nil
}

func (f *FileOverlay) Note(operationID string) Note {
	if n, ok := f.notes[operationID]; ok {
		base := f.base.Note(operationID)
		n.Relation = base.Relation
		if n.Sentence == "" {
			n.Sentence = base.Sentence
		}
		if len(n.Synonyms) == 0 {
			n.Synonyms = base.Synonyms
		} else {
			merged := append([]string{}, base.Synonyms...)
			seen := map[string]bool{}
			for _, s := range merged {
				seen[s] = true
			}
			for _, s := range n.Synonyms {
				if !seen[s] {
					merged = append(merged, s)
				}
			}
			n.Synonyms = merged
		}
		return n
	}
	return f.base.Note(operationID)
}

func (f *FileOverlay) Synonyms(operationID string) []string {
	return f.Note(operationID).Synonyms
}

func (f *FileOverlay) AllSynonyms() map[string][]string {
	out := f.base.AllSynonyms()
	for id := range f.notes {
		out[id] = f.Note(id).Synonyms
	}
	return out
}
