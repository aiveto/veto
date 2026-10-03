package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/agent/openai"
	"github.com/aiveto/veto/agentmeta"
	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/bundle"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/opa"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/semantics"
)

type sources struct {
	cfg       config.File
	contracts []string
	relations string
	agent     string
}

func callerName(name string) string {
	if name == "" {
		return "local"
	}
	return name
}

func allowSet(list []string) map[string]bool {
	if list == nil {
		return nil
	}
	out := make(map[string]bool, len(list))
	for _, p := range list {
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func authSecrets(names config.Sources) map[string]string {
	if len(names) == 0 {
		return nil
	}
	out := make(map[string]string, len(names))
	for scheme, src := range names {
		if src.Kind() != "env" || src.Env == "" {
			continue
		}
		if val := os.Getenv(src.Env); val != "" {
			out[scheme] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func tokenDir(cfg config.File) string {
	if d := os.Getenv("VETO_TOKEN_DIR"); d != "" {
		return d
	}
	if cfg.TokenDir != "" {
		return cfg.TokenDir
	}
	return auth.DefaultTokenDir()
}

func authResolver(cfg config.File) *auth.Resolver {
	schemes := make([]auth.Scheme, 0, len(cfg.Auth))
	for name, src := range cfg.Auth {
		schemes = append(schemes, auth.Scheme{
			Name:                   name,
			Source:                 src.Kind(),
			Env:                    src.Env,
			ClientID:               src.ClientID,
			ClientSecretEnv:        src.ClientSecretEnv,
			AuthorizationURL:       src.AuthorizationURL,
			TokenURL:               src.TokenURL,
			Issuer:                 src.Issuer,
			DeviceAuthorizationURL: src.DeviceAuthorizationURL,
			RedirectURL:            src.RedirectURL,
			Scopes:                 append([]string(nil), src.Scopes...),
			Audience:               src.Audience,
			Header:                 src.Header,
			Command:                append([]string(nil), src.Command...),
			Timeout:                time.Duration(src.Timeout),
			AuthToken:              src.AuthToken,
			UserToken:              src.UserToken,
			UserHeader:             src.UserHeader,
			Subject:                src.Subject,
			SubjectTokenType:       src.SubjectTokenType,
		})
	}
	return auth.New(auth.Options{
		Schemes:        schemes,
		Dir:            tokenDir(cfg),
		HTTP:           &http.Client{Timeout: cfg.Timeout},
		CommandTimeout: cfg.Timeout,
	})
}

func applyDeployment(cat *catalog.Catalog, cfg config.File) {
	cat.Select(catalog.Selection{ReadOnly: cfg.ReadOnly, Tags: cfg.Expose.Tags, Paths: cfg.Expose.Paths})
	if cfg.Confirms() {
		return
	}
	cat.ClearConfirmation()
}

func confirmationNotice(cfg config.File) string {
	if cfg.Confirms() {
		return ""
	}
	return "confirmation is off"
}

func applyAgent(cat *catalog.Catalog, path string) error {
	if path == "" {
		return nil
	}
	f, err := agentmeta.Load(path)
	if err != nil {
		return err
	}
	return agentmeta.Apply(cat, f)
}

func loadCatalog(contracts []string, relationsPath string) (*catalog.Catalog, error) {
	if len(contracts) == 0 {
		return nil, errors.New("contract required")
	}
	parts := make([]*catalog.Catalog, 0, len(contracts))
	for _, path := range contracts {
		cat, err := openapi.Load(context.Background(), path)
		if err != nil {
			return nil, err
		}
		parts = append(parts, cat)
	}
	cat, err := catalog.Merge(parts...)
	if err != nil {
		return nil, err
	}
	if relationsPath == "" {
		return cat, nil
	}
	data, err := os.ReadFile(relationsPath)
	if err != nil {
		return nil, fmt.Errorf("read relations: %w", err)
	}
	rels, err := catalog.ParseRelations(data)
	if err != nil {
		return nil, err
	}
	if err := catalog.ApplyRelations(cat, rels); err != nil {
		return nil, err
	}
	return cat, nil
}

func resolve(configPath string, contracts []string, relations, agent string) (sources, error) {
	return resolveBundle(configPath, "", contracts, relations, agent)
}

func resolveBundle(configPath, bundlePath string, contracts []string, relations, agent string) (sources, error) {
	cfg := config.Defaults()
	loadedConfig := false
	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			return sources{}, err
		}
		cfg = loaded
		loadedConfig = true
	}
	if bundlePath == "" {
		bundlePath = cfg.Bundle
	}
	if bundlePath != "" {
		shared, err := bundle.Load(bundlePath)
		if err != nil {
			return sources{}, err
		}
		if loadedConfig {
			cfg = overlayBundle(cfg, shared.Config)
		} else {
			cfg = shared.Config
		}
	}
	if len(contracts) == 0 {
		contracts = cfg.Contracts
	}
	if relations == "" {
		relations = cfg.RelationsFile
	}
	if agent == "" {
		agent = cfg.AgentFile
	}
	return sources{cfg: cfg, contracts: contracts, relations: relations, agent: agent}, nil
}

func overlayBundle(deploy, shared config.File) config.File {
	out := deploy
	if len(shared.Contracts) > 0 {
		out.Contracts = shared.Contracts
	}
	if shared.RelationsFile != "" {
		out.RelationsFile = shared.RelationsFile
	}
	if len(shared.Cases) > 0 {
		out.Cases = shared.Cases
	}
	if shared.SemanticsFile != "" {
		out.SemanticsFile = shared.SemanticsFile
		out.Semantics = shared.Semantics
	}
	if shared.AgentFile != "" {
		out.AgentFile = shared.AgentFile
	}
	if shared.FlowFile != "" {
		out.FlowFile = shared.FlowFile
	}
	return out
}

func releaseBundles() {
	if err := bundle.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "bundle: %v\n", err)
	}
}

func buildLoop(contracts []string, configPath, agentPath, relationsPath, baseURL string) (*agent.Loop, config.File, error) {
	return buildLoopBundle(contracts, configPath, "", agentPath, relationsPath, baseURL)
}

func buildLoopBundle(contracts []string, configPath, bundlePath, agentPath, relationsPath, baseURL string) (*agent.Loop, config.File, error) {
	src, err := resolveBundle(configPath, bundlePath, contracts, relationsPath, agentPath)
	if err != nil {
		return nil, config.File{}, err
	}
	cfg := src.cfg
	contracts = src.contracts
	relationsPath = src.relations
	agentPath = src.agent
	cat, err := loadCatalog(contracts, relationsPath)
	if err != nil {
		return nil, config.File{}, err
	}
	if agentPath == "" {
		agentPath = cfg.AgentFile
	}
	if err := applyAgent(cat, agentPath); err != nil {
		return nil, config.File{}, err
	}
	applyDeployment(cat, cfg)
	if err := cat.SelectServer(cfg.Server); err != nil {
		return nil, config.File{}, err
	}
	var sem agent.Notes = semantics.NewDerived(cat)
	if cfg.SemanticsFile != "" {
		data, err := os.ReadFile(cfg.SemanticsFile)
		if err != nil {
			return nil, config.File{}, fmt.Errorf("read semantics: %w", err)
		}
		over, err := semantics.ParseOverlay(data, sem)
		if err != nil {
			return nil, config.File{}, err
		}
		sem = over
	}
	flows := map[string]*flow.Definition{}
	if cfg.FlowFile != "" {
		data, err := os.ReadFile(cfg.FlowFile)
		if err != nil {
			return nil, config.File{}, fmt.Errorf("read flow: %w", err)
		}
		def, err := flow.Parse(data)
		if err != nil {
			return nil, config.File{}, err
		}
		flows[def.Name] = def
	}
	pages := 0
	if cfg.Page == "follow" {
		pages = 5
	}
	loop, err := agent.New(cat, sem, execute.Client{
		BaseURL:     baseURL,
		HTTP:        auth.WithEnvProxy(&http.Client{Timeout: cfg.Timeout}),
		Auth:        authSecrets(cfg.Auth),
		Creds:       authResolver(cfg),
		FollowPages: pages,
		Fields:      cfg.ResponseFields,
		Limit:       cfg.ResponseLimit,
	})
	if err != nil {
		return nil, config.File{}, err
	}
	if err := applyApprovalEnv(loop.State, cfg.ApprovalTTL); err != nil {
		return nil, config.File{}, err
	}
	loop.Flows = flows
	if err := applyProviders(loop, cfg); err != nil {
		return nil, cfg, err
	}
	return loop, cfg, nil
}

// applyProviders constructs the providers named in the config. The accepted values are the defaults.
func applyProviders(loop *agent.Loop, cfg config.File) error {
	switch cfg.Memory {
	case "local":
		loop.Memory = memory.NewLocalMap()
	case "file":
		log, err := memory.NewLog(cfg.MemoryFile)
		if err != nil {
			return err
		}
		loop.Memory = log
	default:
		return fmt.Errorf("memory provider %q is not in this slice", cfg.Memory)
	}
	base := policy.Builtin{Caller: callerName(cfg.Caller), Allow: allowSet(cfg.Permissions)}
	switch cfg.Policy {
	case "builtin":
		loop.SetPolicy(base)
		loop.SetFloor(base)
	case "opa":
		eng, err := opa.New(context.Background(), cfg.PolicyFile, cfg.PolicyBundle, base)
		if err != nil {
			return err
		}
		eng.Environment = cfg.Environment
		if cfg.Caller != "" {
			eng.Principal = cfg.Caller
		}
		loop.SetPolicy(eng)
		loop.SetFloor(base)
	default:
		return fmt.Errorf("unsupported policy provider %q", cfg.Policy)
	}
	if cfg.ApprovalWebhook.URL != "" || len(cfg.ApprovalWebhook.Command) > 0 {
		hook, err := policy.NewWebhook(cfg.ApprovalWebhook.URL, append([]string(nil), cfg.ApprovalWebhook.Command...))
		if err != nil {
			return err
		}
		loop.Notify = hook
	}
	if cfg.Execution != "in-process" {
		return fmt.Errorf("execution provider %q is not in this slice", cfg.Execution)
	}
	if cfg.Decision != "default" {
		return fmt.Errorf("decision provider %q is not in this slice", cfg.Decision)
	}
	switch cfg.Model {
	case "scripted":
		loop.Model = agent.NewScripted()
	case "openai":
		live, err := openai.New(cfg.ModelBaseURL, os.Getenv("OPENAI_API_KEY"), cfg.ModelName)
		if err != nil {
			return err
		}
		loop.Model = live
	default:
		return fmt.Errorf("model provider %q is not in this slice", cfg.Model)
	}
	return nil
}
