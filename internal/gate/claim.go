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

// CitationRole is what a finish report is doing with a gate it cites.
//
// The same record can play either part. A red run is a contradiction when it
// is offered as a pass, and a *result* when it is offered as proof that an
// oracle falsifies — the deliberate run against a tree with the work removed
// that 🎯T31 and this package's own doctrine ask every worker to perform. The
// record cannot tell those apart: they are the same run. Only the prose around
// the citation says which, so the role is read from the report and the store is
// never consulted (🎯T443).
type CitationRole string

const (
	// RoleClaimedPass: the report offers the gate as evidence that its work
	// passes. The default, and the reading under which a non-green citation is
	// a contradiction worth flagging.
	RoleClaimedPass CitationRole = "claimed_pass"
	// RoleFalsification: the report offers the gate as evidence that its
	// oracles have power — the red is the result being demonstrated, not a
	// failure being glossed over. Bannering these was 🎯T443's defect: the
	// checker fired on jv-t380-auto's honest report for doing exactly what the
	// acceptance criterion demanded, and a checker that flags the behaviour it
	// asks for is a checker readers learn to skim past.
	RoleFalsification CitationRole = "falsification"
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

// removalFramingRe matches a report saying which tree a deliberate red ran
// against: one with the work taken out. This is the half of the framing a
// worker cannot write by accident — a report that never says the fix was
// absent is not describing a falsification run, whatever else it says about
// failure.
var removalFramingRe = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`\bpre-? ?fix\b`,
	`\bunpatched\b`,
	`\bbefore the (fix|change|wiring)\b`,
	`\bwithout the (fix|change|wiring)\b`,
	`\bwith the (fix|change|wiring|code) (removed|reverted|out)\b`,
	`\b(fix|change|wiring|code) (removed|reverted|backed out)\b`,
	`\b(removed|reverted|backed out|stubbed out) the (fix|change|wiring|code)\b`,
}, "|"))

// failureFramingRe matches the other half: that failing is the result being
// shown. "red" needs word boundaries or it matches the tail of "measured".
var failureFramingRe = regexp.MustCompile(`(?i)\b(fail\w*|red|falsif\w*)\b`)

// contextRadius is how far from a citation's own line the framing that gives
// it a role may sit. Two lines covers the shapes workers actually write — a
// label above the attestation, the quoted failure below it — without letting
// an unrelated paragraph vouch for a red.
const contextRadius = 2

// ClassifyCitation reads what role a cited gate plays, from the report text
// around it. window is that surrounding text; see citationWindow.
//
// The citation's own attestation line is removed before matching, so the
// framing has to be prose the worker wrote about the run rather than the label
// they gave it. Otherwise naming a gate "t443-prefix-red" would classify it,
// and a role that a worker can assert by choosing a name is not a check.
func ClassifyCitation(window string, c CitedAttestation) CitationRole {
	framing := strings.ToLower(strings.ReplaceAll(window, c.Raw, " "))
	// A window that also calls this gate a pass is not describing a
	// falsification run, whatever else it says. Refusing the exemption here is
	// the safe direction: the cost is a banner on a confusingly worded honest
	// report, and the alternative is a green laundered through the word "red".
	for _, m := range greenClaimMarkers {
		if strings.Contains(framing, m) {
			return RoleClaimedPass
		}
	}
	if removalFramingRe.MatchString(framing) && failureFramingRe.MatchString(framing) {
		return RoleFalsification
	}
	return RoleClaimedPass
}

// citationWindow returns the line range whose prose gives the citation on line
// i its role: line i, plus the run of lines around it, stopping at a blank line
// or at another attestation. Stopping at another attestation is what keeps a
// markdown table of gate results from letting one row's framing speak for the
// next row's verdict.
func citationWindow(lines []string, isAttestation map[int]bool, i int) (start, end int) {
	start, end = i, i
	for start > i-contextRadius && start > 0 {
		if prev := start - 1; strings.TrimSpace(lines[prev]) == "" || isAttestation[prev] {
			break
		}
		start--
	}
	for end < i+contextRadius && end < len(lines)-1 {
		if next := end + 1; strings.TrimSpace(lines[next]) == "" || isAttestation[next] {
			break
		}
		end++
	}
	return start, end
}

// falsificationRoles reports which cited attestations a report offers as proof
// that an oracle falsifies, and which lines their framing occupies.
//
// The line set matters as much as the citation set: the quoted failure that IS
// the demonstration sits in the same neighbourhood as the citation, so flagging
// it as output contradicting a pass is the same mistake in a different arm.
// That is both halves of what fired on jv-t380-auto's report.
//
// Citations are located per line rather than searched for across the whole
// report, so an attestation wrapped across a line break is simply never exempt
// — the conservative answer, and the one that cannot be arranged deliberately.
func falsificationRoles(lines []string) (exempt map[string]bool, framingLines map[int]bool) {
	isAttestation := make(map[int]bool, len(lines))
	for i, ln := range lines {
		if attestationRe.MatchString(ln) {
			isAttestation[i] = true
		}
	}
	exempt, framingLines = map[string]bool{}, map[int]bool{}
	for i := range lines {
		if !isAttestation[i] {
			continue
		}
		start, end := citationWindow(lines, isAttestation, i)
		window := strings.Join(lines[start:end+1], "\n")
		for _, c := range ParseAttestations(lines[i]) {
			// A green citation is never exempt: it is not flagged in the first
			// place, and a pre-fix run that PASSED is a finding, not a proof.
			if c.Verdict.IsGreen() && c.StatusIsZero() {
				continue
			}
			if ClassifyCitation(window, c) != RoleFalsification {
				continue
			}
			exempt[c.Raw] = true
			for j := start; j <= end; j++ {
				framingLines[j] = true
			}
		}
	}
	return exempt, framingLines
}

// blankLines returns text with the given lines emptied, keeping every other
// line where it was. Used to hide a falsification demonstration from the
// output scan without moving the lines that remain.
func blankLines(lines []string, skip map[int]bool) string {
	if len(skip) == 0 {
		return strings.Join(lines, "\n")
	}
	kept := make([]string, len(lines))
	for i, ln := range lines {
		if !skip[i] {
			kept[i] = ln
		}
	}
	return strings.Join(kept, "\n")
}

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
	lines := strings.Split(text, "\n")
	cited := ParseAttestations(text)
	exempt, framingLines := falsificationRoles(lines)
	claimsGreen := claimsGreenPass(lower)

	// Attestation checks first: a cited record is the strongest evidence
	// available, and it is checkable rather than inferred.
	for _, c := range cited {
		if !c.Verdict.IsGreen() || !c.StatusIsZero() {
			// 🎯T443: cited as proof that the oracle falsifies, not as a pass.
			// The red is the result, and saying so is what was asked for.
			if exempt[c.Raw] {
				continue
			}
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
	// 🎯T443: the failure quoted as the content of a falsification run is that
	// run's result, not output arguing with a pass, so it is hidden from the
	// scan. Everything outside those lines is scanned as before.
	for _, a := range ScanOutput(blankLines(lines, framingLines)) {
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
