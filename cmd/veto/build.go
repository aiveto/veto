package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/aiveto/veto/agent"
	"github.com/aiveto/veto/agent/openai"
	"github.com/aiveto/veto/agentmeta"
	"github.com/aiveto/veto/auth"
	"github.com/aiveto/veto/bundle"
	"github.com/aiveto/veto/capability"
	"github.com/aiveto/veto/catalog"
	"github.com/aiveto/veto/config"
	"github.com/aiveto/veto/execute"
	"github.com/aiveto/veto/flow"
	"github.com/aiveto/veto/memory"
	"github.com/aiveto/veto/opa"
	"github.com/aiveto/veto/openapi"
	"github.com/aiveto/veto/policy"
	"github.com/aiveto/veto/runtime"
	"github.com/aiveto/veto/semantics"
)

type sources struct {
	cfg       config.File
	contracts []string
	relations string
	agent     string
}

var (
	bundleMu    sync.Mutex
	openBundles []*bundle.Loaded
)

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
		JSON:           cfg.JSONSet(),
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
		holdBundle(shared)
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

func holdBundle(l *bundle.Loaded) {
	if l == nil {
		return
	}
	bundleMu.Lock()
	openBundles = append(openBundles, l)
	bundleMu.Unlock()
}

func releaseBundles() {
	bundleMu.Lock()
	all := openBundles
	openBundles = nil
	bundleMu.Unlock()
	var err error
	for _, l := range all {
		err = errors.Join(err, l.Close())
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bundle: %v\n", err)
	}
}

func buildLoop(contracts []string, configPath, agentPath, relationsPath, baseURL string) (*agent.Loop, config.File, error) {
	return buildLoopBundle(contracts, configPath, "", agentPath, relationsPath, baseURL)
}

func buildEvalLoop(contracts []string, configPath, agentPath, relationsPath, baseURL string) (*agent.Loop, config.File, error) {
	src, err := resolveBundle(configPath, "", contracts, relationsPath, agentPath)
	if err != nil {
		return nil, config.File{}, err
	}
	src.cfg.Model = "scripted"
	return assembleLoop(src, baseURL)
}

func buildLoopBundle(contracts []string, configPath, bundlePath, agentPath, relationsPath, baseURL string) (*agent.Loop, config.File, error) {
	src, err := resolveBundle(configPath, bundlePath, contracts, relationsPath, agentPath)
	if err != nil {
		return nil, config.File{}, err
	}
	return assembleLoop(src, baseURL)
}

func assembleLoop(src sources, baseURL string) (*agent.Loop, config.File, error) {
	srv, cfg, err := assembleKernel(src, baseURL)
	if err != nil {
		return nil, config.File{}, err
	}
	loop, err := agent.New(srv.Catalog, srv.Semantics, srv.Calls.Exec)
	if err != nil {
		return nil, config.File{}, err
	}
	loop.SetRuntime(srv.Calls)
	loop.Flows = srv.Flows
	if loop.Packs != nil {
		loop.Packs.Tasks = flow.Lines(srv.Flows)
	}
	if err := applyAgentProviders(loop, cfg); err != nil {
		return nil, cfg, err
	}
	return loop, cfg, nil
}

func assembleKernel(src sources, baseURL string) (*capability.Server, config.File, error) {
	cfg := src.cfg
	cat, sem, err := loadKernel(src)
	if err != nil {
		return nil, config.File{}, err
	}
	pages := 0
	if cfg.Page == "follow" {
		pages = 5
	}
	exec := execute.Client{
		BaseURL: baseURL,
		HTTP:    auth.WithEnvProxy(&http.Client{Timeout: cfg.Timeout}),
		Auth:    authSecrets(cfg.Auth),
		Creds:   authResolver(cfg),
		Fields:  cfg.ResponseFields,
		Limit:   cfg.ResponseLimit,
	}
	rt := runtime.Runtime{
		Catalog: cat,
		Exec:    exec,
		State:   policy.NewState(),
		JSON:    cfg.JSONSet(),
		Gate:    &runtime.InvokeGate{},
		Pages:   pages,
	}
	rt.Gate.Per = cfg.InvokeLimit
	if err := applyApprovalConfig(context.Background(), rt.State, cfg); err != nil {
		return nil, config.File{}, err
	}
	if err := applyRuntimePolicy(&rt, cfg); err != nil {
		return nil, config.File{}, err
	}
	flows, err := loadFlows(cfg, cat)
	if err != nil {
		return nil, config.File{}, err
	}
	return &capability.Server{Catalog: cat, Semantics: sem, Calls: &rt, Flows: flows}, cfg, nil
}

func loadKernel(src sources) (*catalog.Catalog, semantics.Notes, error) {
	cfg := src.cfg
	agentPath := src.agent
	cat, err := loadCatalog(src.contracts, src.relations)
	if err != nil {
		return nil, nil, err
	}
	if agentPath == "" {
		agentPath = cfg.AgentFile
	}
	if err := applyAgent(cat, agentPath); err != nil {
		return nil, nil, err
	}
	applyDeployment(cat, cfg)
	if err := cat.SelectServer(cfg.Server); err != nil {
		return nil, nil, err
	}
	var sem semantics.Notes = semantics.New(cat)
	if cfg.SemanticsFile != "" {
		data, err := os.ReadFile(cfg.SemanticsFile)
		if err != nil {
			return nil, nil, fmt.Errorf("read semantics: %w", err)
		}
		over, err := semantics.ParseOverlay(data, sem)
		if err != nil {
			return nil, nil, err
		}
		sem = over
	}
	return cat, sem, nil
}

func loadFlows(cfg config.File, cat *catalog.Catalog) (map[string]*flow.Definition, error) {
	flows := map[string]*flow.Definition{}
	if cfg.FlowFile == "" {
		return flows, nil
	}
	data, err := os.ReadFile(cfg.FlowFile)
	if err != nil {
		return nil, fmt.Errorf("read flow: %w", err)
	}
	defs, err := flow.ParseFile(data)
	if err != nil {
		return nil, err
	}
	for _, def := range defs {
		taught, err := flow.Teach(def, cat)
		if err != nil {
			return nil, err
		}
		if _, ok := flows[taught.Name]; ok {
			return nil, fmt.Errorf("duplicate flow %s", taught.Name)
		}
		flows[taught.Name] = taught
	}
	return flows, nil
}

func applyRuntimePolicy(rt *runtime.Runtime, cfg config.File) error {
	base := policy.Builtin{Caller: auth.OrLocal(cfg.Caller), Allow: allowSet(cfg.Permissions)}
	switch cfg.Policy {
	case "opa":
		eng, err := opa.New(context.Background(), cfg.PolicyFile, cfg.PolicyBundle, base)
		if err != nil {
			return err
		}
		eng.Environment = cfg.Environment
		if cfg.Caller != "" {
			eng.Principal = cfg.Caller
		}
		rt.Policy = eng
		rt.Base = base
	default:
		rt.Policy = base
		rt.Base = base
	}
	if cfg.ApprovalWebhook.URL != "" || len(cfg.ApprovalWebhook.Command) > 0 {
		hook, err := policy.NewWebhook(cfg.ApprovalWebhook.URL, append([]string(nil), cfg.ApprovalWebhook.Command...))
		if err != nil {
			return err
		}
		rt.Notify = hook
	}
	return nil
}

func buildServer(contracts []string, configPath, agentPath, relationsPath, baseURL string) (*capability.Server, config.File, error) {
	return buildServerBundle(contracts, configPath, "", agentPath, relationsPath, baseURL)
}

func buildServerBundle(contracts []string, configPath, bundlePath, agentPath, relationsPath, baseURL string) (*capability.Server, config.File, error) {
	src, err := resolveBundle(configPath, bundlePath, contracts, relationsPath, agentPath)
	if err != nil {
		return nil, config.File{}, err
	}
	return assembleKernel(src, baseURL)
}

// applyAgentProviders constructs model and memory. Search and invoke do not call this.
func applyAgentProviders(loop *agent.Loop, cfg config.File) error {
	switch cfg.Memory {
	case "file":
		log, err := memory.NewLog(cfg.MemoryFile)
		if err != nil {
			return err
		}
		loop.Memory = log
	default:
		loop.Memory = memory.New()
	}
	switch cfg.Model {
	case "openai":
		live, err := openai.New(cfg.ModelBaseURL, os.Getenv("OPENAI_API_KEY"), cfg.ModelName)
		if err != nil {
			return err
		}
		loop.Model = live
	default:
		loop.Model = agent.NewScripted()
	}
	return nil
}
