// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package delivery

import "testing"

func TestParseModeAndAlias(t *testing.T) {
	cases := []struct {
		raw   string
		alias bool
		want  Mode
		err   bool
	}{
		{"", false, ModeSubmit, false},
		{"", true, ModeInterrupt, false},
		{"submit", false, ModeSubmit, false},
		{"steer", false, ModeSteer, false},
		{"interrupt", false, ModeInterrupt, false},
		{"interrupt", true, ModeInterrupt, false},
		{"queue", false, ModeQueue, false},
		{"steer", true, "", true},
		{"cancel", false, "", true},
	}
	for _, c := range cases {
		got, err := Parse(c.raw, c.alias)
		if (err != nil) != c.err {
			t.Fatalf("Parse(%q,%v) err=%v want err=%v", c.raw, c.alias, err, c.err)
		}
		if got != c.want {
			t.Fatalf("Parse(%q,%v)=%q want %q", c.raw, c.alias, got, c.want)
		}
	}
	if len(Modes()) != 4 {
		t.Fatalf("Modes() = %v", Modes())
	}
}
