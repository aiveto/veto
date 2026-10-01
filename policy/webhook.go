package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"time"
)

type (
	// Notice is a pending call. It carries no parameters and no credentials.
	Notice struct {
		ID        string
		Operation string
		Caller    string
	}

	// Notifier is told when a call is pending. It does not approve the call.
	Notifier interface {
		Pending(ctx context.Context, notice Notice) error
	}

	HTTPNotifier struct {
		url    string
		client *http.Client
	}

	CommandNotifier struct {
		command []string
	}
)

func NewWebhook(rawURL string, command []string) (Notifier, error) {
	if rawURL == "" && len(command) == 0 {
		return nil, fmt.Errorf("approval webhook required")
	}
	if rawURL != "" && len(command) > 0 {
		return nil, fmt.Errorf("approval webhook is a command or an HTTP URL")
	}
	if rawURL != "" {
		u, err := url.Parse(rawURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("approval webhook URL is not http or https")
		}
		return &HTTPNotifier{
			url: rawURL,
			client: &http.Client{
				Timeout:       10 * time.Second,
				CheckRedirect: refuseRedirect,
			},
		}, nil
	}
	if command[0] == "" {
		return nil, fmt.Errorf("approval webhook command required")
	}
	return &CommandNotifier{command: append([]string(nil), command...)}, nil
}

func (h *HTTPNotifier) Pending(ctx context.Context, notice Notice) error {
	if h == nil {
		return fmt.Errorf("approval webhook required")
	}
	body, err := noticeJSON(notice)
	if err != nil {
		return err
	}
	return h.post(ctx, body)
}

func (c *CommandNotifier) Pending(ctx context.Context, notice Notice) error {
	if c == nil {
		return fmt.Errorf("approval webhook required")
	}
	body, err := noticeJSON(notice)
	if err != nil {
		return err
	}
	return c.run(ctx, body)
}

func noticeJSON(notice Notice) ([]byte, error) {
	body, err := json.Marshal(struct {
		ID        string `json:"id"`
		Operation string `json:"operation"`
		Caller    string `json:"caller"`
	}{
		ID:        notice.ID,
		Operation: notice.Operation,
		Caller:    notice.Caller,
	})
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return body, nil
}

func (h *HTTPNotifier) post(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := h.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: refuseRedirect}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	closeErr := resp.Body.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func (c *CommandNotifier) run(ctx context.Context, body []byte) error {
	cmd := exec.CommandContext(ctx, c.command[0], c.command[1:]...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	cmd.Stdin = bytes.NewReader(body)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func refuseRedirect(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}
