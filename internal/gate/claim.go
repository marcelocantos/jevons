// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"regexp"
	"strings"
)

// FlagKind names one way a finish report's green claim fails to hold up.
type FlagKind string

const (
	// FlagPipelineMasked: the report cites a status for a gate that was piped
	// through another command. A pipeline's status is the last stage's, and
	// tail always succeeds. This is 🎯T386's headline case.
	FlagPipelineMasked FlagKind = "pipeline_masked"
	// FlagShellArrayTrap: the report reads a pipeline status out of an array
	// that does not exist in the shell that ran it — bash's PIPESTATUS under
	// zsh, or index 0 of zsh's 1-indexed pipestatus. The interpolation is
	// empty and the status was never read at all.
	FlagShellArrayTrap FlagKind = "shell_array_trap"
	// FlagEmptyStatus: the report shows a status variable that expanded to
	// nothing ("EXIT="). An empty status is not zero.
	FlagEmptyStatus FlagKind = "empty_status"
	// FlagOutputContradicts: the report claims a pass while quoting output
	// that shows a panic, a timeout, a data race or a FAIL line.
	FlagOutputContradicts FlagKind = "output_contradicts"
	// FlagAttestationNotGreen: the report cites a gate attestation whose own
	// verdict is not GREEN, or whose status is not zero.
	FlagAttestationNotGreen FlagKind = "attestation_not_green"
	// FlagAttestationUnknown: the report cites a gate id with no record
	// behind it. The line was not produced by a run this machine performed.
	FlagAttestationUnknown FlagKind = "attestation_unknown"
	// FlagAttestationContradicted: the cited line disagrees with the stored
	// record — the report says GREEN, the record says otherwise.
	FlagAttestationContradicted FlagKind = "attestation_contradicted"
)

// Flag is one contradiction found in a finish report.
type Flag struct {
	Kind FlagKind
	// Detail is the sentence shown to the overseer.
	Detail string
	// Evidence is the fragment of the report that triggered it.
	Evidence string
}

func (f Flag) String() string {
	if f.Evidence == "" {
		return fmt.Sprintf("%s: %s", f.Kind, f.Detail)
	}
	return fmt.Sprintf("%s: %s — %q", f.Kind, f.Detail, f.Evidence)
}

// greenClaimMarkers are the ways a report says a gate passed. Deliberately
// narrow: "green" and "passed" as words, and explicit zero-status shapes.
var greenClaimMarkers = []string{
	"green",
	"passed",
	"passes",
	"all pass",
	"tests pass",
	"exit 0",
	"exit=0",
	"exit code 0",
	"exit status 0",
	"✓",
}

// gateCommandRe matches a command a worker would cite as a gate.
var gateCommandRe = regexp.MustCompile(
	`\b(make\s+(test|test-go|test-web|test-ui|bullseye|check)\S*|go\s+test|go\s+vet|go\s+build|npm\s+test|node\s+\S+_test\.js)\b`)

// pipedGateRe matches a gate command piped into another program. The pipe is
// the defect: everything after it owns the status.
var pipedGateRe = regexp.MustCompile(
	`(make\s+\S+|go\s+test[^|\n]*|go\s+vet[^|\n]*|go\s+build[^|\n]*|npm\s+test[^|\n]*)\|\s*(tail|head|grep|sed|awk|wc|tee|less|cat)\b`)

// bashArrayRe matches bash's PIPESTATUS, which does not exist in zsh — the
// shell this repo's harness actually runs.
var bashArrayRe = regexp.MustCompile(`\$\{?PIPESTATUS\[`)

// zshZeroIndexRe matches index 0 of zsh's pipestatus, which is 1-indexed, so
// the expansion is empty however right the array name is.
var zshZeroIndexRe = regexp.MustCompile(`\$\{?pipestatus\[0\]`)

// emptyStatusRe matches a printed status variable that expanded to nothing:
// "EXIT=" at end of line, "exit=" followed by whitespace or nothing.
var emptyStatusRe = regexp.MustCompile(`(?im)\b(exit|status|rc)=[ \t]*$`)

// FlagFalseGreen reads a finish report and returns every reason its green
// claim should not be believed. An empty result is not a certificate that
// the work is done — it means the report does not contradict itself.
//
// lookup resolves a cited gate id to its stored record; nil makes the check
// purely textual, which is what the hermetic tests and any caller without a
// store use.
//
// Over-broadness is the failure mode to fear here. A checker that flags
// honest reports gets ignored, and an ignored checker is worse than none: it
// launders the next real false green through a reader who has learned to skim
// past the banner. Every rule below therefore requires a green CLAIM as well
// as a suspicious shape, and the markers are narrow on purpose.
func FlagFalseGreen(report string, lookup func(string) (*Record, bool)) []Flag {
	text := strings.TrimSpace(report)
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)

	var flags []Flag
	cited := ParseAttestations(text)
	claimsGreen := claimsGreenPass(lower)

	// Attestation checks first: a cited record is the strongest evidence
	// available, and it is checkable rather than inferred.
	for _, c := range cited {
		if !c.Verdict.IsGreen() || !c.StatusIsZero() {
			flags = append(flags, Flag{
				Kind: FlagAttestationNotGreen,
				Detail: fmt.Sprintf(
					"gate %q is cited with verdict %s and exit=%s, which is not a pass",
					c.Name, c.Verdict, c.Status),
				Evidence: c.Raw,
			})
			continue
		}
		if lookup == nil {
			continue
		}
		rec, ok := lookup(c.ID)
		if !ok {
			flags = append(flags, Flag{
				Kind: FlagAttestationUnknown,
				Detail: fmt.Sprintf(
					"no gate record %s exists, so the cited green for %q was not produced by a run here",
					c.ID, c.Name),
				Evidence: c.Raw,
			})
			continue
		}
		if !rec.Verdict.IsGreen() || rec.Status() != "0" {
			flags = append(flags, Flag{
				Kind: FlagAttestationContradicted,
				Detail: fmt.Sprintf(
					"gate record %s says exit=%s %s, the report says exit=%s %s",
					c.ID, rec.Status(), rec.Verdict, c.Status, c.Verdict),
				Evidence: c.Raw,
			})
		}
	}

	if !claimsGreen {
		return flags
	}

	if m := pipedGateRe.FindString(text); m != "" && citesAStatus(lower) {
		flags = append(flags, Flag{
			Kind: FlagPipelineMasked,
			Detail: "the gate was piped into another command, so the status cited " +
				"is that last command's, not the gate's — run it under bin/gate instead",
			Evidence: strings.TrimSpace(m),
		})
	}
	if m := bashArrayRe.FindString(text); m != "" {
		flags = append(flags, Flag{
			Kind: FlagShellArrayTrap,
			Detail: "PIPESTATUS is bash-only and this harness runs zsh, where the " +
				"expansion is empty — the status was never read",
			Evidence: statusLineAround(text, m),
		})
	}
	if m := zshZeroIndexRe.FindString(text); m != "" {
		flags = append(flags, Flag{
			Kind: FlagShellArrayTrap,
			Detail: "zsh arrays index from 1, so ${pipestatus[0]} is empty — " +
				"the status was never read",
			Evidence: statusLineAround(text, m),
		})
	}
	if m := emptyStatusRe.FindString(text); m != "" {
		flags = append(flags, Flag{
			Kind:     FlagEmptyStatus,
			Detail:   "a status variable expanded to nothing; an empty status is not zero",
			Evidence: strings.TrimSpace(m),
		})
	}
	for _, a := range ScanOutput(text) {
		flags = append(flags, Flag{
			Kind: FlagOutputContradicts,
			Detail: fmt.Sprintf(
				"the report claims a pass while quoting output that says otherwise (%s)",
				a.Marker),
			Evidence: a.Line,
		})
	}
	return flags
}

// claimsGreenPass reports whether the report asserts a gate passed. Requires
// both a pass word and a gate command, so "the UI looks green" or a passing
// mention of exit codes in design prose is not a claim.
func claimsGreenPass(lower string) bool {
	if !gateCommandRe.MatchString(lower) && !strings.Contains(lower, "gate ") {
		return false
	}
	for _, m := range greenClaimMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// citesAStatus reports whether the text puts a number on the outcome. A
// report that merely shows a piped command without claiming its status is
// describing, not attesting.
func citesAStatus(lower string) bool {
	return strings.Contains(lower, "exit") ||
		strings.Contains(lower, "status") ||
		strings.Contains(lower, "$?") ||
		strings.Contains(lower, "pipestatus")
}

// statusLineAround returns the whole line containing marker, so the flag
// quotes the shell the worker actually ran.
func statusLineAround(text, marker string) string {
	i := strings.Index(text, marker)
	if i < 0 {
		return marker
	}
	start := strings.LastIndexByte(text[:i], '\n') + 1
	end := strings.IndexByte(text[i:], '\n')
	if end < 0 {
		return trimLine(text[start:])
	}
	return trimLine(text[start : i+end])
}

// BannerHeading opens the warning prepended to a flagged report.
const BannerHeading = "⚠ FALSE-GREEN CHECK (🎯T386): this report's own evidence " +
	"does not support the pass it claims."

// Banner renders flags as the note that rides in front of a report on its way
// to the overseer. Empty when there is nothing to say — silence is the normal
// case and must stay cheap to read past.
func Banner(flags []Flag) string {
	if len(flags) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(BannerHeading)
	for _, f := range flags {
		b.WriteString("\n  • ")
		b.WriteString(f.String())
	}
	b.WriteString("\n  Re-run the gate as `bin/gate -- <command>` and cite its GATE line.")
	return b.String()
}
