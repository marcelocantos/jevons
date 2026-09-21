// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Achieve-time flags (🎯T765). A ledger attestation is the artifact that
// actually retires a target, and until this check it was the one place a
// fabricated green went unread: 🎯T757 was attested as "a clean-gate green"
// that never ran, on a commit the only real gate never saw.
const (
	// FlagAchieveGateUncited: the attestation says a gate passed but names no
	// gate id, so there is nothing for a reader to resolve.
	FlagAchieveGateUncited FlagKind = "achieve_gate_uncited"
	// FlagAchieveGatePrecedesFix: the cited gate ran on a commit that does not
	// contain the commit the attestation names as the fix — in 🎯T757's case,
	// the commit before it.
	FlagAchieveGatePrecedesFix FlagKind = "achieve_gate_precedes_fix"
	// FlagAchieveTreeUnknown: the cited record carries no tree provenance, so
	// which commit it measured cannot be read back.
	FlagAchieveTreeUnknown FlagKind = "achieve_tree_unknown"
)

// AchieveVerdict is what CheckAchieve concludes about one achieve.
type AchieveVerdict string

const (
	// AchieveVerified: every cited gate resolves to a GREEN record that ran
	// clean on code containing every commit the attestation names.
	AchieveVerified AchieveVerdict = "verified"
	// AchieveUngated: the attestation claims no gate at all. Outside this
	// check — T765 is about gates that are cited, or claimed, and fail.
	AchieveUngated AchieveVerdict = "ungated"
	// AchieveMarked: the gate evidence does not verify, but the attestation
	// states an accepted risk about the gate itself. Recorded, visibly —
	// never silently accepted. The escape for repos where -clean is
	// structurally impossible (yourworld2 until ge ships SDL3 libs).
	AchieveMarked AchieveVerdict = "marked"
	// AchieveRefused: the gate evidence does not support the achieve.
	AchieveRefused AchieveVerdict = "refused"
)

// AchieveMarker is the visible prefix a marked achieve is rendered with.
const AchieveMarker = "UNVERIFIED — ACCEPTED RISK"

// AchieveCheckArgs is the input to CheckAchieve.
type AchieveCheckArgs struct {
	// Attestation is the ledger row's attestation text.
	Attestation string
	// Lookup resolves a gate id to its record. Required: a ledger check with
	// no store has nothing to check against.
	Lookup func(id string) (*Record, bool)
	// Contains reports whether commit `code` contains commit `fix` — fix is
	// an ancestor of, or equal to, code. Both may be abbreviated. Nil falls
	// back to prefix equality, which is stricter, never looser.
	Contains func(code, fix string) bool
	// IsCommit reports whether a hex token the attestation names is a commit
	// at all. Attestations also carry report ids, output digests, gate ids
	// quoted bare and shas from other repos; demanding the gate contain
	// those buries the real flags under noise a reader learns to skip
	// (🎯T760's lesson). Nil treats every token as a commit.
	IsCommit func(sha string) bool
}

// AchieveResult is CheckAchieve's answer for one attestation.
type AchieveResult struct {
	Verdict AchieveVerdict
	Flags   []Flag
	// Risk is the accepted-risk sentence a marked achieve rests on.
	Risk string
}

// String renders the result for a terminal or a ledger banner.
func (r AchieveResult) String() string {
	var b strings.Builder
	if r.Verdict == AchieveMarked {
		fmt.Fprintf(&b, "%s: %s", AchieveMarker, r.Risk)
	} else {
		b.WriteString(string(r.Verdict))
	}
	for _, f := range r.Flags {
		b.WriteString("\n  • ")
		b.WriteString(f.String())
	}
	return b.String()
}

// ledgerGateRe parses a gate citation as ledger attestations actually write
// it. The finish-report form (attestationRe) requires exit= before the
// verdict; ledger rows routinely drop it — 🎯T752's verifiable attestation
// reads "GATE t752-head GREEN id=aefad0ae" — and demanding the long form here
// would call the one good specimen prose.
var ledgerGateRe = regexp.MustCompile(
	`GATE\s+(\S+)\s+(?:exit=(\S+)\s+)?(?:(GREEN|DIRTY|EMPTY|RED|SUSPECT|UNKNOWN|VOID|KILLED)\s+)?id=([0-9a-zA-Z]+)`)

// gateMentionRe matches an attestation that talks about a gate run. With no
// citation behind it, this is 🎯T757's "clean-gate green" in prose.
var gateMentionRe = regexp.MustCompile(`(?i)\b(bin/gate|gate|gates|gated)\b`)

// gateRiskRe matches an accepted-risk statement that is about the gate
// evidence. It must name the gate, its cleanliness or the dirt: an accepted
// risk about something else — 🎯T752's "Accepted-risk T552: no Grok seat for
// live observe" — must not launder a DIRTY gate into a marker.
var gateRiskRe = regexp.MustCompile(
	`(?i)\baccepted[- ]risk\b[^\n.;]{0,120}?\b(dirty|-clean|clean|gate|gates)\b[^\n.;]*`)

// keyValueRe strips key=value tokens (id=, out=, tree=clean@…) before
// looking for the commits an attestation names, so a gate's own handles are
// not mistaken for the fix.
var keyValueRe = regexp.MustCompile(`\b\w+=\S+`)

// namedCommitRe matches an abbreviated or full commit sha in prose.
var namedCommitRe = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)

// NamedCommits returns the commits an attestation names as evidence, with
// gate handles removed. A token must mix letters and digits: a bare date or
// count is all digits, and a pure-letter run is a word.
func NamedCommits(attestation string) []string {
	text := ledgerGateRe.ReplaceAllString(attestation, " ")
	text = keyValueRe.ReplaceAllString(text, " ")
	seen := map[string]bool{}
	var out []string
	for _, sha := range namedCommitRe.FindAllString(strings.ToLower(text), -1) {
		if seen[sha] || !strings.ContainsAny(sha, "0123456789") || !strings.ContainsAny(sha, "abcdef") {
			continue
		}
		seen[sha] = true
		out = append(out, sha)
	}
	return out
}

// CheckAchieve reads a ledger attestation the way FlagFalseGreen reads a
// finish report: resolve each cited gate id, read the record's verdict, and
// compare the commit it measured with the commits the attestation names.
//
// Every cited gate must hold up. A ledger row is a claim, not a narrative:
// a red-before control belongs in the finish report, and the ledger records
// what closed the target.
func CheckAchieve(args *AchieveCheckArgs) AchieveResult {
	text := strings.TrimSpace(args.Attestation)
	cited := ledgerGateRe.FindAllStringSubmatch(text, -1)

	if len(cited) == 0 {
		if !gateMentionRe.MatchString(text) {
			return AchieveResult{Verdict: AchieveUngated}
		}
		return withRisk(text, []Flag{{
			Kind: FlagAchieveGateUncited,
			Detail: "the attestation claims a gate run but cites no gate id, so nothing " +
				"can be resolved — cite the GATE line bin/gate printed",
			Evidence: trimLine(text),
		}})
	}

	var fixes []string
	for _, sha := range NamedCommits(text) {
		if args.IsCommit == nil || args.IsCommit(sha) {
			fixes = append(fixes, sha)
		}
	}
	contains := args.Contains
	if contains == nil {
		contains = func(code, fix string) bool { return sameCommit(code, fix) }
	}
	var flags []Flag
	for _, m := range cited {
		name, citedVerdict, id, raw := m[1], m[3], m[4], m[0]
		rec, ok := args.Lookup(id)
		if !ok {
			flags = append(flags, Flag{
				Kind: FlagAttestationUnknown,
				Detail: fmt.Sprintf(
					"no gate record %s exists, so the cited run for %q was not produced here", id, name),
				Evidence: raw,
			})
			continue
		}
		// A misquote is checked before the verdict: a DIRTY record cited as
		// GREEN must carry the contradiction, which withRisk never marks, so
		// an accepted-risk sentence cannot launder it into a marker.
		if citedVerdict != "" && Verdict(citedVerdict) != rec.Verdict {
			flags = append(flags, Flag{
				Kind: FlagAttestationContradicted,
				Detail: fmt.Sprintf("the attestation says %s, gate record %s says %s",
					citedVerdict, id, rec.Verdict),
				Evidence: raw,
			})
		}
		if !rec.Verdict.IsGreen() || rec.Status() != "0" {
			// DIRTY lands here: T386 / T396 make GREEN the only citable
			// verdict, and a dirty pass did not measure the commit.
			flags = append(flags, Flag{
				Kind: FlagAttestationNotGreen,
				Detail: fmt.Sprintf(
					"gate record %s is exit=%s %s — GREEN is the only verdict that can close a target",
					id, rec.Status(), rec.Verdict),
				Evidence: raw,
			})
			continue
		}
		t := rec.Tree
		if t == nil || t.Commit == "" {
			flags = append(flags, Flag{
				Kind: FlagAchieveTreeUnknown,
				Detail: fmt.Sprintf(
					"gate record %s carries no tree provenance, so the commit it measured is unknown", id),
				Evidence: raw,
			})
			continue
		}
		if !t.Clean {
			flags = append(flags, Flag{
				Kind: FlagDirtyTreeGate,
				Detail: fmt.Sprintf(
					"gate record %s ran in a working tree with %d uncommitted change(s) on %s; "+
						"re-run it as `bin/gate -clean -- <command>` (🎯T397)",
					id, t.DirtyFiles, t.ShortCommit()),
				Evidence: raw,
			})
			continue
		}
		for _, fix := range fixes {
			if contains(t.Commit, fix) {
				continue
			}
			flags = append(flags, Flag{
				Kind: FlagAchieveGatePrecedesFix,
				Detail: fmt.Sprintf(
					"gate record %s ran on %s, which does not contain %s — the gate did not measure "+
						"the code being attested", id, t.ShortCommit(), fix),
				Evidence: raw,
			})
		}
	}
	if len(flags) == 0 {
		return AchieveResult{Verdict: AchieveVerified}
	}
	return withRisk(text, flags)
}

// withRisk turns a failing check into a visible marker when the attestation
// states an accepted risk about its gate evidence, and a refusal otherwise.
// A gate id with no record, or a record the attestation misquotes, is never
// marked: that is not a risk someone accepted, it is a citation of a run
// that did not happen as described.
func withRisk(text string, flags []Flag) AchieveResult {
	for _, f := range flags {
		if f.Kind == FlagAttestationUnknown || f.Kind == FlagAttestationContradicted {
			return AchieveResult{Verdict: AchieveRefused, Flags: flags}
		}
	}
	if risk := gateRiskRe.FindString(text); risk != "" {
		return AchieveResult{Verdict: AchieveMarked, Flags: flags, Risk: strings.TrimSpace(risk)}
	}
	return AchieveResult{Verdict: AchieveRefused, Flags: flags}
}

// sameCommit compares two possibly-abbreviated shas.
func sameCommit(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if a == "" || b == "" {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// LedgerAchieve is one achieve a ledger records: the live `attestation` of an
// achieved target, or an "Achieved <date>: …" paragraph kept in `context`
// after the target was reopened. The second source is where 🎯T757 and
// 🎯T760's reverted achieves live, and a reverted false green is still the
// specimen a walk has to be able to name.
type LedgerAchieve struct {
	ID          string
	Date        string
	Source      string // "attestation" or "context"
	Status      string // the target's current status
	Attestation string
}

// Live reports whether this achieve is what currently retires its target: the
// attestation of a target whose status is achieved. A paragraph kept in
// context, or an attestation left on a target since reopened, is history —
// still worth naming, but not something today's ledger rests on.
func (a LedgerAchieve) Live() bool {
	return a.Source == "attestation" && a.Status == "achieved"
}

// contextAchieveRe matches an achieve paragraph bullseye appends to context.
var contextAchieveRe = regexp.MustCompile(`(?ms)^Achieved (\d{4}-\d{2}-\d{2}): (.*?)(?:\n\s*\n|\z)`)

// LedgerAchieves reads every achieve a bullseye ledger records, sorted by
// target id. A malformed ledger is an error, never an empty walk.
func LedgerAchieves(data []byte) ([]LedgerAchieve, error) {
	var ledger struct {
		Targets map[string]struct {
			Status      string `yaml:"status"`
			Achieved    string `yaml:"achieved"`
			Attestation string `yaml:"attestation"`
			Context     string `yaml:"context"`
		} `yaml:"targets"`
	}
	if err := yaml.Unmarshal(data, &ledger); err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	if ledger.Targets == nil {
		return nil, fmt.Errorf("ledger: no targets map")
	}
	var out []LedgerAchieve
	for id, t := range ledger.Targets {
		live := strings.TrimSpace(t.Attestation)
		if live != "" {
			out = append(out, LedgerAchieve{ID: id, Date: t.Achieved, Source: "attestation", Status: t.Status, Attestation: live})
		}
		for _, m := range contextAchieveRe.FindAllStringSubmatch(t.Context, -1) {
			text := strings.TrimSpace(m[2])
			if text == live {
				continue // bullseye mirrors the live attestation into context
			}
			out = append(out, LedgerAchieve{ID: id, Date: m[1], Source: "context", Status: t.Status, Attestation: text})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Date < out[j].Date
	})
	return out, nil
}

// LedgerCheck is one achieve and what CheckAchieve concluded about it.
type LedgerCheck struct {
	Achieve LedgerAchieve
	Result  AchieveResult
}

// LedgerCheckArgs is the input to CheckLedger.
type LedgerCheckArgs struct {
	Achieves []LedgerAchieve
	// ID and Since narrow the walk: one target, or achieves dated on or after
	// Since (YYYY-MM-DD). Empty means no narrowing.
	ID    string
	Since string
	// Lookup, Contains and IsCommit are passed to CheckAchieve.
	Lookup   func(id string) (*Record, bool)
	Contains func(code, fix string) bool
	IsCommit func(sha string) bool
}

// CheckLedger runs CheckAchieve over a ledger's achieves and splits the
// results into live and historical (see LedgerAchieve.Live). Only a refused
// live achieve is a standing false closure; a refused historical one was a
// false closure that has since been reversed, and counting both alike would
// fail every audit forever on a specimen someone already corrected.
func CheckLedger(args *LedgerCheckArgs) (live, historical []LedgerCheck) {
	for _, a := range args.Achieves {
		if args.ID != "" && a.ID != args.ID {
			continue
		}
		if args.Since != "" && a.Date < args.Since {
			continue
		}
		c := LedgerCheck{Achieve: a, Result: CheckAchieve(&AchieveCheckArgs{
			Attestation: a.Attestation,
			Lookup:      args.Lookup,
			Contains:    args.Contains,
			IsCommit:    args.IsCommit,
		})}
		if a.Live() {
			live = append(live, c)
		} else {
			historical = append(historical, c)
		}
	}
	return live, historical
}

// CountVerdict counts the checks with verdict v.
func CountVerdict(checks []LedgerCheck, v AchieveVerdict) int {
	n := 0
	for _, c := range checks {
		if c.Result.Verdict == v {
			n++
		}
	}
	return n
}
