// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package shaevidence

import (
	"strings"
)

const achievedStampWord = "Achieved"

// Finding is one cited evidence SHA that failed the ancestor check.
type Finding struct {
	SHA    string
	Kind   Reachability
	Line   string
	Source string // "attestation" | "achieved" | "prose"
}

// ScanFindings runs check over every evidence SHA in text and returns those
// that are not ancestors. Ancestor citations are omitted — silence is the
// normal case. Historical unreachable citations are REPORTED, never rewritten
// in place (🎯T427 acceptance 5).
func ScanFindings(text string, check CheckFunc) []Finding {
	if check == nil {
		return nil
	}
	var out []Finding
	for _, c := range ExtractEvidenceSHAs(text) {
		k := check(c.SHA)
		if k == Ancestor {
			continue
		}
		out = append(out, Finding{
			SHA:    c.SHA,
			Kind:   k,
			Line:   c.Line,
			Source: classifySource(c.Line),
		})
	}
	return out
}

// AttestationBlob keeps attestation fields and achieved-date stamps from a
// bullseye.yaml body so a ledger walk does not treat depends_on ids or other
// hex-looking fields as evidence SHAs. Only those lines are kept — indented
// siblings (context, acceptance, …) must not be swallowed, or the first
// attestation would consume the rest of the file. Falls back to the whole
// text when no such lines are present (finish reports, fixtures). A sentence
// that merely contains the word Achieved is not a stamp.
func AttestationBlob(yamlText string) string {
	var b strings.Builder
	for _, line := range strings.Split(yamlText, "\n") {
		trim := strings.TrimSpace(line)
		if attestationField(trim) || achievedStamp(trim) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if b.Len() == 0 {
		return yamlText
	}
	return b.String()
}

// attestationField reports a YAML attestation key. The key is the text
// before the first colon, not a search for the word inside the line.
func attestationField(trim string) bool {
	key, _, found := strings.Cut(trim, ":")
	return found && key == "attestation"
}

// achievedStamp reports a ledger stamp: the first field is Achieved and the
// next field is a YYYY-MM-DD date. Interior words are not a stamp.
func achievedStamp(trim string) bool {
	fields := strings.Fields(trim)
	if len(fields) < 2 || fields[0] != achievedStampWord {
		return false
	}
	date := strings.TrimSuffix(fields[1], ":")
	if len(date) != len("2006-01-02") {
		return false
	}
	for i, c := range date {
		switch i {
		case 4, 7:
			if c != '-' {
				return false
			}
		default:
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

func classifySource(line string) string {
	trim := strings.TrimSpace(line)
	switch {
	case attestationField(trim):
		return "attestation"
	case achievedStamp(trim):
		return "achieved"
	default:
		return "prose"
	}
}
