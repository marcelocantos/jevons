// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// ExistenceVerdict is the 🎯T679.1 seam answer. Absent is returned only when
// the provider path was actually resolved.
type ExistenceVerdict string

const (
	ExistenceUnobservable ExistenceVerdict = "unobservable"
	ExistenceAbsent       ExistenceVerdict = "absent"
	ExistencePresent      ExistenceVerdict = "present"
)

// TranscriptExistence is one provider/session existence reading.
type TranscriptExistence struct {
	Verdict ExistenceVerdict
	Reason  string
	Path    string
}

// TranscriptExistenceQuery names the seat whose current session is looked up.
// Name is the registry key; lookup is by provider + session id (+ workdir
// for Claude). Roots must be the caller's configured Grok/Claude stores —
// zero Roots makes Grok unobservable rather than probing the live home.
type TranscriptExistenceQuery struct {
	Name      string
	Provider  claudia.Provider
	SessionID string
	WorkDir   string
	Roots     discovery.Roots
}

// LookupTranscriptExistence answers whether the current session ever produced
// a transcript: present, absent, or unobservable (🎯T679.1).
func LookupTranscriptExistence(q TranscriptExistenceQuery) TranscriptExistence {
	sid := strings.TrimSpace(q.SessionID)
	if sid == "" {
		return TranscriptExistence{Verdict: ExistenceUnobservable, Reason: "no session id"}
	}
	switch q.Provider {
	case "", claudia.ProviderClaude:
		return lookupClaudeTranscript(sid, q.WorkDir)
	case claudia.ProviderGrok:
		return lookupGrokTranscript(q.Roots, sid)
	case claudia.ProviderCursor:
		return cursorTranscriptUnobservable(q)
	default:
		return TranscriptExistence{
			Verdict: ExistenceUnobservable,
			Reason: fmt.Sprintf("%s has no durable first-submit transcript this daemon can locate",
				providerLabel(q.Provider)),
		}
	}
}

func lookupClaudeTranscript(sessionID, workDir string) TranscriptExistence {
	if strings.TrimSpace(workDir) == "" {
		return TranscriptExistence{Verdict: ExistenceUnobservable, Reason: "no workdir"}
	}
	ok, err := claudia.SessionExists(sessionID, workDir)
	if err != nil {
		return TranscriptExistence{
			Verdict: ExistenceUnobservable,
			Reason:  fmt.Sprintf("claude session lookup: %v", err),
		}
	}
	if ok {
		return TranscriptExistence{
			Verdict: ExistencePresent,
			Path:    claudia.SessionJSONLPath(sessionID, workDir),
		}
	}
	return TranscriptExistence{Verdict: ExistenceAbsent, Reason: "claude JSONL is absent"}
}

func lookupGrokTranscript(roots discovery.Roots, sessionID string) TranscriptExistence {
	got := discovery.GrokUpdatesLookup(roots, sessionID)
	out := TranscriptExistence{
		Verdict: ExistenceVerdict(got.State),
		Reason:  got.Reason,
		Path:    got.Path,
	}
	if got.Err != nil && out.Reason != "" {
		out.Reason = fmt.Sprintf("%s: %v", got.Reason, got.Err)
	}
	if out.Verdict == "" {
		out.Verdict = ExistenceUnobservable
		out.Reason = "grok lookup returned no state"
	}
	return out
}

func cursorTranscriptUnobservable(q TranscriptExistenceQuery) TranscriptExistence {
	// Pinned claudia v0.40.0 startCursorAgent leaves JSONLPath empty and sets
	// TailJSONL=false, so agent.go keeps the initially computed Claude-shaped
	// path — a phantom T501/T519 already refuse. store.db is opened at
	// session/new|session/load (Launch), not at first Prompt, so its
	// existence is a resume check and cannot prove first-submit creation.
	name := strings.TrimSpace(q.Name)
	if name == "" {
		name = "cursor seat"
	}
	return TranscriptExistence{
		Verdict: ExistenceUnobservable,
		Reason: name + ": cursor ACP has no first-submit JSONL contract; " +
			"store.db is a resume check (session/new|load) and the Claude-shaped path is a phantom (T501/T519)",
	}
}

func providerLabel(p claudia.Provider) string {
	if p == "" {
		return "empty provider"
	}
	return string(p)
}
