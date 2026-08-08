// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package config is jevonsd's structured configuration (🎯T44 + 🎯T27.2).
// It owns every value that used to be compiled in — identity (owner and
// overseer names), the persona prompt, state paths, port, models, and
// external-provider declarations — so that no owner-specific identity
// lives in code. Precedence: built-in defaults, then
// ~/.jevons/config.yaml (or --config), then explicit flags.
//
// External ecosystem providers (mnemo, …) are declared under `providers:`
// (🎯T27.2). That list is distinct from the `provider` field (🎯T148),
// which selects the default claudia agent backend.
package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

//go:embed persona.md
var personaTemplate string

// Config carries jevonsd's configurable identity, paths, and defaults.
type Config struct {
	// OwnerName is how the overseer refers to its human (the human's name).
	// Empty means the neutral "the owner".
	OwnerName string `yaml:"owner_name"`
	// OverseerName is the registry name of the CEO agent. It names the
	// overseer's workdir under StateDir and its budget protection entry.
	OverseerName string `yaml:"overseer_name"`

	// BindAddr is the listen interface. The default is loopback-only
	// (🎯T6 safe default): remote devices connect through the pigeon
	// relay, not by exposing the daemon to the LAN. Set "0.0.0.0" to
	// bind all interfaces deliberately.
	BindAddr string `yaml:"bind_addr"`

	Port          int    `yaml:"port"`
	WorkDir       string `yaml:"workdir"`        // default workdir for workers
	// Provider is the default claudia agent backend id ("grok", "claude", …).
	// Empty = JEVONS_PROVIDER env, else grok (🎯T148). Not an allow-list —
	// unknown strings pass through to claudia.
	Provider      string `yaml:"provider"`
	Model         string `yaml:"model"`          // default worker model ("" = provider default)
	OverseerModel string `yaml:"overseer_model"` // "" = same as Model

	StateDir    string `yaml:"state_dir"`    // jevons state (registry, threads, usage.db, …)
	SessionsDir string `yaml:"sessions_dir"` // Grok sessions tree (~/.grok/sessions)
	// ClaudeProjects is the Claude Code projects tree (~/.claude/projects)
	// used for fleet inspect/status/transcript discovery (🎯T213). Empty
	// falls back to Default (~/.claude/projects). Not the parent ~/.claude.
	ClaudeProjects string `yaml:"claude_projects"`
	ReposRoot      string `yaml:"repos_root"` // where the owner's repos live

	// MCPServerName is the name under which jevonsd's MCP server is
	// registered in agent sessions; tools appear to the overseer as
	// <name>__jevons_*. Configurable because the Grok stack keeps
	// per-account, per-name server state: a name whose first contact
	// failed can stay broken, and recovery is picking a fresh name.
	MCPServerName string `yaml:"mcp_server_name"`

	// PersonaFile optionally replaces the built-in persona template
	// wholesale. PersonaNotes is appended to the rendered persona as an
	// "Owner Notes" section — the place for owner-specific repos, style,
	// and routing hints that must not live in code.
	PersonaFile  string `yaml:"persona_file"`
	PersonaNotes string `yaml:"persona_notes"`

	// Providers is the desired set of external ecosystem providers
	// (🎯T27.2 / parent 🎯T27). Distinct from Provider (agent backend).
	// Empty means no external providers are configured. Reloadable via
	// provider.ConfigManager without a full daemon restart.
	Providers []ProviderDecl `yaml:"providers"`

	// Portfolios are named domain groups of repos/products (🎯T200).
	// Membership is declarative path fragments matched against agent
	// workdirs — never agent-name parsing. Empty means no portfolio chrome.
	Portfolios []Portfolio `yaml:"portfolios"`

	// FrontierConsume tunes the 🎯T254.1 unattended frontier consumption
	// loop (daemon auto-spawns workers for unengaged ready frontier leaves
	// under the product PO). Zero value = enabled with conservative defaults.
	FrontierConsume FrontierConsumeConfig `yaml:"frontier_consume"`
}

// FrontierConsumeConfig is the 🎯T254.1 enforcement loop tuning. The loop is
// on by default (the target is daemon enforcement without owner spawn
// commands); Disabled opts out. Zero numeric fields use compiled defaults
// (10m interval, 1 spawn per cycle, 3 auto-spawns per target).
type FrontierConsumeConfig struct {
	Disabled           bool `yaml:"disabled"`
	IntervalMinutes    int  `yaml:"interval_minutes"`
	MaxSpawnsPerCycle  int  `yaml:"max_spawns_per_cycle"`
	MaxSpawnsPerTarget int  `yaml:"max_spawns_per_target"`
}

// Portfolio is one named domain group (e.g. personal, minicades, schools).
// Members are path fragments (e.g. "github.com/org/repo") that match agent
// workdirs by containment after normalization. No standing GM agent (🎯T201).
type Portfolio struct {
	// ID is the stable handle (required, unique among portfolios).
	ID string `yaml:"id" json:"id"`
	// Name is the owner-visible label; empty defaults to ID.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Members are declarative membership paths (repo/product roots).
	Members []string `yaml:"members" json:"members"`
}

// Transport modes for ProviderDecl (match internal/provider constants).
const (
	ProviderTransportLaunch  = "launch"
	ProviderTransportConnect = "connect"
)

// ProviderDecl is one external tool registration in structured config
// (🎯T27.2). Transport is launch (exec/argv) or connect (url); Params
// holds mode-specific keys and is additive for forward-compatible
// extensions (unknown keys are preserved, not rejected).
//
// YAML example:
//
//	providers:
//	  - id: mnemo
//	    transport: connect
//	    enable: true
//	    params:
//	      url: http://127.0.0.1:8741
//	  - id: pulse
//	    transport: launch
//	    enable: false
//	    params:
//	      exec: /usr/local/bin/pulse-provider
//	      argv: ["--serve", "--port", "9100"]
type ProviderDecl struct {
	ID        string         `yaml:"id" json:"id"`
	Transport string         `yaml:"transport" json:"transport"` // launch | connect
	Enable    *bool          `yaml:"enable,omitempty" json:"enable,omitempty"`
	Params    map[string]any `yaml:"params,omitempty" json:"params,omitempty"`
}

// Enabled reports whether this declaration is active. Omitted enable
// defaults to true (listed providers are desired unless disabled).
func (p ProviderDecl) Enabled() bool {
	if p.Enable == nil {
		return true
	}
	return *p.Enable
}

// ParamString returns a string param, or "" if missing/non-string.
func (p ProviderDecl) ParamString(key string) string {
	if p.Params == nil {
		return ""
	}
	v, ok := p.Params[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprint(v)
	}
	return s
}

// ParamStringSlice returns a string-slice param (argv). Accepts []any
// from YAML or []string; non-string elements are stringified.
func (p ProviderDecl) ParamStringSlice(key string) []string {
	if p.Params == nil {
		return nil
	}
	v, ok := p.Params[key]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		out := make([]string, len(t))
		copy(out, t)
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if e == nil {
				continue
			}
			if s, ok := e.(string); ok {
				out = append(out, s)
			} else {
				out = append(out, fmt.Sprint(e))
			}
		}
		return out
	default:
		return nil
	}
}

// LaunchExec is params.exec for transport=launch.
func (p ProviderDecl) LaunchExec() string { return p.ParamString("exec") }

// LaunchArgv is params.argv for transport=launch.
func (p ProviderDecl) LaunchArgv() []string { return p.ParamStringSlice("argv") }

// ConnectURL is params.url for transport=connect.
func (p ProviderDecl) ConnectURL() string { return p.ParamString("url") }

// ValidatePortfolios checks Portfolios entries (🎯T200). Empty list is
// valid (calm missing). Hard errors: missing id, duplicate ids, empty
// member path strings. Empty members on a portfolio is allowed (calm).
func ValidatePortfolios(list []Portfolio) error {
	seen := make(map[string]struct{}, len(list))
	for i, p := range list {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return fmt.Errorf("config: portfolios[%d]: id is required", i)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("config: portfolios: duplicate id %q", id)
		}
		seen[id] = struct{}{}
		for j, m := range p.Members {
			if strings.TrimSpace(m) == "" {
				return fmt.Errorf("config: portfolios[%q]: members[%d]: path is required", id, j)
			}
		}
	}
	return nil
}

// DisplayName returns the owner-visible portfolio label.
func (p Portfolio) DisplayName() string {
	if n := strings.TrimSpace(p.Name); n != "" {
		return n
	}
	return strings.TrimSpace(p.ID)
}

// ValidateProviders checks Providers entries. Empty list is valid.
// Unknown param keys are allowed (forward-compatible). Hard errors:
// missing id/transport, bad transport, duplicate ids, launch without
// exec/argv, connect without url.
func ValidateProviders(list []ProviderDecl) error {
	seen := make(map[string]struct{}, len(list))
	for i, p := range list {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return fmt.Errorf("config: providers[%d]: id is required", i)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("config: providers: duplicate id %q", id)
		}
		seen[id] = struct{}{}
		switch p.Transport {
		case ProviderTransportLaunch:
			if p.LaunchExec() == "" && len(p.LaunchArgv()) == 0 {
				return fmt.Errorf("config: providers[%q]: launch needs params.exec and/or params.argv", id)
			}
		case ProviderTransportConnect:
			if p.ConnectURL() == "" {
				return fmt.Errorf("config: providers[%q]: connect needs params.url", id)
			}
		case "":
			return fmt.Errorf("config: providers[%q]: transport is required (launch|connect)", id)
		default:
			return fmt.Errorf("config: providers[%q]: transport %q unsupported (want launch|connect)", id, p.Transport)
		}
	}
	return nil
}

// Default returns the out-of-the-box configuration, rooted at the
// user's home directory.
func Default() Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return Config{
		OverseerName:   "jevons",
		BindAddr:       "127.0.0.1",
		Port:           13705,
		WorkDir:        ".",
		StateDir:       filepath.Join(home, ".jevons"),
		SessionsDir:    filepath.Join(home, ".grok", "sessions"),
		ClaudeProjects: filepath.Join(home, ".claude", "projects"),
		ReposRoot:      filepath.Join(home, "work", "github.com"),
		MCPServerName:  "jevonsmcp",
	}
}

// DefaultPath returns the default config file location.
func DefaultPath() string { return filepath.Join(Default().StateDir, "config.yaml") }

// Load returns Default overlaid with the YAML file at path. A missing
// file is not an error — the defaults ARE the out-of-the-box config. A
// present-but-malformed file is a hard error (never silently ignore the
// owner's explicit configuration). Provider declarations are validated
// when present (🎯T27.2).
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}
	// A file that clears required fields falls back to defaults rather
	// than producing an unusable identity.
	def := Default()
	if cfg.OverseerName == "" {
		cfg.OverseerName = def.OverseerName
	}
	if cfg.BindAddr == "" {
		cfg.BindAddr = def.BindAddr
	}
	if cfg.Port == 0 {
		cfg.Port = def.Port
	}
	if cfg.StateDir == "" {
		cfg.StateDir = def.StateDir
	}
	if cfg.SessionsDir == "" {
		cfg.SessionsDir = def.SessionsDir
	}
	if cfg.ClaudeProjects == "" {
		cfg.ClaudeProjects = def.ClaudeProjects
	}
	if cfg.MCPServerName == "" {
		cfg.MCPServerName = def.MCPServerName
	}
	if err := ValidateProviders(cfg.Providers); err != nil {
		return cfg, err
	}
	if err := ValidatePortfolios(cfg.Portfolios); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// OwnerRef is the rendered reference to the human: the configured name,
// or a neutral fallback.
func (c Config) OwnerRef() string {
	if c.OwnerName != "" {
		return c.OwnerName
	}
	return "the owner"
}

// OverseerDir is the overseer agent's workdir (holds its generated
// AGENTS.md and .mcp.json).
func (c Config) OverseerDir() string { return filepath.Join(c.StateDir, c.OverseerName) }

// Persona renders the overseer's instructions: the built-in template
// (or PersonaFile) with this config's identity, plus PersonaNotes.
func (c Config) Persona() (string, error) {
	tmpl := personaTemplate
	if c.PersonaFile != "" {
		data, err := os.ReadFile(c.PersonaFile)
		if err != nil {
			return "", fmt.Errorf("config: persona_file: %w", err)
		}
		tmpl = string(data)
	}
	t, err := template.New("persona").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("config: persona template: %w", err)
	}
	var b strings.Builder
	if err := t.Execute(&b, c); err != nil {
		return "", fmt.Errorf("config: render persona: %w", err)
	}
	out := b.String()
	if notes := strings.TrimSpace(c.PersonaNotes); notes != "" {
		out += "\n## Owner Notes\n\n" + notes + "\n"
	}
	return out, nil
}
