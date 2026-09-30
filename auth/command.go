package auth

import (
	"bytes"
	"context"
	"encoding/json"
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

func runCommand(ctx context.Context, scheme Scheme, in commandIn, timeout time.Duration) (Output, error) {
	if len(scheme.Command) == 0 {
		return Output{}, fmt.Errorf("auth command is unset")
	}
	if scheme.Timeout > 0 {
		timeout = scheme.Timeout
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := json.Marshal(in)
	if err != nil {
		return Output{}, fmt.Errorf("auth command failed")
	}
	cmd := exec.CommandContext(cctx, scheme.Command[0], scheme.Command[1:]...)
	cmd.Stdin = bytes.NewReader(raw)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err = cmd.Run()
	if err != nil {
		if cctx.Err() != nil && ctx.Err() == nil {
			return Output{}, fmt.Errorf("auth command timed out")
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return Output{}, fmt.Errorf("auth command exited %d", exit.ExitCode())
		}
		return Output{}, fmt.Errorf("auth command failed")
	}
	var out commandOut
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return Output{}, fmt.Errorf("auth command returned invalid JSON")
	}
	var exp time.Time
	if out.ExpiresAt != "" {
		exp, err = time.Parse(time.RFC3339, out.ExpiresAt)
		if err != nil {
			return Output{}, fmt.Errorf("auth command returned invalid expires_at")
		}
	}
	return Output{Headers: out.Headers, ExpiresAt: exp}, nil
}
