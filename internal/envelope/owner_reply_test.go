// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import "testing"

func TestClassifyOverseerOwnerStream(t *testing.T) {
	decision := "```jevons\njevons: kind owner-reply\njevons: purpose decision\n```\nNeeds your call on T1140."
	routine := "```jevons\njevons: kind owner-reply\njevons: purpose routine\n```\nAll routine post-restart status confirmations — no action needed."
	missingPurpose := "```jevons\njevons: kind owner-reply\n```\nUnmarked purpose is routine."

	cases := []struct {
		text     string
		terminal bool
		want     OverseerOwnerStreamClass
	}{
		{"[silent] ok", true, OverseerOwnerSuppress},
		{"No action needed.", true, OverseerOwnerSuppress},
		{decision, false, OverseerOwnerPending},
		{decision, true, OverseerOwnerShow},
		{routine, true, OverseerOwnerSuppress},
		{missingPurpose, true, OverseerOwnerSuppress},
		{"```jevons\njevons: kind owner-reply\njevons: purpose answer\n```\nYes.", true, OverseerOwnerShow},
		{"```jevons\njevons: kind owner-reply\njevons: purpose decision\n```", true, OverseerOwnerSuppress},
	}
	for _, c := range cases {
		if got := ClassifyOverseerOwnerStream(c.text, c.terminal); got != c.want {
			t.Fatalf("ClassifyOverseerOwnerStream(%q, %v) = %v want %v", c.text, c.terminal, got, c.want)
		}
	}
}

func TestOverseerOwnerChatBodyStripsFence(t *testing.T) {
	in := "```jevons\njevons: kind owner-reply\njevons: purpose fault\n```\nDaemon connection refused — transient."
	if got := OverseerOwnerChatBody(in); got != "Daemon connection refused — transient." {
		t.Fatalf("body=%q", got)
	}
}
