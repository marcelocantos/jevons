// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turndepth

import "testing"

// 🎯T392.4: the policy is the shipped ceiling. A turn at the floor is
// asked to checkpoint; one below it is left alone; the interrupt
// fallback stays disarmed unless the owner arms it.
func TestT3924EvaluateAsksAtTheCeilingAndLeavesTheCommonBandAlone(t *testing.T) {
	t.Parallel()
	p := Policy{Ceiling: MinCeiling, Grace: -1}

	below := p.Evaluate(State{Agent: "jv", Calls: MinCeiling - 1})
	if below.Action != ActionNone {
		t.Fatalf("call %d = %s; the 1–%d band must be untouched", MinCeiling-1, below.Action, MinCeiling-1)
	}

	at := p.Evaluate(State{Agent: "jv", Calls: MinCeiling})
	if at.Action != ActionRequestCheckpoint {
		t.Fatalf("call %d = %s; want request_checkpoint", MinCeiling, at.Action)
	}

	asked := p.Evaluate(State{Agent: "jv", Calls: MinCeiling + 1, Requested: true})
	if asked.Action != ActionNone {
		t.Fatalf("past ceiling with interrupt disarmed = %s; want none", asked.Action)
	}

	armed := Policy{Ceiling: MinCeiling, Grace: -1, InterruptEnabled: true}
	cut := armed.Evaluate(State{Agent: "jv", Calls: MinCeiling, Requested: true})
	if cut.Action != ActionInterrupt {
		t.Fatalf("armed fallback at ceiling+0 grace = %s; want interrupt", cut.Action)
	}
}

func TestT3924DisabledPolicyStillReportsTheCount(t *testing.T) {
	t.Parallel()
	d := Policy{Disabled: true}.Evaluate(State{Agent: "jv", Calls: 99})
	if d.Action != ActionNone || d.Calls != 99 {
		t.Fatalf("disabled = %+v; counting must continue, action must be none", d)
	}
}
