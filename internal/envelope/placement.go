// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import "strings"

// PlacementReason is a typed placement rejection (🎯T1190). Not prose
// classification — only fence position relative to the author's message.
type PlacementReason string

func (r PlacementReason) String() string { return string(r) }

const (
	PlacementProseAfterFence    PlacementReason = "placement_prose_after_fence"
	PlacementNotLine1OrTrailing PlacementReason = "placement_not_line1_or_trailing"
)

// PlacementRejectReason reports when text carries a ```jevons fence Parse
// rejects for placement. Slot errors (m != nil) are not placement rejects.
func PlacementRejectReason(text string) (PlacementReason, bool) {
	m, _ := Parse(text)
	if m != nil {
		return "", false
	}
	body, _ := StripPrefixes(text)
	if body == "" || !bodyContainsJevonsFenceMarker(body) {
		return "", false
	}
	if after, ok := textAfterLastCompleteJevonsFence(body); ok {
		if strings.TrimSpace(trimGoalStatusTail(after)) != "" {
			return PlacementProseAfterFence, true
		}
	}
	return PlacementNotLine1OrTrailing, true
}

// PlacementCorrectionNotice is what the daemon sends back to the author when
// a jevons fence is present but not at an accepted placement (🎯T1190).
func PlacementCorrectionNotice(text string) string {
	reason, ok := PlacementRejectReason(text)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("⚠ Your report contains a `jevons` envelope the daemon cannot read (🎯T1190): " + reason.String())
	b.WriteString("\nReason `" + reason.String() + "`: place the envelope at line 1 of your message (payload may follow), " +
		"or as the message's final block after any prose summary — only `GOAL_STATUS:` may follow the closing fence.")
	b.WriteString("\nResend with the envelope at line 1, or move it to the final block with no prose after the closing fence.")
	b.WriteString("\nA finish-report the daemon reads as done has this shape:\n" + DoneShape + "\n" + DoneShapeNote)
	return b.String()
}

func bodyContainsJevonsFenceMarker(body string) bool {
	const open = "```" + FenceInfo
	if strings.Contains(body, open) {
		return true
	}
	lower := strings.ToLower(body)
	return strings.Contains(lower, "```"+strings.ToLower(FenceInfo))
}

func textAfterLastCompleteJevonsFence(body string) (string, bool) {
	end, ok := lastCompleteJevonsFenceEnd(body)
	if !ok {
		return "", false
	}
	if end >= len(body) {
		return "", true
	}
	return body[end:], true
}

func lastCompleteJevonsFenceEnd(body string) (int, bool) {
	lines := strings.Split(body, "\n")
	for closeLine := len(lines) - 1; closeLine >= 0; closeLine-- {
		if strings.TrimSpace(lines[closeLine]) != "```" {
			continue
		}
		for openLine := closeLine - 1; openLine >= 0; openLine-- {
			opener := strings.TrimSpace(lines[openLine])
			if !strings.HasPrefix(opener, "```") {
				continue
			}
			info := strings.TrimSpace(strings.TrimPrefix(opener, "```"))
			fields := strings.Fields(info)
			if len(fields) == 0 || strings.ToLower(fields[0]) != FenceInfo {
				break
			}
			end := 0
			for i := 0; i <= closeLine; i++ {
				end += len(lines[i])
				if i < closeLine {
					end++
				}
			}
			if closeLine < len(lines)-1 {
				end++
			}
			return end, true
		}
	}
	return 0, false
}
