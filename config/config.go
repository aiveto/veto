package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type File struct {
	Model         string        `yaml:"model"`
	ModelName     string        `yaml:"model_name"`
	ModelBaseURL  string        `yaml:"model_base_url"`
	Memory        string        `yaml:"memory"`
	Semantics     string        `yaml:"semantics"`
	SemanticsFile string        `yaml:"semantics_file"`
	Decision      string        `yaml:"decision"`
	Policy        string        `yaml:"policy"`
	PolicyFile    string        `yaml:"policy_file"`
	PolicyBundle  string        `yaml:"policy_bundle"`
	Environment   string        `yaml:"environment"`
	Telemetry     string        `yaml:"telemetry"`
	TraceExport   string        `yaml:"trace_export"`
	Execution     string        `yaml:"execution"`
	Subagents     string        `yaml:"subagents"`
	FlowFile      string        `yaml:"flow_file"`
	AgentFile     string        `yaml:"agent_file"`
	RelationsFile string        `yaml:"relations_file"`
	Contracts     []string      `yaml:"contracts"`
	ReplayRedact  string        `yaml:"replay_redact"`
	TraceFile     string        `yaml:"trace_file"`
	Timeout       time.Duration `yaml:"timeout"`
	Auth          Sources       `yaml:"auth"`
	TokenDir      string        `yaml:"token_dir"`
	Server        string        `yaml:"server"`
	Page          string        `yaml:"page"`
	Caller        string        `yaml:"caller"`
	Permissions   []string      `yaml:"permissions"`
	MemoryFile    string        `yaml:"memory_file"`
}

func Defaults() File {
	return File{
		Model:       "scripted",
		Memory:      "local",
		Semantics:   "derived",
		Decision:    "default",
		Policy:      "builtin",
		Telemetry:   "otel",
		Execution:   "in-process",
		Subagents:   "off",
		TraceExport: "",
		Timeout:     30 * time.Second,
	}
}

// Relative paths are resolved from the config file.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read config: %w", err)
	}
	cfg := Defaults()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return File{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	dir := filepath.Dir(path)
	if cfg.SemanticsFile != "" && !filepath.IsAbs(cfg.SemanticsFile) {
		cfg.SemanticsFile = filepath.Join(dir, cfg.SemanticsFile)
	}
	if cfg.FlowFile != "" && !filepath.IsAbs(cfg.FlowFile) {
		cfg.FlowFile = filepath.Join(dir, cfg.FlowFile)
	}
	if cfg.AgentFile != "" && !filepath.IsAbs(cfg.AgentFile) {
		cfg.AgentFile = filepath.Join(dir, cfg.AgentFile)
	}
	if cfg.RelationsFile != "" && !filepath.IsAbs(cfg.RelationsFile) {
		cfg.RelationsFile = filepath.Join(dir, cfg.RelationsFile)
	}
	if cfg.MemoryFile != "" && !filepath.IsAbs(cfg.MemoryFile) {
		cfg.MemoryFile = filepath.Join(dir, cfg.MemoryFile)
	}
	if cfg.PolicyFile != "" && !filepath.IsAbs(cfg.PolicyFile) {
		cfg.PolicyFile = filepath.Join(dir, cfg.PolicyFile)
	}
	if cfg.PolicyBundle != "" && !filepath.IsAbs(cfg.PolicyBundle) {
		cfg.PolicyBundle = filepath.Join(dir, cfg.PolicyBundle)
	}
	if cfg.TraceFile != "" && !filepath.IsAbs(cfg.TraceFile) {
		cfg.TraceFile = filepath.Join(dir, cfg.TraceFile)
	}
	if cfg.TokenDir != "" && !filepath.IsAbs(cfg.TokenDir) {
		cfg.TokenDir = filepath.Join(dir, cfg.TokenDir)
	}
	for i, name := range cfg.Contracts {
		if name != "" && !filepath.IsAbs(name) {
			cfg.Contracts[i] = filepath.Join(dir, name)
		}
	}
	if err := cfg.validate(); err != nil {
		return File{}, err
	}
	return cfg, nil
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
	if f.Decision == "" {
		f.Decision = d.Decision
	}
	if f.Policy == "" {
		f.Policy = d.Policy
	}
	if f.Telemetry == "" {
		f.Telemetry = d.Telemetry
	}
	if f.Execution == "" {
		f.Execution = d.Execution
	}
	if f.Subagents == "" {
		f.Subagents = d.Subagents
	}
	if f.Timeout <= 0 {
		f.Timeout = d.Timeout
	}
}

func (f File) validate() error {
	if f.Model != "scripted" && f.Model != "openai" {
		return fmt.Errorf("model provider %q is not in this slice", f.Model)
	}
	if f.Memory != "local" && f.Memory != "file" {
		return fmt.Errorf("memory provider %q is not in this slice", f.Memory)
	}
	if f.Memory == "file" && f.MemoryFile == "" {
		return fmt.Errorf("memory file provider needs memory_file")
	}
	if f.Semantics != "derived" && f.Semantics != "file" {
		return fmt.Errorf("semantics provider %q is not in this slice", f.Semantics)
	}
	if f.Semantics == "file" && f.SemanticsFile == "" {
		return fmt.Errorf("semantics file provider needs semantics_file")
	}
	if f.Decision == "jev" {
		return fmt.Errorf("decision provider %q is not implemented", f.Decision)
	}
	if f.Decision != "default" {
		return fmt.Errorf("decision provider %q is not in this slice", f.Decision)
	}
	switch f.Policy {
	case "builtin", "opa":
	case "spicedb":
		return fmt.Errorf("policy provider %q is not implemented", f.Policy)
	default:
		return fmt.Errorf("policy provider %q is not in this slice", f.Policy)
	}
	if f.Policy == "opa" && f.PolicyFile != "" && f.PolicyBundle != "" {
		return fmt.Errorf("policy opa takes policy_file or policy_bundle")
	}
	if f.Telemetry != "otel" {
		return fmt.Errorf("telemetry provider %q is not in this slice", f.Telemetry)
	}
	if f.TraceExport != "" && f.TraceExport != "stdout" && f.TraceExport != "otlp" {
		return fmt.Errorf("trace export %q is not in this slice", f.TraceExport)
	}
	if f.Execution == "temporal" {
		return fmt.Errorf("execution provider %q is not implemented", f.Execution)
	}
	if f.Execution != "in-process" {
		return fmt.Errorf("execution provider %q is not in this slice", f.Execution)
	}
	if f.Subagents != "off" {
		return fmt.Errorf("subagents are not in this slice")
	}
	if f.ReplayRedact != "" && f.ReplayRedact != "true" && f.ReplayRedact != "false" {
		return fmt.Errorf("replay_redact %q is not true or false", f.ReplayRedact)
	}
	if f.Page != "" && f.Page != "follow" {
		return fmt.Errorf("page %q is not follow", f.Page)
	}
	for name, src := range f.Auth {
		if err := src.validate(name); err != nil {
			return err
		}
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
	switch s.Kind() {
	case "env", "invoke":
		return nil
	case "login":
		if s.ClientID == "" {
			return fmt.Errorf("auth %s: client_id required", name)
		}
		if s.Issuer == "" && (s.AuthorizationURL == "" || s.TokenURL == "") {
			return fmt.Errorf("auth %s: authorization_url and token_url, or issuer", name)
		}
		return nil
	case "client_credentials":
		if s.TokenURL == "" || s.ClientID == "" || s.ClientSecretEnv == "" {
			return fmt.Errorf("auth %s: token_url, client_id, and client_secret_env required", name)
		}
		return nil
	case "command":
		if len(s.Command) == 0 {
			return fmt.Errorf("auth %s: command required", name)
		}
		return nil
	case "token_exchange":
		if s.TokenURL == "" || s.Audience == "" || s.ClientID == "" || s.ClientSecretEnv == "" || s.Subject == "" {
			return fmt.Errorf("auth %s: token_url, audience, client_id, client_secret_env, and subject required", name)
		}
		return nil
	default:
		return fmt.Errorf("auth source %q is not in this slice", s.Kind())
	}
}

// An unset key redacts.
func (f File) Redact() bool {
	return f.ReplayRedact != "false"
}
