// Package semantics derives a note for an operation and applies a file overlay.
package semantics

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/internal/yamlfile"
)

var builtin = map[string][]string{
	"delete": {"remove", "retire", "destroy"},
	"get":    {"fetch", "read", "load"},
	"list":   {"enumerate", "browse"},
	"create": {"add", "new"},
	"update": {"patch", "change"},
}

type (
	Note struct {
		OperationID string
		Sentence    string
		Synonyms    []string
		Relation    string // from a declared edge, such as "Order.customerId identifies customers.get"
	}

	Derived struct {
		notes map[string]Note
		syns  map[string][]string
	}

	OverlayEntry struct {
		OperationID string   `yaml:"operation"`
		Sentence    string   `yaml:"sentence"`
		Synonyms    []string `yaml:"synonyms"`
	}

	notes interface {
		Note(operationID string) Note
		AllSynonyms() map[string][]string
	}

	FileOverlay struct {
		base  notes
		notes map[string]Note
		syns  map[string][]string
	}
)

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

func New(cat *catalog.Catalog) *Derived {
	d := &Derived{notes: map[string]Note{}, syns: map[string][]string{}}
	if cat == nil {
		return d
	}
	for _, op := range cat.Operations {
		syns := deriveSynonyms(op)
		rel := relationSentence(cat, op.ID)
		if rel != "" {
			syns = append(append([]string{}, syns...), strings.Fields(rel)...)
		}
		d.notes[op.ID] = Note{
			OperationID: op.ID,
			Sentence:    op.Description,
			Synonyms:    syns,
			Relation:    rel,
		}
		d.syns[op.ID] = syns
	}
	return d
}

func (d *Derived) Note(operationID string) Note {
	n, ok := d.notes[operationID]
	if !ok {
		return Note{OperationID: operationID}
	}
	return n
}

func (d *Derived) Synonyms(operationID string) []string {
	return d.Note(operationID).Synonyms
}

func (d *Derived) AllSynonyms() map[string][]string {
	return d.syns
}

func deriveSynonyms(op catalog.Operation) []string {
	var out []string
	words := strings.Fields(strings.ToLower(op.Description + " " + op.Name + " " + op.ID + " " + op.Group + " " + strings.Join(op.Tags, " ")))
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

func ParseOverlay(data []byte, base notes) (*FileOverlay, error) {
	var entries []OverlayEntry
	if err := decodeStrict(data, &entries); err != nil {
		return nil, fmt.Errorf("parse semantics: %w", err)
	}
	fo := &FileOverlay{base: base, notes: map[string]Note{}}
	seen := map[string]struct{}{}
	for _, e := range entries {
		if e.OperationID == "" {
			return nil, errors.New("parse semantics: operation required")
		}
		if _, ok := seen[e.OperationID]; ok {
			return nil, fmt.Errorf("parse semantics: duplicate operation %q", e.OperationID)
		}
		seen[e.OperationID] = struct{}{}
		fo.notes[e.OperationID] = Note{
			OperationID: e.OperationID,
			Sentence:    e.Sentence,
			Synonyms:    e.Synonyms,
		}
	}
	fo.syns = fo.allSynonyms()
	return fo, nil
}

func decodeStrict(data []byte, out any) error {
	return yamlfile.Decode(data, out)
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
	return f.syns
}

func (f *FileOverlay) allSynonyms() map[string][]string {
	out := map[string][]string{}
	if f.base != nil {
		maps.Copy(out, f.base.AllSynonyms())
	}
	for id := range f.notes {
		out[id] = f.Note(id).Synonyms
	}
	return out
}
