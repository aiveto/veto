package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"

	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/mcpserver"
	"github.com/spf13/cobra"
)

type doctorCmd struct {
	contract  []string
	config    string
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
		Short: "Check contracts, relations, auth env names, and pins.",
		Run: func(*cobra.Command, []string) {
			runDoctor(*cmd)
		},
	}
	c.Flags().StringArrayVar(&cmd.contract, "contract", nil, "OpenAPI file. Repeat to register another API. Overrides config.")
	c.Flags().StringVar(&cmd.config, "config", "", "Path to veto.yaml provider keys.")
	c.Flags().StringVar(&cmd.agent, "agent", "", "Path to agent.yaml. Overrides agent_file.")
	c.Flags().StringVar(&cmd.relations, "relations", "", "Relations file. Overrides relations_file.")
	c.Flags().StringVar(&cmd.baseURL, "base-url", "", "Override the server URL on every operation. Empty uses each contract server.")
	c.Flags().StringArrayVar(&cmd.pin, "pin", nil, "Pin operation ids.")
	c.Flags().BoolVar(&cmd.ping, "ping", false, "Request each server URL.")
	return c
}

func runDoctor(cmd doctorCmd) {
	loop, cfg, err := buildLoop(cmd.contract, cmd.config, cmd.agent, cmd.relations, cmd.baseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "doctor: %v\n", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	blockers := doctorBlockers(ctx, loop.Catalog, cfg, cmd.pin, cmd.ping)
	if len(blockers) == 0 {
		fmt.Fprintln(os.Stderr, "ok")
		return
	}
	for _, line := range blockers {
		fmt.Fprintln(os.Stderr, line)
	}
	os.Exit(1)
}

func doctorBlockers(ctx context.Context, cat *catalog.Catalog, cfg config.File, pins []string, ping bool) []string {
	var out []string
	if err := mcpserver.ValidatePins(cat, pins); err != nil {
		out = append(out, err.Error())
	}
	out = append(out, authBlockers(cat, cfg.Auth, tokenDir(cfg))...)
	if ping {
		out = append(out, pingServers(ctx, &http.Client{Timeout: cfg.Timeout}, cat)...)
	}
	sort.Strings(out)
	return out
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
