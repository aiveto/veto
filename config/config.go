// Package config loads veto.yaml. An unset key keeps the default.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/aiveto/veto/internal/yamlfile"
	"gopkg.in/yaml.v3"
)

var (
	modelProviders      = []string{"scripted", "openai"}
	memoryProviders     = []string{"local", "file"}
	semanticsProviders  = []string{"derived", "file"}
	policyProviders     = []string{"builtin", "opa"}
	authSourceProviders = []string{"env", "invoke", "login", "client_credentials", "command", "token_exchange"}
)

type File struct {
	Model           string            `yaml:"model"`
	ModelName       string            `yaml:"model_name"`
	ModelBaseURL    string            `yaml:"model_base_url"`
	Memory          string            `yaml:"memory"`
	Semantics       string            `yaml:"semantics"`
	SemanticsFile   string            `yaml:"semantics_file"`
	Policy          string            `yaml:"policy"`
	PolicyFile      string            `yaml:"policy_file"`
	PolicyBundle    string            `yaml:"policy_bundle"`
	Environment     string            `yaml:"environment"`
	TraceExport     string            `yaml:"trace_export"`
	FlowFile        string            `yaml:"flow_file"`
	AgentFile       string            `yaml:"agent_file"`
	RelationsFile   string            `yaml:"relations_file"`
	Contracts       []string          `yaml:"contracts"`
	Bundle          string            `yaml:"bundle"`
	Cases           []string          `yaml:"cases"`
	ReplayRedact    *bool             `yaml:"replay_redact"`
	TraceFile       string            `yaml:"trace_file"`
	Timeout         time.Duration     `yaml:"timeout"`
	Auth            Sources           `yaml:"auth"`
	TokenDir        string            `yaml:"token_dir"`
	Server          string            `yaml:"server"`
	Page            string            `yaml:"page"`
	ResponseFields  []string          `yaml:"response_fields"`
	ResponseLimit   int               `yaml:"response_limit"`
	InvokeLimit     int               `yaml:"invoke_limit"`
	Caller          string            `yaml:"caller"`
	Callers         map[string]string `yaml:"callers"`
	Confirmation    *bool             `yaml:"confirmation"`
	ApprovalTTL     time.Duration     `yaml:"approval_ttl"`
	ApprovalStore   string            `yaml:"approval_store"`
	ApprovalWebhook Webhook           `yaml:"approval_webhook"`
	ChatApproval    bool              `yaml:"chat_approval"`
	ReadOnly        bool              `yaml:"read_only"`
	Expose          Expose            `yaml:"expose"`
	Permissions     []string          `yaml:"permissions"`
	MemoryFile      string            `yaml:"memory_file"`
}

// Expose keeps operations with a listed tag or under a listed path; both empty keeps every operation.
type Expose struct {
	Tags  []string `yaml:"tags"`
	Paths []string `yaml:"paths"`
}

func knownProvider(got string, want []string) bool {
	return slices.Contains(want, got)
}

func unknownProvider(kind, got string, want []string) error {
	if knownProvider(got, want) {
		return nil
	}
	return fmt.Errorf("%s provider %q is not in this slice", kind, got)
}

func Defaults() File {
	return File{
		Model:     "scripted",
		Memory:    "local",
		Semantics: "derived",
		Policy:    "builtin",
		Timeout:   30 * time.Second,
	}
}

// Relative paths are resolved from the config file.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read config: %w", err)
	}
	cfg := Defaults()
	if err := yamlfile.Prepare(data); err != nil {
		return File{}, fmt.Errorf("parse config: %w", err)
	}
	if err := rejectUnknown(data); err != nil {
		return File{}, fmt.Errorf("parse config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return File{}, fmt.Errorf("parse config: %w", err)
	}
	var extra yaml.Node
	err = dec.Decode(&extra)
	if err == nil {
		return File{}, fmt.Errorf("parse config: %w", errors.New("extra document"))
	}
	if !errors.Is(err, io.EOF) {
		return File{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	dir := filepath.Dir(path)
	cfg.SemanticsFile = resolvePath(dir, cfg.SemanticsFile)
	cfg.FlowFile = resolvePath(dir, cfg.FlowFile)
	cfg.AgentFile = resolvePath(dir, cfg.AgentFile)
	cfg.RelationsFile = resolvePath(dir, cfg.RelationsFile)
	cfg.MemoryFile = resolvePath(dir, cfg.MemoryFile)
	cfg.PolicyFile = resolvePath(dir, cfg.PolicyFile)
	cfg.PolicyBundle = resolvePath(dir, cfg.PolicyBundle)
	cfg.TraceFile = resolvePath(dir, cfg.TraceFile)
	cfg.TokenDir = resolvePath(dir, cfg.TokenDir)
	cfg.Bundle = resolvePath(dir, cfg.Bundle)
	if len(cfg.ApprovalWebhook.Command) > 0 {
		bin := cfg.ApprovalWebhook.Command[0]
		if strings.Contains(bin, string(filepath.Separator)) {
			cfg.ApprovalWebhook.Command[0] = resolvePath(dir, bin)
		}
	}
	for i, name := range cfg.Contracts {
		if !remoteContract(name) {
			cfg.Contracts[i] = resolvePath(dir, name)
		}
	}
	for i, name := range cfg.Cases {
		cfg.Cases[i] = resolvePath(dir, name)
	}
	if err := cfg.validate(); err != nil {
		return File{}, err
	}
	return cfg, nil
}

// Confirms reports whether this deployment keeps the confirmation gate. Unset means it does.
func (f *File) Confirms() bool {
	if f == nil || f.Confirmation == nil {
		return true
	}
	return *f.Confirmation
}

func (f *File) applyDefaults() {
	d := Defaults()
	if f.Model == "" {
		f.Model = d.Model
	}
	if f.Memory == "" {
		f.Memory = d.Memory
	}
	if f.Semantics == "" {
		f.Semantics = d.Semantics
	}
	if f.Policy == "" {
		f.Policy = d.Policy
	}
	if f.Timeout <= 0 {
		f.Timeout = d.Timeout
	}
}

func (f *File) validate() error {
	if err := unknownProvider("model", f.Model, modelProviders); err != nil {
		return err
	}
	if err := unknownProvider("memory", f.Memory, memoryProviders); err != nil {
		return err
	}
	if f.Memory == "file" && f.MemoryFile == "" {
		return errors.New("memory file provider needs memory_file")
	}
	if err := unknownProvider("semantics", f.Semantics, semanticsProviders); err != nil {
		return err
	}
	if f.Semantics == "file" && f.SemanticsFile == "" {
		return errors.New("semantics file provider needs semantics_file")
	}
	if !knownProvider(f.Policy, policyProviders) {
		return fmt.Errorf("unsupported policy provider %q", f.Policy)
	}
	if f.Policy == "opa" && f.PolicyFile != "" && f.PolicyBundle != "" {
		return errors.New("policy opa takes policy_file or policy_bundle")
	}
	if f.TraceExport != "" && f.TraceExport != "stdout" && f.TraceExport != "otlp" {
		return fmt.Errorf("trace export %q is not in this slice", f.TraceExport)
	}
	if f.Page != "" && f.Page != "follow" {
		return fmt.Errorf("page %q is not follow", f.Page)
	}
	if f.ResponseLimit < 0 {
		return errors.New("response_limit is negative")
	}
	if f.InvokeLimit < 0 {
		return errors.New("invoke_limit is negative")
	}
	for name, src := range f.Auth {
		if err := src.validate(name); err != nil {
			return err
		}
	}
	for id, env := range f.Callers {
		if id == "" || env == "" {
			return errors.New("caller credential required")
		}
	}
	if err := f.ApprovalWebhook.validate(); err != nil {
		return err
	}
	if f.ApprovalTTL < 0 {
		return errors.New("approval_ttl is negative")
	}
	if f.ApprovalStore != "" && (strings.Contains(f.ApprovalStore, "://") || strings.ContainsAny(f.ApprovalStore, "/:\\")) {
		return errors.New("approval_store is an environment variable name")
	}
	for _, p := range f.Expose.Paths {
		if !strings.HasPrefix(p, "/") {
			return fmt.Errorf("expose path %q does not start with /", p)
		}
	}
	return nil
}

// Webhook is a command or an HTTP URL. It is called when a call is pending.
type Webhook struct {
	URL     string
	Command []string
}

func (w *Webhook) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var one string
		if err := node.Decode(&one); err != nil {
			return err
		}
		one = strings.TrimSpace(one)
		if one == "" {
			return nil
		}
		if strings.Contains(one, "://") {
			w.URL = one
			return nil
		}
		w.Command = []string{one}
		return nil
	case yaml.SequenceNode:
		var parts []string
		if err := node.Decode(&parts); err != nil {
			return err
		}
		w.Command = parts
		return nil
	case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
		return errors.New("approval_webhook is a command or an HTTP URL")
	default:
		return errors.New("approval_webhook is a command or an HTTP URL")
	}
}

func (w Webhook) validate() error {
	if w.URL == "" && len(w.Command) == 0 {
		return nil
	}
	if w.URL != "" && len(w.Command) > 0 {
		return errors.New("approval_webhook is a command or an HTTP URL")
	}
	if w.URL != "" {
		u, err := url.Parse(w.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("approval_webhook URL is not http or https")
		}
		return nil
	}
	if strings.TrimSpace(w.Command[0]) == "" {
		return errors.New("approval_webhook command required")
	}
	return nil
}

type (
	// Sources maps an OpenAPI security scheme name to where its credential comes from.
	// A string value is the environment variable for source env.
	Sources map[string]Source

	CommandLine []string

	Source struct {
		Source                 string      `yaml:"source"`
		Env                    string      `yaml:"env"`
		ClientID               string      `yaml:"client_id"`
		ClientSecretEnv        string      `yaml:"client_secret_env"`
		AuthorizationURL       string      `yaml:"authorization_url"`
		TokenURL               string      `yaml:"token_url"`
		Issuer                 string      `yaml:"issuer"`
		DeviceAuthorizationURL string      `yaml:"device_authorization_url"`
		RedirectURL            string      `yaml:"redirect_url"`
		Scopes                 []string    `yaml:"scopes"`
		Audience               string      `yaml:"audience"`
		Header                 string      `yaml:"header"`
		Command                CommandLine `yaml:"command"`
		Timeout                Duration    `yaml:"timeout"`
		AuthToken              string      `yaml:"auth_token"`
		UserToken              string      `yaml:"user_token"`
		UserHeader             string      `yaml:"user_header"`
		Subject                string      `yaml:"subject"`
		SubjectTokenType       string      `yaml:"subject_token_type"`
	}

	Duration time.Duration
)

func (s Source) Kind() string {
	if s.Source == "" {
		return "env"
	}
	return s.Source
}

func (s *Source) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var env string
		if err := node.Decode(&env); err != nil {
			return err
		}
		*s = Source{Source: "env", Env: env}
		return nil
	}
	if err := unknownKeys(node, reflect.TypeFor[Source]()); err != nil {
		return err
	}
	type plain Source
	var decoded plain
	if err := node.Decode(&decoded); err != nil {
		return err
	}
	*s = Source(decoded)
	if s.Source == "" {
		s.Source = "env"
	}
	return nil
}

func rejectUnknown(data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return err
	}
	return unknownKeys(&node, reflect.TypeFor[File]())
}

func unknownKeys(node *yaml.Node, typ reflect.Type) error {
	return walkConfig(node, typ, map[*yaml.Node]struct{}{})
}

func walkConfig(node *yaml.Node, typ reflect.Type, active map[*yaml.Node]struct{}) error {
	if node == nil {
		return nil
	}
	if _, seen := active[node]; seen {
		return errors.New("yaml alias cycle")
	}
	active[node] = struct{}{}
	defer delete(active, node)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil
		}
		return walkConfig(node.Content[0], typ, active)
	case yaml.AliasNode:
		return walkConfig(node.Alias, typ, active)
	case yaml.ScalarNode:
		return nil
	case yaml.MappingNode:
		seenKey := map[string]struct{}{}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if _, ok := seenKey[key]; ok {
				return fmt.Errorf("duplicate field %q", key)
			}
			seenKey[key] = struct{}{}
		}
		if typ.Kind() == reflect.Map {
			elem := typ.Elem()
			for i := 1; i < len(node.Content); i += 2 {
				if err := walkConfig(node.Content[i], elem, active); err != nil {
					return err
				}
			}
		}
		if typ.Kind() == reflect.Struct {
			fields := yamlFields(typ)
			for i := 0; i+1 < len(node.Content); i += 2 {
				key := node.Content[i].Value
				ft, ok := fields[key]
				if !ok {
					return fmt.Errorf("unknown config field %q", key)
				}
				if err := walkConfig(node.Content[i+1], ft, active); err != nil {
					return err
				}
			}
		}
		if typ.Kind() != reflect.Map && typ.Kind() != reflect.Struct {
			for i := 1; i < len(node.Content); i += 2 {
				if err := walkConfig(node.Content[i], typ, active); err != nil {
					return err
				}
			}
		}
	case yaml.SequenceNode:
		elem := typ
		if typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
			elem = typ.Elem()
		}
		for _, item := range node.Content {
			if err := walkConfig(item, elem, active); err != nil {
				return err
			}
		}
	}
	return nil
}

func yamlFields(typ reflect.Type) map[string]reflect.Type {
	out := make(map[string]reflect.Type, typ.NumField())
	for f := range typ.Fields() {
		if f.PkgPath != "" {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tag == "-" {
			continue
		}
		name := strings.ToLower(f.Name)
		if tag != "" {
			part, _, _ := strings.Cut(tag, ",")
			if part == "-" {
				continue
			}
			if part != "" {
				name = part
			}
		}
		out[name] = f.Type
	}
	return out
}

func (c *CommandLine) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		var one string
		if err := node.Decode(&one); err != nil {
			return err
		}
		if one == "" {
			*c = nil
			return nil
		}
		*c = CommandLine{one}
		return nil
	}
	var parts []string
	if err := node.Decode(&parts); err != nil {
		return err
	}
	*c = parts
	return nil
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!int" {
		var n int64
		if err := node.Decode(&n); err != nil {
			return err
		}
		*d = Duration(time.Duration(n))
		return nil
	}
	var text string
	if err := node.Decode(&text); err != nil {
		return err
	}
	if text == "" {
		return nil
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return fmt.Errorf("timeout: %w", err)
	}
	*d = Duration(parsed)
	return nil
}

func (s Source) validate(name string) error {
	if !knownProvider(s.Kind(), authSourceProviders) {
		return fmt.Errorf("auth source %q is not in this slice", s.Kind())
	}
	switch s.Kind() {
	case "login":
		if s.ClientID == "" {
			return fmt.Errorf("auth %s: client_id required", name)
		}
		if s.Issuer == "" && (s.AuthorizationURL == "" || s.TokenURL == "") {
			return fmt.Errorf("auth %s: authorization_url and token_url, or issuer", name)
		}
	case "client_credentials":
		if s.TokenURL == "" || s.ClientID == "" || s.ClientSecretEnv == "" {
			return fmt.Errorf("auth %s: token_url, client_id, and client_secret_env required", name)
		}
	case "command":
		if len(s.Command) == 0 {
			return fmt.Errorf("auth %s: command required", name)
		}
	case "token_exchange":
		if s.TokenURL == "" || s.Audience == "" || s.ClientID == "" || s.ClientSecretEnv == "" || s.Subject == "" {
			return fmt.Errorf("auth %s: token_url, audience, client_id, client_secret_env, and subject required", name)
		}
	}
	return nil
}

func remoteContract(name string) bool {
	return strings.HasPrefix(name, "https://") || strings.HasPrefix(name, "http://")
}

func resolvePath(dir, name string) string {
	if name == "" || filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(dir, name)
}

// An unset key redacts.
func (f *File) Redact() bool {
	return f.ReplayRedact == nil || *f.ReplayRedact
}
