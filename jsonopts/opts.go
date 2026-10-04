// Package jsonopts is the encode and decode options for veto's own JSON.
package jsonopts

import (
	"io"

	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
)

// Set is v2 unless V1 is set. The zero value is the default.
type Set struct {
	V1 bool
}

func (s Set) Marshal(v any) ([]byte, error) {
	return jsonv2.Marshal(v, s.opts()...)
}

func (s Set) Unmarshal(data []byte, v any) error {
	return jsonv2.Unmarshal(data, v, s.opts()...)
}

func (s Set) MarshalWrite(w io.Writer, v any, extra ...jsonv2.Options) error {
	return jsonv2.MarshalWrite(w, v, append(s.opts(), extra...)...)
}

func (s Set) opts() []jsonv2.Options {
	if !s.V1 {
		return nil
	}
	return []jsonv2.Options{jsonv1.DefaultOptionsV1()}
}

// Indent is pretty-printed JSON for a CLI writer.
func (s Set) Indent() []jsonv2.Options {
	return []jsonv2.Options{jsontext.Multiline(true), jsontext.WithIndent("  ")}
}
