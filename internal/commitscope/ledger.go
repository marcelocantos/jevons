// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// SharedLedger is the basename of the intent ledger. Path-scope
// (`git commit --only` this file) is not content-scope: other workers
// write target rows into the same file (🎯T748).
const SharedLedger = "bullseye.yaml"

// ClaimEnv names the target IDs the actor claims as theirs. Empty means
// none are claimed, so every semantically-changed target is named. A
// worker that sets this to its engaged id is told only about the others.
const ClaimEnv = "JEVONS_TARGET_ID"

// FileContent is HEAD vs staged bytes for one staged path. The binary
// fills this from git; tests pass fixtures. The decision stays pure.
type FileContent struct {
	Path   string
	Head   []byte // nil / empty if the path is new
	Staged []byte
}

// IsSharedLedger reports whether rel is the intent ledger.
func IsSharedLedger(rel string) bool {
	return strings.EqualFold(path.Base(filepath.ToSlash(rel)), SharedLedger)
}

type ledgerDoc struct {
	Targets map[string]any `yaml:"targets"`
}

// ChangedTargets returns target IDs whose parsed records differ between
// head and staged. Comparison is on canonical JSON of the unmarshalled
// YAML, so a scalar-style rewrite (plain / quoted / `|-`) of the same
// text is not a change — the T742 false alarm that fired when bullseye
// re-serialized a single-quoted context as a block scalar.
func ChangedTargets(head, staged []byte) ([]string, error) {
	h, err := parseTargets(head)
	if err != nil {
		return nil, err
	}
	s, err := parseTargets(staged)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(h)+len(s))
	for id := range h {
		ids[id] = struct{}{}
	}
	for id := range s {
		ids[id] = struct{}{}
	}
	var out []string
	for id := range ids {
		if !bytes.Equal(h[id], s[id]) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

func parseTargets(raw []byte) (map[string][]byte, error) {
	out := make(map[string][]byte)
	if len(bytes.TrimSpace(raw)) == 0 {
		return out, nil
	}
	var doc ledgerDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	for id, rec := range doc.Targets {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		b, err := json.Marshal(rec)
		if err != nil {
			return nil, err
		}
		out[id] = b
	}
	return out, nil
}

// ForeignTargets are the changed IDs the actor did not claim. An empty
// claimed set means every changed ID is foreign: the actor has not
// distinguished "this path is mine" from "this path's current diff is
// mine", which is the gap 🎯T748 closes.
func ForeignTargets(changed, claimed []string) []string {
	if len(changed) == 0 {
		return nil
	}
	if len(claimed) == 0 {
		return append([]string(nil), changed...)
	}
	have := make(map[string]bool, len(claimed))
	for _, c := range claimed {
		if n := normalizeID(c); n != "" {
			have[n] = true
		}
	}
	var out []string
	for _, id := range changed {
		if !have[normalizeID(id)] {
			out = append(out, id)
		}
	}
	return out
}

func normalizeID(raw string) string {
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

// ParseClaimed splits ClaimEnv (comma / whitespace) into target IDs.
func ParseClaimed(v string) []string {
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ';' || unicode.IsSpace(r)
	})
	var out []string
	seen := make(map[string]bool)
	for _, f := range fields {
		n := normalizeID(f)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func ledgerContentWarning(req *Request) string {
	var foreign []string
	ledgerPath := SharedLedger
	for _, c := range req.Contents {
		if !IsSharedLedger(c.Path) {
			continue
		}
		ledgerPath = c.Path
		changed, err := ChangedTargets(c.Head, c.Staged)
		if err != nil {
			// Unparseable staged YAML is not a silent pass, but it is
			// not a refusal either — the PO must still be able to land
			// a ledger commit. Name the failure so it is not a wolf.
			return fmt.Sprintf("commitscope: cannot parse %s for content-scope (🎯T748): %v\n", c.Path, err)
		}
		foreign = append(foreign, ForeignTargets(changed, req.Claimed)...)
	}
	if len(foreign) == 0 {
		return ""
	}
	return ledgerWarning(ledgerPath, foreign, req.Claimed)
}

func ledgerWarning(rel string, foreign, claimed []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "commitscope: %s's current DIFF is not only yours (🎯T748).\n\n", rel)
	b.WriteString("`--only` scopes the PATH, not the path's current hunks. This commit would\n")
	b.WriteString("include target rows this actor did not write:\n")
	for i, id := range foreign {
		if i == MaxNamed {
			fmt.Fprintf(&b, "  … and %d more\n", len(foreign)-MaxNamed)
			break
		}
		fmt.Fprintf(&b, "  %s\n", id)
	}
	if len(claimed) > 0 {
		b.WriteString("\nClaimed as yours: ")
		b.WriteString(strings.Join(claimed, ", "))
		b.WriteByte('\n')
	} else {
		b.WriteString("\nNo target IDs claimed (")
		b.WriteString(ClaimEnv)
		b.WriteString(" unset). Every semantically-changed row is named.\n")
	}
	b.WriteString("\nInspect before landing:\n")
	fmt.Fprintf(&b, "  git diff HEAD -- %s\n\n", rel)
	b.WriteString("The commit is allowed: refusing would stop the PO from closing targets.\n")
	b.WriteString("Decide per named row before landing, and say which in the message:\n")
	b.WriteString("  - its author is still live (check jevons_agent_list) — leave the row to\n")
	b.WriteString("    them; land your own paths now and the ledger row once they commit.\n")
	b.WriteString("  - its author is reaped and nobody is left to commit it — adopt the row:\n")
	b.WriteString("    land it and name it in the message as adopted, not authored, e.g.\n")
	b.WriteString("    \"ledger: achieve T888; adopt orphaned rows from reaped seats (T999)\".\n")
	b.WriteString("Refusing alone would leave an orphaned row dirty forever; a message that\n")
	b.WriteString("claims you wrote it is the other failure.\n")
	return b.String()
}
