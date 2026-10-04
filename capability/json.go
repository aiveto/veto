package capability

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"io"

	jsonv2 "encoding/json/v2"
)

// RunJSON reads capability lines from r and writes the same JSON MCP returns.
func RunJSON(ctx context.Context, s *Server, r io.Reader, w io.Writer) error {
	dec := jsontext.NewDecoder(r)
	var failed error
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var line Line
		if err := jsonv2.UnmarshalDecode(dec, &line); err != nil {
			if errors.Is(err, io.EOF) {
				return failed
			}
			return err
		}
		b, err := s.Handle(ctx, line)
		if err != nil {
			failed = err
			if len(b) == 0 {
				var encErr error
				b, encErr = s.Encode(map[string]string{"error": err.Error()})
				if encErr != nil {
					return encErr
				}
			}
			if err := writeLine(w, b); err != nil {
				return err
			}
			continue
		}
		if err := writeLine(w, b); err != nil {
			return err
		}
	}
}

func writeLine(w io.Writer, b []byte) error {
	if _, err := w.Write(b); err != nil {
		return err
	}
	if len(b) == 0 || b[len(b)-1] != '\n' {
		_, err := io.WriteString(w, "\n")
		return err
	}
	return nil
}
