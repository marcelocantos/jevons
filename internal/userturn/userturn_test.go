// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package userturn

import "testing"

func TestLegacyInjectionText(t *testing.T) {
	facts := []string{
		"open-intent harness daemon-restart",
		"open-intent harness standing-brief",
		"open-intent harness event",
		"open-intent harness system-reminder",
		"open-intent harness background-task",
		"<user_query>open-intent harness system-reminder</user_query>",
	}
	for _, f := range facts {
		if !LegacyInjectionText(f) {
			t.Errorf("want inject: %q", f)
		}
	}
	owner := []string{
		"",
		"[Daemon restart 12:00] reattached",
		"[Jevons fleet standing brief] Execute T444.",
		"[event: daemon-restarted] fleet status",
		"<system-reminder>\nBackground task \"call-1\" completed (exit code: 0).\n</system-reminder>",
		"Background task call-2 completed",
		"Please fix the chat wire",
		"open-intent harness other",
		"open-intent harness system-reminder please",
	}
	for _, o := range owner {
		if LegacyInjectionText(o) {
			t.Errorf("want owner text: %q", o)
		}
	}
}
