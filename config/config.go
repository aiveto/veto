package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type File struct {
	Model         string            `yaml:"model"`
	ModelName     string            `yaml:"model_name"`
	ModelBaseURL  string            `yaml:"model_base_url"`
	Memory        string            `yaml:"memory"`
	Semantics     string            `yaml:"semantics"`
	SemanticsFile string            `yaml:"semantics_file"`
	Decision      string            `yaml:"decision"`
	Policy        string            `yaml:"policy"`
	Telemetry     string            `yaml:"telemetry"`
	TraceExport   string            `yaml:"trace_export"`
	Execution     string            `yaml:"execution"`
	Subagents     string            `yaml:"subagents"`
	FlowFile      string            `yaml:"flow_file"`
	AgentFile     string            `yaml:"agent_file"`
	RelationsFile string            `yaml:"relations_file"`
	Contracts     []string          `yaml:"contracts"`
	ReplayRedact  string            `yaml:"replay_redact"`
	TraceFile     string            `yaml:"trace_file"`
	Timeout       time.Duration     `yaml:"timeout"`
	Auth          map[string]string `yaml:"auth"`
	Server        string            `yaml:"server"`
	Page          string            `yaml:"page"`
	Caller        string            `yaml:"caller"`
	Permissions   []string          `yaml:"permissions"`
	MemoryFile    string            `yaml:"memory_file"`
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
	if cfg.TraceFile != "" && !filepath.IsAbs(cfg.TraceFile) {
		cfg.TraceFile = filepath.Join(dir, cfg.TraceFile)
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
	if f.Decision != "default" && f.Decision != "jev" {
		return fmt.Errorf("decision provider %q is not in this slice", f.Decision)
	}
	switch f.Policy {
	case "builtin", "opa", "spicedb":
	default:
		return fmt.Errorf("policy provider %q is not in this slice", f.Policy)
	}
	if f.Telemetry != "otel" {
		return fmt.Errorf("telemetry provider %q is not in this slice", f.Telemetry)
	}
	if f.TraceExport != "" && f.TraceExport != "stdout" && f.TraceExport != "otlp" {
		return fmt.Errorf("trace export %q is not in this slice", f.TraceExport)
	}
	if f.Execution != "in-process" && f.Execution != "temporal" {
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
	return nil
}

// An unset key redacts.
func (f File) Redact() bool {
	return f.ReplayRedact != "false"
}
