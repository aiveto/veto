package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"

	"strings"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/mcpserver"
	"github.com/spf13/cobra"
)

type doctorCmd struct {
	contract  []string
	config    string
	bundle    string
	agent     string
	relations string
	baseURL   string
	pin       []string
	ping      bool
}

func newDoctorCommand() *cobra.Command {
	cmd := &doctorCmd{}
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check contracts, auth, ids, parameters, summaries, and pins.",
		Run: func(*cobra.Command, []string) {
			runDoctor(*cmd)
		},
	}
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&cmd.bundle, "bundle", "", "Directory or zip of contracts, relations, and check cases.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().StringVar(&cmd.baseURL, "base-url", "", "Override the server URL on every operation. Empty uses each contract server.")
	c.Flags().StringArrayVar(&cmd.pin, "pin", nil, "Pin operation ids.")
	c.Flags().BoolVar(&cmd.ping, "ping", false, "Request each server URL.")
	return c
}

func runDoctor(cmd doctorCmd) {
	loop, cfg, err := buildLoopBundle(cmd.contract, cmd.config, cmd.bundle, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		releaseBundles()
		fmt.Fprintf(os.Stderr, "doctor: %v\n", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	lines, fail := doctorReport(ctx, loop.Catalog, cfg, cmd.pin, cmd.ping)
	cancel()
	if len(lines) == 0 {
		fmt.Fprintln(os.Stderr, "ok")
		return
	}
	for _, line := range lines {
		fmt.Fprintln(os.Stderr, line)
	}
	if fail {
		releaseBundles()
		os.Exit(1)
	}
}

func doctorBlockers(ctx context.Context, cat *catalog.Catalog, cfg config.File, pins []string, ping bool) []string {
	lines, _ := doctorReport(ctx, cat, cfg, pins, ping)
	return lines
}

func doctorReport(ctx context.Context, cat *catalog.Catalog, cfg config.File, pins []string, ping bool) ([]string, bool) {
	var out []string
	fail := false
	if err := mcpserver.ValidatePins(cat, pins); err != nil {
		out = append(out, err.Error())
		fail = true
	}
	authLines := authBlockers(cat, cfg.Auth, tokenDir(cfg))
	if len(authLines) > 0 {
		out = append(out, authLines...)
		fail = true
	}
	found, bad := catalogFindings(cat, cfg.Auth, tokenDir(cfg))
	out = append(out, found...)
	if bad {
		fail = true
	}
	if ping {
		pingLines := pingServers(ctx, &http.Client{Timeout: cfg.Timeout}, cat)
		if len(pingLines) > 0 {
			out = append(out, pingLines...)
			fail = true
		}
	}
	sort.Strings(out)
	return out, fail
}

func catalogFindings(cat *catalog.Catalog, names config.Sources, dir string) ([]string, bool) {
	if cat == nil {
		return nil, false
	}
	var out []string
	fail := false
	for _, op := range cat.Operations {
		if line := missingAuth(op, names, dir); line != "" {
			out = append(out, line)
			fail = true
		}
		if op.IDCollision != "" {
			out = append(out, fmt.Sprintf("%s: colliding id %s", op.ID, op.IDCollision))
			fail = true
		}
		if op.IDFallback {
			out = append(out, op.ID+": fallback id")
		}
		for _, p := range op.Params {
			why := execute.Unserializable(p)
			if why == "" {
				continue
			}
			out = append(out, fmt.Sprintf("%s: parameter %s cannot be serialized: %s", op.ID, p.Name, why))
			fail = true
		}
		if line := summaryLine(op); line != "" {
			out = append(out, line)
		}
		if line := approvalLine(op); line != "" {
			out = append(out, line)
		}
	}
	return out, fail
}

func missingAuth(op catalog.Operation, names config.Sources, dir string) string {
	groups := op.Requirements
	if len(groups) == 0 && len(op.Auth) > 0 {
		groups = [][]catalog.Auth{op.Auth}
	}
	if len(groups) == 0 {
		return ""
	}
	for _, group := range groups {
		if groupReady(group, names, dir) {
			return ""
		}
	}
	var missing []string
	seen := map[string]bool{}
	for _, group := range groups {
		for _, a := range group {
			if a.Name == "" || seen[a.Name] || schemeReady(a, names, dir) {
				continue
			}
			seen[a.Name] = true
			missing = append(missing, a.Name)
		}
	}
	sort.Strings(missing)
	if len(missing) == 0 {
		return op.ID + ": missing auth"
	}
	return op.ID + ": missing auth " + strings.Join(missing, ", ")
}

func groupReady(group []catalog.Auth, names config.Sources, dir string) bool {
	if len(group) == 0 {
		return false
	}
	for _, a := range group {
		if !schemeReady(a, names, dir) {
			return false
		}
	}
	return true
}

func schemeReady(a catalog.Auth, names config.Sources, dir string) bool {
	if a.Kind == "unsupported" || (a.Kind == "apiKey" && a.Header == "" && a.Query == "") {
		return false
	}
	src, ok := names[a.Name]
	if !ok {
		return false
	}
	return len(sourceBlockers(a.Name, src, dir)) == 0
}

func summaryLine(op catalog.Operation) string {
	text := strings.TrimSpace(op.Summary)
	if text == "" {
		return op.ID + ": empty summary"
	}
	if weakSummary(text, op.ID) {
		return op.ID + ": weak summary"
	}
	return ""
}

func weakSummary(text, id string) bool {
	if strings.EqualFold(text, id) {
		return true
	}
	return len(strings.Fields(text)) < 2
}

func approvalLine(op catalog.Operation) string {
	if !op.RequiresConfirmation || !writeOp(op) {
		return ""
	}
	return op.ID + ": write requires approval"
}

func writeOp(op catalog.Operation) bool {
	switch op.SideEffect {
	case catalog.SideEffectWrite, catalog.SideEffectDestructive:
		return true
	case catalog.SideEffectNone:
	}
	switch op.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func authBlockers(cat *catalog.Catalog, names config.Sources, dir string) []string {
	if cat == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, op := range cat.Operations {
		for _, a := range op.AuthSchemes() {
			if a.Name == "" || seen[a.Name] {
				continue
			}
			seen[a.Name] = true
			src, ok := names[a.Name]
			if !ok {
				out = append(out, fmt.Sprintf("auth scheme %s has no env var", a.Name))
				continue
			}
			out = append(out, sourceBlockers(a.Name, src, dir)...)
		}
	}
	return out
}

func sourceBlockers(name string, src config.Source, dir string) []string {
	switch src.Kind() {
	case "env":
		if src.Env == "" {
			return []string{fmt.Sprintf("auth scheme %s has no env var", name)}
		}
		if os.Getenv(src.Env) != "" || auth.HasAccessToken(dir, name) {
			return nil
		}
		return []string{src.Env + " is unset"}
	case "client_credentials":
		if src.ClientSecretEnv == "" {
			return []string{fmt.Sprintf("auth scheme %s has no client secret env", name)}
		}
		if os.Getenv(src.ClientSecretEnv) == "" {
			return []string{src.ClientSecretEnv + " is unset"}
		}
		return nil
	case "login":
		if !auth.HasRefreshToken(dir, name) && !auth.HasAccessToken(dir, name) {
			return []string{fmt.Sprintf("auth scheme %s has no stored token", name)}
		}
		return nil
	case "command":
		if len(src.Command) == 0 {
			return []string{fmt.Sprintf("auth scheme %s has no command", name)}
		}
		return nil
	case "token_exchange":
		if src.ClientSecretEnv == "" || os.Getenv(src.ClientSecretEnv) == "" {
			envName := src.ClientSecretEnv
			if envName == "" {
				envName = "client secret"
			}
			return []string{envName + " is unset"}
		}
		if src.Subject != "" && src.Subject != "invoke" && !auth.HasAccessToken(dir, src.Subject) {
			return []string{fmt.Sprintf("auth scheme %s has no stored token", src.Subject)}
		}
		return nil
	default:
		return nil
	}
}

func pingServers(ctx context.Context, client *http.Client, cat *catalog.Catalog) []string {
	if cat == nil {
		return nil
	}
	if client == nil {
		return []string{"ping client is missing"}
	}
	seen := map[string]bool{}
	var urls []string
	for _, op := range cat.Operations {
		if op.BaseURL == "" || seen[op.BaseURL] {
			continue
		}
		seen[op.BaseURL] = true
		urls = append(urls, op.BaseURL)
	}
	sort.Strings(urls)
	var out []string
	for _, raw := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			out = append(out, fmt.Sprintf("ping %s: %v", raw, err))
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			out = append(out, fmt.Sprintf("ping %s: %v", raw, err))
			continue
		}
		if err := resp.Body.Close(); err != nil {
			out = append(out, fmt.Sprintf("ping %s: %v", raw, err))
		}
	}
	return out
}
