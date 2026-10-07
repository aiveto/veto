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
	srv, cfg, err := buildServerBundle(cmd.contract, cmd.config, cmd.bundle, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		releaseBundles()
		fmt.Fprintf(os.Stderr, "doctor: %v\n", err)
		exitMain(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	lines, fail := doctorReport(ctx, srv.Catalog, cfg, cmd.pin, cmd.ping)
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
		exitMain(1)
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
	creds := authResolver(cfg)
	authLines := authBlockers(cat, creds)
	if len(authLines) > 0 {
		out = append(out, authLines...)
		fail = true
	}
	found, bad := catalogFindings(cat, creds)
	out = append(out, found...)
	if line := confirmationNotice(cfg); line != "" {
		out = append(out, line)
	}
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

func catalogFindings(cat *catalog.Catalog, creds *auth.Resolver) ([]string, bool) {
	if cat == nil {
		return nil, false
	}
	var out []string
	fail := false
	for _, op := range cat.Operations {
		if line := missingAuth(op, creds); line != "" {
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
			why := p.Unserializable()
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

func missingAuth(op catalog.Operation, creds *auth.Resolver) string {
	groups := op.Requirements
	if len(groups) == 0 && len(op.Auth) > 0 {
		groups = [][]catalog.Auth{op.Auth}
	}
	if len(groups) == 0 {
		return ""
	}
	for _, group := range groups {
		if groupReady(group, creds) {
			return ""
		}
	}
	var missing []string
	seen := map[string]bool{}
	for _, group := range groups {
		for _, a := range group {
			if a.Name == "" || seen[a.Name] || schemeReady(a, creds) {
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

func groupReady(group []catalog.Auth, creds *auth.Resolver) bool {
	if len(group) == 0 {
		return true
	}
	for _, a := range group {
		if !schemeReady(a, creds) {
			return false
		}
	}
	return true
}

func schemeReady(a catalog.Auth, creds *auth.Resolver) bool {
	if creds != nil && creds.Has(a.Name) && len(creds.Blockers(a)) == 0 {
		return true
	}
	if a.Kind == "unsupported" || (a.Kind == "apiKey" && a.Header == "" && a.Query == "") {
		return false
	}
	return creds != nil && len(creds.Blockers(a)) == 0
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

func authBlockers(cat *catalog.Catalog, creds *auth.Resolver) []string {
	if cat == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, op := range cat.Operations {
		if missingAuth(op, creds) == "" {
			continue
		}
		for _, a := range op.AuthSchemes() {
			if a.Name == "" || seen[a.Name] {
				continue
			}
			seen[a.Name] = true
			if creds == nil {
				out = append(out, fmt.Sprintf("auth scheme %s has no env var", a.Name))
				continue
			}
			out = append(out, creds.Blockers(a)...)
		}
	}
	return out
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
			out = append(out, fmt.Sprintf("ping %s: upstream: %v", raw, err))
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			out = append(out, fmt.Sprintf("ping %s: upstream: %v", raw, err))
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			out = append(out, fmt.Sprintf("ping %s: upstream: status %d", raw, resp.StatusCode))
		}
		if err := resp.Body.Close(); err != nil {
			out = append(out, fmt.Sprintf("ping %s: upstream: %v", raw, err))
		}
	}
	return out
}
