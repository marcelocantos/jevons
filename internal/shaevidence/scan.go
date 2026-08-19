// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package shaevidence

import (
	"strings"
)

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

// AttestationBlob extracts attestation lines and "Achieved …" prose from a
// bullseye.yaml body so a ledger walk does not treat depends_on ids or other
// hex-looking fields as evidence SHAs. Only those lines are kept — indented
// siblings (context, acceptance, …) must not be swallowed, or the first
// attestation would consume the rest of the file. Falls back to the whole
// text when no attestation markers are present (finish reports, fixtures).
func AttestationBlob(yamlText string) string {
	var b strings.Builder
	for _, line := range strings.Split(yamlText, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "attestation:") ||
			strings.HasPrefix(trim, "Achieved ") ||
			strings.Contains(trim, "Achieved ") {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if b.Len() == 0 {
		return yamlText
	}
	return b.String()
}

func classifySource(line string) string {
	trim := strings.TrimSpace(line)
	switch {
	case strings.Contains(trim, "attestation:"):
		return "attestation"
	case strings.HasPrefix(trim, "Achieved ") || strings.Contains(trim, "Achieved "):
		return "achieved"
	default:
		return "prose"
	}
}
