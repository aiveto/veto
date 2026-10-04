package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/capability"
)

// RunJSON reads capability lines from r and writes the same JSON MCP returns.
func (s *Server) RunJSON(ctx context.Context, r io.Reader, w io.Writer) error {
	dec := json.NewDecoder(r)
	var failed error
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var line capability.Line
		if err := dec.Decode(&line); err != nil {
			if errors.Is(err, io.EOF) {
				return failed
			}
			return err
		}
		b, err := s.handleLine(ctx, line)
		if err != nil {
			failed = err
			b, encErr := s.Encode(map[string]string{"error": err.Error()})
			if encErr != nil {
				return encErr
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

func (s *Server) handleLine(ctx context.Context, line capability.Line) ([]byte, error) {
	n := 0
	if line.Search != nil {
		n++
	}
	if line.Describe != nil {
		n++
	}
	if line.Invoke != nil {
		n++
	}
	if n != 1 {
		return nil, errors.New("exactly one of search, describe, invoke")
	}
	switch {
	case line.Search != nil:
		return s.RunSearch(*line.Search)
	case line.Describe != nil:
		return s.RunDescribe(*line.Describe)
	default:
		ctx = auth.WithCaller(ctx, callerName(line.Caller))
		return s.RunInvoke(ctx, *line.Invoke)
	}
}

func callerName(name string) string {
	if name == "" {
		return "local"
	}
	return name
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
