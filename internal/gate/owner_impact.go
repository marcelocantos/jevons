// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"path"
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// 🎯T1122 clause 3: "Owner-visible changes carry the question in their
// review: a PR / finish-report touching ui/ or the overseer's owner-facing
// speech states what the owner does differently because of it, and the PO
// refuses one that cannot say."
//
// FlagOwnerImpactMissing is the mechanical half of that refusal: a
// finish-report whose declared oracle commit changed owner-visible surface
// — non-test ui/src, ui/index.html, or the overseer/persona doctrine files
// that shape the owner's chat — without naming an owner-impact slot (or
// with an explicit "none") is flagged. The flag does not judge whether the
// claimed impact is RIGHT; it only requires that the question was asked.

// isOwnerImpactSourcePath reports whether a changed path falls inside the
// surface clause 3 names. Test files under ui/src are excluded: a test-only
// change does not touch what the owner sees.
func isOwnerImpactSourcePath(p string) bool {
	p = strings.TrimSpace(p)
	switch p {
	case "ui/index.html", "internal/roles/builtin/overseer.md", "internal/config/persona.md":
		return true
	}
	if strings.HasPrefix(p, "ui/src/") {
		return !isUIImpactTestPath(p)
	}
	return false
}

// isUIImpactTestPath names ui/src paths that are test-only: *.test.ts(x),
// *.test.js(x), *_test.go-style suffixes, and anything under a __tests__
// directory. A test file changing alongside production source still trips
// the flag on the production path; this only excuses paths that are
// entirely test.
func isUIImpactTestPath(p string) bool {
	if strings.Contains(p, "/__tests__/") {
		return true
	}
	base := path.Base(p)
	for _, suf := range []string{".test.ts", ".test.tsx", ".test.js", ".test.jsx", "_test.ts", "_test.tsx"} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	return false
}

// isCommitLikeSHA is FlagLandingSHA's own validated-shape check, reused so
// an untrusted report-controlled string is never passed to git as a
// revision argument that could begin with '-'.
func isCommitLikeSHA(sha string) bool {
	if len(sha) < 7 || len(sha) > 64 {
		return false
	}
	for _, c := range sha {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// FlagOwnerImpactMissing implements clause 3's mechanical check (🎯T1122).
// m is the already-parsed finish-report envelope; workDir is the repo the
// oracle SHA is checked against. Declared owner-impact (decision, fault, or
// plan-change) skips the check entirely — the slot was filled, whatever its
// value. No SHA, no workDir, or a SHA git cannot resolve (read-only report,
// detached commit) is silent: there is nothing to diff.
func CheckOwnerImpactMissing(m *envelope.Message, workDir string) []Flag {
	if m == nil || m.Kind != envelope.KindFinishReport {
		return nil
	}
	if m.OwnerImpact.Declared() {
		return nil
	}
	sha := strings.TrimSpace(m.SHA)
	if sha == "" || !isCommitLikeSHA(sha) || strings.TrimSpace(workDir) == "" {
		return nil
	}
	// "Its merge-base" is the commit's own first parent: for a single
	// landed commit that IS the merge-base of (sha, parent), and it is
	// what makes the diff "what this commit changed" rather than
	// everything already on the branch before the worker started
	// (which `git diff base..sha` against a stale HEAD would include —
	// HEAD here is the worker's own worktree tip, usually sha itself).
	parent := sha + "^1"
	base, err := gitOut(workDir, "merge-base", sha, parent)
	if err != nil {
		return nil
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return nil
	}
	out, err := gitOut(workDir, "diff", "--name-only", base, sha)
	if err != nil {
		return nil
	}
	var touched []string
	for _, f := range strings.Split(out, "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if isOwnerImpactSourcePath(f) {
			touched = append(touched, f)
		}
	}
	if len(touched) == 0 {
		return nil
	}
	return []Flag{{
		Kind: FlagOwnerImpactMissingKind,
		Detail: fmt.Sprintf(
			"commit %s touches owner-visible surface (%s) with no owner-impact slot (or an "+
				"explicit \"none\"): name what the owner does differently — decision, fault, or "+
				"plan-change (🎯T1122) — the PO refuses this report until the slot is filled",
			shortSHA(sha), strings.Join(touched, ", ")),
		Evidence: "jevons: oracle sha=" + sha,
	}}
}

// CheckOwnerImpactMissingReport is the report-time convenience wrapping
// envelope.Parse, for callers (false_green.go) that only have report text.
func CheckOwnerImpactMissingReport(report, workDir string) []Flag {
	m, _ := envelope.Parse(report)
	return CheckOwnerImpactMissing(m, workDir)
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
