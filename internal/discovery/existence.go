// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LookupState is an honest file-existence answer. Absent is only returned
// when the store root was resolved and searched; lookup failure and
// permission errors are Unobservable (🎯T679.1).
type LookupState string

const (
	LookupUnobservable LookupState = "unobservable"
	LookupAbsent       LookupState = "absent"
	LookupPresent      LookupState = "present"
)

// FileLookup is one resolved existence check. Path is set on Present.
type FileLookup struct {
	State  LookupState
	Path   string
	Reason string
	Err    error
}

const grokUpdatesName = "updates.jsonl"

// GrokUpdatesLookup reports whether sessionID has an updates.jsonl under
// any configured Grok root (ordinary and exclusive-MCP). chat_history.jsonl
// is never the conversation source (🎯T621). TranscriptPath collapsing
// lookup failure and absence into "" is not this answer.
func GrokUpdatesLookup(roots Roots, sessionID string) FileLookup {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return FileLookup{State: LookupUnobservable, Reason: "no session id"}
	}
	if !IsSessionID(sid) {
		return FileLookup{State: LookupUnobservable, Reason: "session id is not a Grok session id"}
	}
	var rootsToSearch []string
	if strings.TrimSpace(roots.GrokSessions) != "" {
		rootsToSearch = append(rootsToSearch, roots.GrokSessions)
	}
	for _, extra := range roots.GrokHomeSessions {
		if strings.TrimSpace(extra) != "" {
			rootsToSearch = append(rootsToSearch, extra)
		}
	}
	if extra := ClaudiaGrokHomeSessionsDir(roots.ClaudiaGrokHomes, sid); extra != "" {
		rootsToSearch = append(rootsToSearch, extra)
	}
	if len(rootsToSearch) == 0 {
		return FileLookup{State: LookupUnobservable, Reason: "no grok session roots configured"}
	}
	var firstObs FileLookup
	allAbsent := true
	for _, root := range rootsToSearch {
		got := lookupGrokRoot(root, sid)
		switch got.State {
		case LookupPresent:
			return got
		case LookupUnobservable:
			allAbsent = false
			if firstObs.Reason == "" {
				firstObs = got
			}
		}
	}
	if !allAbsent {
		return firstObs
	}
	return FileLookup{State: LookupAbsent, Reason: "no updates.jsonl under any grok root"}
}

func lookupGrokRoot(sessionsDir, sessionID string) FileLookup {
	buckets, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return FileLookup{State: LookupAbsent, Reason: "grok sessions root does not exist"}
		}
		return FileLookup{
			State:  LookupUnobservable,
			Reason: fmt.Sprintf("cannot list grok sessions root %s", sessionsDir),
			Err:    err,
		}
	}
	var firstWalkErr error
	var firstWalkPath string
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		p := filepath.Join(sessionsDir, bucket.Name(), sessionID, grokUpdatesName)
		fi, err := os.Stat(p)
		if err == nil && !fi.IsDir() {
			return FileLookup{State: LookupPresent, Path: p}
		}
		if err == nil && fi.IsDir() {
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			if firstWalkErr == nil {
				firstWalkErr = err
				firstWalkPath = p
			}
		}
	}
	if firstWalkErr != nil {
		return FileLookup{
			State:  LookupUnobservable,
			Reason: fmt.Sprintf("cannot stat grok updates.jsonl %s", firstWalkPath),
			Err:    firstWalkErr,
		}
	}
	return FileLookup{State: LookupAbsent, Reason: "no updates.jsonl in grok root"}
}
