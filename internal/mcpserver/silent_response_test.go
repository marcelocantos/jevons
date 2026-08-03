// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
)

func TestIsSilentAgentResponse(t *testing.T) {
	t.Parallel()
	if !IsSilentAgentResponse("[silent] T222 already working") {
		t.Fatal("prefix")
	}
	if !IsSilentAgentResponse("[SILENT] ok") {
		t.Fatal("case")
	}
	if !IsSilentAgentResponse("  [silent] continued jv-x") {
		t.Fatal("trim")
	}
	if IsSilentAgentResponse("T222 still working. No continue needed.") {
		t.Fatal("unmarked status must NOT filter")
	}
	if IsSilentAgentResponse("") {
		t.Fatal("empty")
	}
}

func TestNotifySuppressesSilentAgentResponse(t *testing.T) {
	s := &Server{}
	var got string
	s.SetNotify(func(text string) { got = text })
	s.notify("jevons-po", "[silent] T222 and T223 both still working. No continue needed.")
	if got != "" {
		t.Fatalf("silent reply should not notify overseer: %q", got)
	}
	s.notify("jv-worker", "done: PR green")
	if !strings.Contains(got, "done: PR green") {
		t.Fatalf("normal reply still notifies: %q", got)
	}
}

func TestOpsEventBodiesTeachSilentPrefix(t *testing.T) {
	t.Parallel()
	d := FormatDaemonRestartedText("jevons-po", []WorkerIdleRef{{Name: "jv-x", TargetID: "T1"}})
	if !strings.Contains(d, SilentResponsePrefix) {
		t.Fatal("daemon-restarted body must teach [silent]")
	}
	w := FormatWorkerIdleText(WorkerIdleRef{Name: "jv-x", TargetID: "T1"})
	if !strings.Contains(w, SilentResponsePrefix) {
		t.Fatal("worker-idle body must teach [silent]")
	}
}
