// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package repowatch

import (
	"fmt"
	"strings"
	"time"
)

// EventSource labels repo-watch briefs on the wire to a PO.
const EventSource = "repo-watch"

// Brief is one typed delivery to a PO about a single repo's delta.
type Brief struct {
	Repo      string    `json:"repo"`
	PO        string    `json:"po"`
	At        time.Time `json:"at"`
	Items     []Item    `json:"items"`
	Trigger   string    `json:"trigger,omitempty"`
}

// FormatBrief renders the daemon-delivered envelope body. The caller wraps
// with butler.FormatEventPush(EventSource, …). Typed markers let a PO
// recogniser route without parsing free prose.
func FormatBrief(b Brief) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "jevons: kind repo-watch\n")
	fmt.Fprintf(&buf, "jevons: repo %s\n", b.Repo)
	fmt.Fprintf(&buf, "jevons: po %s\n", b.PO)
	if b.Trigger != "" {
		fmt.Fprintf(&buf, "jevons: trigger %s\n", b.Trigger)
	}
	if !b.At.IsZero() {
		fmt.Fprintf(&buf, "jevons: at %s\n", b.At.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(&buf, "Repo-watch (🎯T1022): %d new/updated item(s) on %s\n\n", len(b.Items), b.Repo)
	for _, it := range b.Items {
		kind := string(it.Kind)
		fmt.Fprintf(&buf, "- %s #%d %s\n  %s\n", kind, it.Number, it.Title, it.URL)
		fmt.Fprintf(&buf, "  author=%s state=%s updated=%s",
			it.Author, it.State, it.UpdatedAt.UTC().Format(time.RFC3339))
		if it.Kind == KindPR {
			fmt.Fprintf(&buf, " draft=%v ci=%s files=%d +%d/-%d",
				it.Draft, it.CIState, it.FilesChanged, it.Additions, it.Deletions)
		}
		if len(it.Labels) > 0 {
			fmt.Fprintf(&buf, " labels=%s", strings.Join(it.Labels, ","))
		}
		buf.WriteByte('\n')
		// Hint the conservative risk class with what the daemon can see
		// without a diff (full classification is the PO's job with the
		// ClassifyRepoItemRisk helper / doctrine).
		hint := hintRisk(it)
		fmt.Fprintf(&buf, "  risk_hint=%s\n", hint)
	}
	buf.WriteString("\nTriage each item per the 🎯T1022.3 doctrine (docs/design/po-github-watch.md):\n")
	buf.WriteString("  direct → gh comment/label/close or (docs-only / bot minor-patch dep bump) approve+squash-merge.\n")
	buf.WriteString("  else   → jevons_repo_escalate with assessment + recommended action.\n")
	buf.WriteString("Do not auto-mirror every issue into bullseye; mirror is the accept-as-work step.\n")
	return buf.String()
}

func hintRisk(it Item) string {
	if it.Kind == KindPR {
		if it.Draft {
			return "escalate (draft)"
		}
		if it.IsBot && it.CIState == "success" {
			return "maybe-direct (bot + green CI — confirm dep-bump minor/patch or docs-only)"
		}
		if it.CIState == "success" && it.FilesChanged > 0 && it.FilesChanged <= 3 && it.Additions+it.Deletions < 40 {
			return "maybe-direct (small + green CI — confirm docs-only)"
		}
		return "escalate (default for PRs until classified)"
	}
	return "classify (issue — duplicate/question/bug_report/spam → direct; else escalate)"
}
