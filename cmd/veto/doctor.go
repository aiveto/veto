package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"

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
	out = append(out, authBlockers(cat, cfg.Auth)...)
	if ping {
		out = append(out, pingServers(ctx, &http.Client{Timeout: cfg.Timeout}, cat)...)
	}
	sort.Strings(out)
	return out
}

func authBlockers(cat *catalog.Catalog, names map[string]string) []string {
	if cat == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, op := range cat.Operations {
		for _, a := range op.Auth {
			if a.Name == "" || seen[a.Name] {
				continue
			}
			seen[a.Name] = true
			envName := names[a.Name]
			if envName == "" {
				out = append(out, fmt.Sprintf("auth scheme %s has no env var", a.Name))
				continue
			}
			if os.Getenv(envName) == "" {
				out = append(out, envName+" is unset")
			}
		}
	}
	return out
}

func pingServers(ctx context.Context, client *http.Client, cat *catalog.Catalog) []string {
	if cat == nil {
		return nil
	}
	if client == nil {
		client = http.DefaultClient
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
		resp.Body.Close()
	}
	return out
}
