// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"

	"github.com/marcelocantos/jevons/internal/shaevidence"
)

// FlagUnreachableSHAs reports evidence SHAs in a finish report that are not
// ancestors of HEAD (🎯T427). check is typically shaevidence.CheckInRepo(dir,
// "HEAD"); nil skips the check entirely so hermetic false-green tests stay
// offline.
//
// Composed with FlagFalseGreen by callers (cmd/gate check, daemon banner) —
// kept separate so the existing (report, lookup) signature does not grow a
// third dependency every hermetic test would have to invent.
func FlagUnreachableSHAs(report string, check shaevidence.CheckFunc) []Flag {
	if check == nil {
		return nil
	}
	findings := shaevidence.ScanFindings(report, check)
	if len(findings) == 0 {
		return nil
	}
	flags := make([]Flag, 0, len(findings))
	for _, f := range findings {
		var detail string
		switch f.Kind {
		case shaevidence.Rewritten:
			detail = fmt.Sprintf(
				"cited SHA %s exists as an object but is not an ancestor of HEAD "+
					"(rewritten — often a yaml-only tip bullseye amended away); "+
					"prove reachability with `git merge-base --is-ancestor %s HEAD` before citing (🎯T427)",
				f.SHA, f.SHA)
		case shaevidence.Missing:
			detail = fmt.Sprintf(
				"cited SHA %s does not exist in this repository; "+
					"prove reachability with `git merge-base --is-ancestor %s HEAD` before citing (🎯T427)",
				f.SHA, f.SHA)
		default:
			detail = fmt.Sprintf("cited SHA %s is not reachable from HEAD (🎯T427)", f.SHA)
		}
		flags = append(flags, Flag{
			Kind:     FlagSHAUnreachable,
			Detail:   detail,
			Evidence: f.Line,
		})
	}
	return flags
}
