// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package writconf compiles declared-intent manifests and runs high-risk
// fleet commands under writ (seatbelt + egress). writ is the preferred
// enforcer over the interim doit spawn gate (🎯T8.3 residual / 🎯T335).
//
// This package does not re-implement OS sandboxes: Exec shells out to the
// writ CLI (or an injected Executor). Pure helpers mirror allow/deny rules
// for hermetic tests without FUSE or a live seatbelt child.
package writconf

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// SchemaVersion is the only writ intent-manifest version we emit.
const SchemaVersion = "1"

// Manifest is a writ schema v1 intent document (JSON-compatible).
type Manifest struct {
	SchemaVersion string       `json:"schema_version"`
	FS            *FSScopes    `json:"fs,omitempty"`
	Net           []NetIntent  `json:"net,omitempty"`
	Exec          []ExecIntent `json:"exec,omitempty"`
	SSH           []SSHIntent  `json:"ssh,omitempty"`
	Env           *EnvIntent   `json:"env,omitempty"`
}

// FSScopes scopes filesystem access by mode (always-glob targets).
type FSScopes struct {
	Read []FSTarget `json:"read,omitempty"`
	Edit []FSTarget `json:"edit,omitempty"`
}

// FSTarget is one filesystem glob, optionally with an edit mutation budget.
type FSTarget struct {
	Glob     string `json:"glob"`
	MaxFiles *int   `json:"max_files,omitempty"`
}

// MarshalJSON emits bare strings when only Glob is set (writ schema v1).
func (t FSTarget) MarshalJSON() ([]byte, error) {
	if t.MaxFiles == nil {
		return json.Marshal(t.Glob)
	}
	type alias FSTarget
	return json.Marshal(alias(t))
}

// UnmarshalJSON accepts string or object form.
func (t *FSTarget) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		t.Glob = s
		t.MaxFiles = nil
		return nil
	}
	type alias FSTarget
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*t = FSTarget(a)
	return nil
}

// NetIntent scopes network access by host or fetch URL.
type NetIntent struct {
	Fetch string `json:"fetch,omitempty"`
	Host  string `json:"host,omitempty"`
}

// ExecIntent names an allowed binary.
type ExecIntent struct {
	Bin       string `json:"bin"`
	ArgvTaint string `json:"argv_taint,omitempty"`
}

// SSHIntent pins remote SSH destination and command.
type SSHIntent struct {
	Host string `json:"host"`
	User string `json:"user"`
	Cmd  string `json:"cmd"`
}

// EnvIntent names which environment variables are visible.
type EnvIntent struct {
	Allow []string `json:"allow,omitempty"`
}

// FleetManifestArgs configures a default fleet-worker confinement world.
type FleetManifestArgs struct {
	// WorkDir is the worker tree (required for IncludeFS).
	WorkDir string
	// NetHosts are allowed hostnames (host-granularity). Empty uses DefaultNetHosts.
	NetHosts []string
	// IncludeFS adds workdir read/edit scopes. Requires FUSE under real writ;
	// leave false for net-only thin verticals that still enforce egress.
	IncludeFS bool
	// ExtraEnvAllow appends to DefaultEnvAllow.
	ExtraEnvAllow []string
}

// DefaultNetHosts is the baseline allowlist for fleet opaque children
// (provider APIs, git, Go module proxy). Undeclared hosts are denied.
var DefaultNetHosts = []string{
	"api.x.ai",
	"api.anthropic.com",
	"api.openai.com",
	"github.com",
	"api.github.com",
	"objects.githubusercontent.com",
	"proxy.golang.org",
	"sum.golang.org",
	"storage.googleapis.com",
	"golang.org",
}

// DefaultEnvAllow is the env filter for confined children.
var DefaultEnvAllow = []string{
	"PATH", "HOME", "TMPDIR", "USER", "LANG", "TERM",
	"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy",
	"NO_PROXY", "no_proxy", "ALL_PROXY", "all_proxy",
	"SSL_CERT_FILE", "CURL_CA_BUNDLE", "REQUESTS_CA_BUNDLE",
	"GOROOT", "GOPATH", "GOPROXY", "GOSUMDB", "GO111MODULE",
	"XAI_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY",
	"SSH_AUTH_SOCK",
}

// FleetManifest compiles a writ intent document for a fleet worker seat.
func FleetManifest(args FleetManifestArgs) (*Manifest, error) {
	hosts := args.NetHosts
	if len(hosts) == 0 {
		hosts = append([]string(nil), DefaultNetHosts...)
	}
	net := make([]NetIntent, 0, len(hosts))
	for _, h := range hosts {
		h = strings.TrimSpace(strings.ToLower(h))
		if h == "" {
			continue
		}
		net = append(net, NetIntent{Host: h})
	}
	envAllow := append([]string(nil), DefaultEnvAllow...)
	envAllow = append(envAllow, args.ExtraEnvAllow...)

	m := &Manifest{
		SchemaVersion: SchemaVersion,
		Net:           net,
		Env:           &EnvIntent{Allow: dedupeStrings(envAllow)},
	}
	if args.IncludeFS {
		wd := strings.TrimSpace(args.WorkDir)
		if wd == "" {
			return nil, fmt.Errorf("writconf: WorkDir required when IncludeFS")
		}
		abs, err := filepath.Abs(wd)
		if err != nil {
			return nil, fmt.Errorf("writconf: abs workdir: %w", err)
		}
		// Workdir tree only — undeclared paths (e.g. ~/.ssh) stay outside.
		glob := filepath.ToSlash(filepath.Join(abs, "**"))
		m.FS = &FSScopes{
			Read: []FSTarget{{Glob: abs}, {Glob: glob}},
			Edit: []FSTarget{{Glob: abs}, {Glob: glob}},
		}
	}
	if err := Validate(m); err != nil {
		return nil, err
	}
	return m, nil
}

// Validate performs schema-level checks (mirrors writ validate fail-closed).
func Validate(m *Manifest) error {
	if m == nil {
		return fmt.Errorf("writconf: nil manifest")
	}
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("writconf: schema_version %q unsupported (want %q)", m.SchemaVersion, SchemaVersion)
	}
	for i, n := range m.Net {
		if strings.TrimSpace(n.Host) == "" && strings.TrimSpace(n.Fetch) == "" {
			return fmt.Errorf("writconf: net[%d] needs host or fetch", i)
		}
	}
	return nil
}

// MarshalJSONDocument returns pretty JSON suitable for writ --manifest.
func MarshalJSONDocument(m *Manifest) ([]byte, error) {
	if err := Validate(m); err != nil {
		return nil, err
	}
	return json.MarshalIndent(m, "", "  ")
}

// Parse unmarshals a JSON manifest (best-effort; still Validate).
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("writconf: parse: %w", err)
	}
	if err := Validate(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
