// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/seatactivity"
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
//
// The provider map itself lives in internal/seatactivity, because 🎯T702 reads
// the same resolved path one field further on for recency; two copies of the
// map would drift the first time a provider moved its store.
func LookupTranscriptExistence(q TranscriptExistenceQuery) TranscriptExistence {
	loc := seatactivity.Locate(seatactivity.Query{
		Name:      q.Name,
		Provider:  q.Provider,
		SessionID: q.SessionID,
		WorkDir:   q.WorkDir,
		Roots:     q.Roots,
	})
	return TranscriptExistence{
		Verdict: ExistenceVerdict(loc.State),
		Reason:  loc.Reason,
		Path:    loc.Path,
	}
}
