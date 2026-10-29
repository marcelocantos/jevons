// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestT769NoManualAgentStart(t *testing.T) {
	name := "jv-t769-worker"
	ok := []byte(`msg="agent started" name=jv-t769-worker provider=grok
msg="fleet start finished"`)
	if err := t769NoManualAgentStart(ok, name); err != nil {
		t.Fatal(err)
	}
	bad := []byte(`msg="mcp tool" name=jevons_agent_start input="{\"name\":\"jv-t769-worker\"}"`)
	if err := t769NoManualAgentStart(bad, name); err == nil {
		t.Fatal("expected manual start detection")
	}
	if err := t769NoManualAgentStart([]byte(`name=other`), name); err != nil {
		t.Fatal(err)
	}
}
