package auth

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

type commandIn struct {
	OperationID string `json:"operation_id"`
	Method      string `json:"method"`
	URL         string `json:"url"`
	Scheme      string `json:"scheme"`
	UserToken   string `json:"user_token,omitempty"`
}

type commandOut struct {
	Headers   map[string]string `json:"headers"`
	ExpiresAt string            `json:"expires_at,omitempty"`
}

type commandResult struct {
	Headers   map[string]string
	ExpiresAt time.Time
}

func runCommand(ctx context.Context, scheme Scheme, in commandIn, timeout time.Duration) (commandResult, error) {
	if len(scheme.Command) == 0 {
		return commandResult{}, errors.New("auth command is unset")
	}
	if scheme.Timeout > 0 {
		timeout = scheme.Timeout
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := jsonv2.Marshal(in)
	if err != nil {
		return commandResult{}, errors.New("auth command failed")
	}
	cmd := exec.CommandContext(cctx, scheme.Command[0], scheme.Command[1:]...)
	cmd.Stdin = bytes.NewReader(raw)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err = cmd.Run()
	if err != nil {
		if cctx.Err() != nil && ctx.Err() == nil {
			return commandResult{}, errors.New("auth command timed out")
		}
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return commandResult{}, fmt.Errorf("auth command exited %d", exit.ExitCode())
		}
		return commandResult{}, errors.New("auth command failed")
	}
	var out commandOut
	if err := jsonv2.Unmarshal(stdout.Bytes(), &out); err != nil {
		return commandResult{}, errors.New("auth command returned invalid JSON")
	}
	var exp time.Time
	if out.ExpiresAt != "" {
		exp, err = time.Parse(time.RFC3339, out.ExpiresAt)
		if err != nil {
			return commandResult{}, errors.New("auth command returned invalid expires_at")
		}
	}
	return commandResult{Headers: out.Headers, ExpiresAt: exp}, nil
}

func (r *Resolver) fetchCommand(ctx context.Context, s Scheme, operationID, method, endpoint string) (Material, error) {
	in := commandIn{
		OperationID: operationID,
		Method:      method,
		URL:         endpoint,
		Scheme:      s.Name,
	}
	if user := UserToken(ctx); user != "" {
		in.UserToken = user
	}
	out, err := runCommand(ctx, s, in, r.commandTimeout)
	if err != nil {
		return Material{}, err
	}
	return Material{Headers: cloneMap(out.Headers), Expires: out.ExpiresAt, Secrets: mapValues(out.Headers)}, nil
}
