// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package config is jevonsd's structured configuration (🎯T44). It owns
// every value that used to be compiled in — identity (owner and overseer
// names), the persona prompt, state paths, port, and models — so that no
// owner-specific identity lives in code. Precedence: built-in defaults,
// then ~/.jevons/config.yaml (or --config), then explicit flags.
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
	SessionsDir string `yaml:"sessions_dir"` // provider session store the collector/scanner tail
	ReposRoot   string `yaml:"repos_root"`   // where the owner's repos live

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
}

// Default returns the out-of-the-box configuration, rooted at the
// user's home directory.
func Default() Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return Config{
		OverseerName:  "jevons",
		BindAddr:      "127.0.0.1",
		Port:          13705,
		WorkDir:       ".",
		StateDir:      filepath.Join(home, ".jevons"),
		SessionsDir:   filepath.Join(home, ".grok", "sessions"),
		ReposRoot:     filepath.Join(home, "work", "github.com"),
		MCPServerName: "jevonsmcp",
	}
}

// DefaultPath returns the default config file location.
func DefaultPath() string { return filepath.Join(Default().StateDir, "config.yaml") }

// Load returns Default overlaid with the YAML file at path. A missing
// file is not an error — the defaults ARE the out-of-the-box config. A
// present-but-malformed file is a hard error (never silently ignore the
// owner's explicit configuration).
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
	if cfg.MCPServerName == "" {
		cfg.MCPServerName = def.MCPServerName
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
