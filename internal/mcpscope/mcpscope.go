// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcpscope makes fleet control follow the agent rather than the
// directory it happens to have been started in (🎯T464).
//
// Claude Code resolves MCP servers from three places. Two of them live in
// ~/.claude.json: the top-level `mcpServers` map, which every session
// inherits regardless of where it starts (user scope), and
// `projects["<dir>"].mcpServers`, which applies only to a session whose
// working directory is exactly that string (local scope — the map is keyed
// by the launch directory, and a nested directory gets its own key with no
// inheritance). The third is a `.mcp.json` committed in the repo, which is
// inert until separately approved; this package does not read it, and says
// so rather than pretending the two it reads are the whole picture.
//
// jevonsmcp was registered in local scope under the jevons repo path alone.
// A product owner restarted anywhere else therefore had no fleet tools —
// could not spawn, reap, or nudge — while the daemon it was talking about
// was healthy and answering on 127.0.0.1:13705 the entire time.
//
// # The expensive half is the diagnosis, not the plumbing
//
// On 2026-08-15 an agent in this position reported upward that the CONTROL
// PLANE WAS DEAD, and the fleet spent a cycle chasing an outage that was not
// happening. Missing tools look identical from the inside whether the daemon
// died or the registration simply does not reach this directory, and the
// difference is the whole of what the reader needs.
//
// So the rule this package enforces is narrow and load-bearing: the absence
// of tools NEVER licenses the word "down". Only a probe of the endpoint
// does. An agent that cannot see the daemon and has not dialled it knows one
// thing — that it cannot see the daemon — and Diagnose refuses to let that
// be written up as anything more.
package mcpscope

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ServerName is the registration every fleet agent needs to have fleet
// control. Named rather than repeated so the ensure path and the diagnosis
// path cannot drift apart.
const ServerName = "jevonsmcp"

// DefaultEndpoint is where jevonsd serves MCP for the daily universe. A
// journey isolate serves its own port and registers its own name; this
// constant is the daily one, and the ensure path takes the endpoint as an
// argument rather than assuming it.
const DefaultEndpoint = "http://127.0.0.1:13705/mcp"

// Entry is one MCP server registration as ~/.claude.json spells it. Only
// the http shape is modelled: that is what jevonsd serves, and inventing a
// stdio variant here would be a registration this product cannot honour.
type Entry struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// HTTPEntry is the registration for an http endpoint.
func HTTPEntry(url string) Entry { return Entry{Type: "http", URL: strings.TrimSpace(url)} }

// Scope says which of ~/.claude.json's two server maps a registration was
// found in, as it applies to one working directory.
type Scope string

const (
	// ScopeNone means no registration under that name reaches this
	// directory. It says nothing whatsoever about the daemon.
	ScopeNone Scope = "none"
	// ScopeUser means the top-level map carries it, so every session
	// inherits it wherever it starts. This is the state 🎯T464 wants.
	ScopeUser Scope = "user"
	// ScopeLocal means only projects["<dir>"] carries it: this directory
	// has fleet control and its siblings do not.
	ScopeLocal Scope = "local"
)

// Reach is what a probe of the endpoint actually established. Deliberately
// three-valued: a dial that timed out proves nothing, and collapsing it into
// "down" is how a slow start becomes a reported outage.
type Reach string

const (
	// ReachUp means a connection was established.
	ReachUp Reach = "up"
	// ReachDown means the endpoint provably has no listener — refused, or
	// the host does not resolve.
	ReachDown Reach = "down"
	// ReachUnknown means the probe was inconclusive or was never run.
	ReachUnknown Reach = "unknown"
)

// Verdict is the diagnosis. Its whole purpose is that the two situations
// which feel identical from inside a toolless agent get different names.
type Verdict string

const (
	// VerdictOK: registered for this directory and the endpoint answered.
	VerdictOK Verdict = "ok"
	// VerdictOutOfScope: the endpoint answered, and no registration
	// reaches this directory. The control plane is UP. This is the verdict
	// that was misreported as an outage.
	VerdictOutOfScope Verdict = "out_of_scope"
	// VerdictLocalOnly: registered, but only for this exact directory, so
	// the same agent restarted elsewhere loses fleet control. Healthy now,
	// one `cd` from the 🎯T464 failure.
	VerdictLocalOnly Verdict = "local_only"
	// VerdictDown: a registration reaches this directory and the endpoint
	// provably has no listener. The only verdict that may be reported as a
	// control-plane outage.
	VerdictDown Verdict = "down"
	// VerdictUnregisteredAndDown: neither half works.
	VerdictUnregisteredAndDown Verdict = "unregistered_and_down"
	// VerdictUnknown: the probe did not settle, so reachability is
	// undecided and no outage may be claimed either way.
	VerdictUnknown Verdict = "unknown"
)

// ClaimsControlPlaneDown reports whether this verdict entitles its holder to
// tell anyone the daemon is down.
//
// This is the pure oracle for the second half of 🎯T464, and it is
// deliberately a single expression rather than a judgement call spread over
// prose: a verdict reached without a probe that says ReachDown can never be
// escalated as an outage.
func (v Verdict) ClaimsControlPlaneDown() bool {
	return v == VerdictDown || v == VerdictUnregisteredAndDown
}

// Healthy reports whether fleet tools should be present in this session.
func (v Verdict) Healthy() bool { return v == VerdictOK || v == VerdictLocalOnly }

// Diagnose combines where the registration was found with what the probe
// established. It takes the probe result rather than performing it so the
// classification is testable without a network condition for every case —
// the same seam 🎯T379 uses.
//
// Note the asymmetry, which is the point: scope alone can produce
// VerdictOutOfScope only when reach is ReachUp. With ReachUnknown the answer
// is VerdictUnknown even though the scope half is fully decided, because a
// reader who is told "out of scope" will reasonably conclude the daemon is
// fine, and an unprobed endpoint has not earned that either.
func Diagnose(scope Scope, reach Reach) Verdict {
	registered := scope == ScopeUser || scope == ScopeLocal
	switch reach {
	case ReachUp:
		switch scope {
		case ScopeUser:
			return VerdictOK
		case ScopeLocal:
			return VerdictLocalOnly
		default:
			return VerdictOutOfScope
		}
	case ReachDown:
		if registered {
			return VerdictDown
		}
		return VerdictUnregisteredAndDown
	default:
		return VerdictUnknown
	}
}

// Explain renders the verdict as the sentence an agent should actually say,
// naming the working directory and the endpoint so the reader can act
// without a second round trip.
//
// Every branch that is not an outage states positively that the daemon is
// up, because the failure this package exists for was silence on that point
// being read as confirmation of the opposite.
func Explain(v Verdict, name, cwd, endpoint string) string {
	switch v {
	case VerdictOK:
		return fmt.Sprintf("%s is registered in user scope and %s answered: fleet control is available here and in any working directory.",
			name, endpoint)
	case VerdictLocalOnly:
		return fmt.Sprintf("%s is registered ONLY for the working directory %s, and %s answered. Fleet control works here, but this agent restarted anywhere else would lose every %s_* tool. The control plane is UP.",
			name, cwd, endpoint, strings.TrimSuffix(name, "mcp"))
	case VerdictOutOfScope:
		return fmt.Sprintf("%s is NOT REGISTERED FOR THIS WORKING DIRECTORY (%s), so this session has no fleet tools — but %s answered, so THE CONTROL PLANE IS UP. Do not report a daemon outage. Fix the registration (user scope), do not restart the daemon.",
			name, cwd, endpoint)
	case VerdictDown:
		return fmt.Sprintf("%s is registered for this working directory (%s) and %s has no listener: the control plane is DOWN.",
			name, cwd, endpoint)
	case VerdictUnregisteredAndDown:
		return fmt.Sprintf("%s is not registered for this working directory (%s) AND %s has no listener: the control plane is DOWN, and fixing it will not by itself give this session fleet tools.",
			name, cwd, endpoint)
	default:
		return fmt.Sprintf("%s reachability at %s is UNDETERMINED from working directory %s — the probe did not settle. This is not evidence of an outage; probe again before reporting one.",
			endpoint, endpoint, cwd)
	}
}

// claudeDoc is the whole of ~/.claude.json held as raw members, so that
// reading it and writing it back cannot lose a key this product has never
// heard of. The file carries 1800-odd project entries and a long tail of
// client state; a typed struct would silently drop all of it.
type claudeDoc map[string]json.RawMessage

// serverMap is one `mcpServers` object, again raw-valued: another tool's
// registration may carry fields this one does not model, and rewriting it
// through a narrow struct would quietly delete them.
type serverMap map[string]json.RawMessage

// FindScope reports where name is registered as it applies to cwd, reading
// the user map first because a user-scope entry is what makes the local one
// redundant.
//
// A malformed document is an error, never a silent ScopeNone: "no
// registration" and "I could not read the file" lead to opposite actions,
// and conflating them is the same mistake at one remove that this package
// exists to prevent.
func FindScope(cfg []byte, name, cwd string) (Scope, Entry, error) {
	var doc claudeDoc
	if err := json.Unmarshal(cfg, &doc); err != nil {
		return ScopeNone, Entry{}, fmt.Errorf("parse claude config: %w", err)
	}
	if raw, ok := doc["mcpServers"]; ok {
		var servers serverMap
		if err := json.Unmarshal(raw, &servers); err != nil {
			return ScopeNone, Entry{}, fmt.Errorf("parse user mcpServers: %w", err)
		}
		if e, ok := servers[name]; ok {
			return ScopeUser, decodeEntry(e), nil
		}
	}
	raw, ok := doc["projects"]
	if !ok {
		return ScopeNone, Entry{}, nil
	}
	var projects map[string]json.RawMessage
	if err := json.Unmarshal(raw, &projects); err != nil {
		return ScopeNone, Entry{}, fmt.Errorf("parse projects: %w", err)
	}
	// Exact match only. Claude keys this map by the directory a session was
	// launched in; a subdirectory is a separate key and inherits nothing,
	// so an ancestor match here would report fleet control that the session
	// will not actually have.
	projRaw, ok := projects[cwd]
	if !ok {
		return ScopeNone, Entry{}, nil
	}
	var proj map[string]json.RawMessage
	if err := json.Unmarshal(projRaw, &proj); err != nil {
		return ScopeNone, Entry{}, fmt.Errorf("parse project %q: %w", cwd, err)
	}
	serversRaw, ok := proj["mcpServers"]
	if !ok {
		return ScopeNone, Entry{}, nil
	}
	var servers serverMap
	if err := json.Unmarshal(serversRaw, &servers); err != nil {
		return ScopeNone, Entry{}, fmt.Errorf("parse project %q mcpServers: %w", cwd, err)
	}
	if e, ok := servers[name]; ok {
		return ScopeLocal, decodeEntry(e), nil
	}
	return ScopeNone, Entry{}, nil
}

func decodeEntry(raw json.RawMessage) Entry {
	var e Entry
	_ = json.Unmarshal(raw, &e)
	return e
}

// LocalScopeDirs lists every project directory that registers name in local
// scope, sorted. Used to explain a migration honestly: after the user-scope
// entry exists these are redundant, and naming them beats implying there
// were none.
func LocalScopeDirs(cfg []byte, name string) ([]string, error) {
	var doc claudeDoc
	if err := json.Unmarshal(cfg, &doc); err != nil {
		return nil, fmt.Errorf("parse claude config: %w", err)
	}
	raw, ok := doc["projects"]
	if !ok {
		return nil, nil
	}
	var projects map[string]json.RawMessage
	if err := json.Unmarshal(raw, &projects); err != nil {
		return nil, fmt.Errorf("parse projects: %w", err)
	}
	var out []string
	for dir, projRaw := range projects {
		var proj map[string]json.RawMessage
		if err := json.Unmarshal(projRaw, &proj); err != nil {
			continue
		}
		serversRaw, ok := proj["mcpServers"]
		if !ok {
			continue
		}
		var servers serverMap
		if err := json.Unmarshal(serversRaw, &servers); err != nil {
			continue
		}
		if _, ok := servers[name]; ok {
			out = append(out, dir)
		}
	}
	sort.Strings(out)
	return out, nil
}

// EnsureUserScope returns cfg with name registered in the top-level
// `mcpServers` map, and reports whether anything changed.
//
// Pure, and narrow on purpose. ~/.claude.json is shared hot state — every
// concurrent agent in the fleet reads and writes it — so this is the 🎯T376
// shape exactly, and the mitigation is that the whole edit is one key. The
// document is carried as raw members from parse to serialise, so the 1800
// project entries and every field this product has never modelled survive
// untouched; and because the operation adds a single key rather than
// replacing a document, re-applying it to freshly-read bytes after a lost
// race is always correct. WriteEnsure relies on both properties.
//
// changed is false when the entry is already present and identical, and the
// caller must then not write at all: in steady state that is every call, and
// a file this contended should be opened for writing only when there is
// something to say.
func EnsureUserScope(cfg []byte, name string, entry Entry) (out []byte, changed bool, err error) {
	var doc claudeDoc
	if err := json.Unmarshal(cfg, &doc); err != nil {
		return nil, false, fmt.Errorf("parse claude config: %w", err)
	}
	servers := serverMap{}
	if raw, ok := doc["mcpServers"]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, false, fmt.Errorf("parse user mcpServers: %w", err)
		}
	}
	want, err := json.Marshal(entry)
	if err != nil {
		return nil, false, fmt.Errorf("encode entry: %w", err)
	}
	if have, ok := servers[name]; ok && equivalentJSON(have, want) {
		return cfg, false, nil
	}
	servers[name] = want
	encoded, err := json.Marshal(servers)
	if err != nil {
		return nil, false, fmt.Errorf("encode mcpServers: %w", err)
	}
	doc["mcpServers"] = encoded
	// Indented to match what Claude Code itself writes, so a repair does not
	// show up as a whole-file diff in the owner's dotfile history.
	out, err = json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("encode claude config: %w", err)
	}
	return append(out, '\n'), true, nil
}

// equivalentJSON compares two encoded values by structure rather than bytes,
// so a registration that differs only in key order or whitespace is not
// rewritten — which would mean taking the lock on the hot file for nothing.
func equivalentJSON(a, b []byte) bool {
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &y); err != nil {
		return false
	}
	return fmt.Sprintf("%#v", x) == fmt.Sprintf("%#v", y)
}
