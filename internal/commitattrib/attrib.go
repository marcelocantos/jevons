// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package commitattrib records which fleet seat landed a commit so guards
// that share one git identity can reason from provenance instead of timing
// (🎯T760).
package commitattrib

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

// ActorEnv names the committing fleet seat. Empty means the commit carries
// no actor declaration — guards must say "unattributed", not guess from timing.
const ActorEnv = "JEVONS_AGENT"

// TargetEnv names the target IDs the committer claims (comma / whitespace
// separated). Same variable commitscope reads for ledger row scope (🎯T748).
const TargetEnv = "JEVONS_TARGET_ID"

const (
	trailerActor  = "Jevons-Actor"
	trailerTarget = "Jevons-Target"
)

// Record is the attribution read from a commit message or the environment
// about to land one.
type Record struct {
	Actor   string
	Targets []string
}

// FromEnv reads the declaration a seat is about to commit under.
func FromEnv() Record {
	return Record{
		Actor:   strings.TrimSpace(os.Getenv(ActorEnv)),
		Targets: ParseTargets(os.Getenv(TargetEnv)),
	}
}

// ParseMessage extracts Jevons-* trailers from a commit message body.
func ParseMessage(body string) Record {
	var rec Record
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if key, val, ok := strings.Cut(line, ":"); ok {
			switch strings.TrimSpace(key) {
			case trailerActor:
				rec.Actor = strings.TrimSpace(val)
			case trailerTarget:
				rec.Targets = append(rec.Targets, ParseTargets(val)...)
			}
		}
	}
	rec.Targets = dedupeTargets(rec.Targets)
	return rec
}

// ParseTargets splits a claim string into normalized target IDs.
func ParseTargets(v string) []string {
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ';' || unicode.IsSpace(r)
	})
	var out []string
	seen := map[string]bool{}
	for _, f := range fields {
		n := normalizeTargetID(f)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func normalizeTargetID(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "🎯")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	if unicode.ToLower(r[0]) == 't' {
		r[0] = 'T'
	}
	return string(r)
}

func dedupeTargets(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, id := range in {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// Trailers returns the trailer lines to append for rec. Empty when there is
// nothing to declare.
func (rec Record) Trailers() []string {
	var out []string
	if rec.Actor != "" {
		out = append(out, trailerActor+": "+rec.Actor)
	}
	if len(rec.Targets) > 0 {
		out = append(out, trailerTarget+": "+strings.Join(rec.Targets, ", "))
	}
	return out
}

// StampFile appends Jevons-* trailers from the environment when set and not
// already present in the commit message file.
func StampFile(msgPath string) error {
	rec := FromEnv()
	lines := rec.Trailers()
	if len(lines) == 0 {
		return nil
	}
	data, err := os.ReadFile(msgPath)
	if err != nil {
		return err
	}
	body := string(data)
	existing := ParseMessage(body)
	if rec.Actor != "" && existing.Actor != "" {
		rec.Actor = ""
	}
	if len(rec.Targets) > 0 && len(existing.Targets) > 0 {
		rec.Targets = nil
	}
	lines = rec.Trailers()
	if len(lines) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(body, "\n"))
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	b.WriteByte('\n')
	return os.WriteFile(msgPath, []byte(b.String()), 0o644)
}

// SharedLedger is the intent ledger basename.
const SharedLedger = "bullseye.yaml"

// IsSharedLedger reports whether rel is the intent ledger.
func IsSharedLedger(rel string) bool {
	return strings.EqualFold(path.Base(filepath.ToSlash(rel)), SharedLedger)
}

// HasProductChanges is true when any changed path is not ledger-only.
func HasProductChanges(files []string) bool {
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if f != "" && !IsSharedLedger(f) {
			return true
		}
	}
	return false
}

// ReadMessageFile is a test helper around StampFile's read path.
func ReadMessageFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WriteMessageFile writes a commit message file for tests.
func WriteMessageFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}

// ScanTrailers is like ParseMessage but stops at the first non-trailer line
// after trailers begin — used when validating hook output shape.
func ScanTrailers(body string) Record {
	rec := ParseMessage(body)
	if rec.Actor != "" || len(rec.Targets) > 0 {
		return rec
	}
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if key, val, ok := strings.Cut(line, ":"); ok {
			switch strings.TrimSpace(key) {
			case trailerActor:
				rec.Actor = strings.TrimSpace(val)
			case trailerTarget:
				rec.Targets = append(rec.Targets, ParseTargets(val)...)
			}
		}
	}
	rec.Targets = dedupeTargets(rec.Targets)
	return rec
}
