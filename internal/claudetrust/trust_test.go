// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package claudetrust_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/claudetrust"
)

// Live last-frame from ge-po on Colossus 2026-09-20 (🎯T709).
const gePoTrustFrame = "Quick safety check: Is this a project you created or one you trust? (Like your\n" +
	" own code, a well-known open source project, or work from your team). If not,\n" +
	" take a moment to review what's in this folder first.\n\n" +
	" Claude Code'll be able to read, edit, and execute files here.\n\n" +
	" ⚠ This folder pre-approves 10 tool permissions in .claude/settings.local.json:"

func TestIsDialogLiveGePoFrame(t *testing.T) {
	t.Parallel()
	if !claudetrust.IsDialog(gePoTrustFrame) {
		t.Fatal("live ge-po trust frame must classify as a workspace-trust dialog")
	}
	wrapped := "start prompt not delivered to \"ge-po\": send failed: Agent CLI stalled on startup " +
		"(startup_stall / no_composer): no idle input box within the ready timeout — not a cloud outage; " +
		"the seat is retried. Last frame: " + gePoTrustFrame
	if !claudetrust.IsDialog(wrapped) {
		t.Fatal("formatted no_composer owner copy carrying the frame must still classify")
	}
	if claudetrust.IsDialog("claude not ready (no_composer): no idle input box after 30s; last frame:\n● Rebuilding…") {
		t.Fatal("a generic no_composer frame is not a trust dialog")
	}
}

func TestAcceptBytesCreatesProjectAndPreservesSiblings(t *testing.T) {
	t.Parallel()
	const workdir = "/Users/marcelo/work/github.com/squz/ge"
	in := []byte(`{
  "numStartups": 9,
  "mcpServers": {"other": {"type": "http", "url": "http://127.0.0.1:9"}},
  "projects": {
    "/other": {"hasTrustDialogAccepted": false, "allowedTools": ["Bash"]}
  }
}
`)
	out, changed, err := claudetrust.AcceptBytes(in, workdir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("missing trust flag must change the document")
	}
	if !claudetrust.Accepted(out, workdir) {
		t.Fatal("workdir must be accepted after merge")
	}
	if claudetrust.Accepted(out, "/other") {
		t.Fatal("sibling project must not be flipped to accepted")
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["mcpServers"]; !ok {
		t.Fatal("mcpServers must survive — T464/T376 residual")
	}
	if _, ok := doc["numStartups"]; !ok {
		t.Fatal("unrelated top-level keys must survive")
	}
	var projects map[string]map[string]any
	if err := json.Unmarshal(doc["projects"], &projects); err != nil {
		t.Fatal(err)
	}
	other := projects["/other"]
	if other["hasTrustDialogAccepted"] != false {
		t.Fatalf("sibling trust = %v, want false", other["hasTrustDialogAccepted"])
	}
	if tools, ok := other["allowedTools"].([]any); !ok || len(tools) != 1 {
		t.Fatalf("sibling allowedTools lost: %#v", other["allowedTools"])
	}
}

func TestAcceptBytesIdempotentWhenAlreadyTrue(t *testing.T) {
	t.Parallel()
	in := []byte(`{"projects":{"/repo":{"hasTrustDialogAccepted":true,"n":1}}}`)
	out, changed, err := claudetrust.AcceptBytes(in, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("already-accepted must not rewrite the hot file")
	}
	if string(out) != string(in) {
		t.Fatal("idempotent accept must return the original bytes")
	}
}

func TestAcceptBytesEmptyAndMalformed(t *testing.T) {
	t.Parallel()
	out, changed, err := claudetrust.AcceptBytes(nil, "/new")
	if err != nil || !changed || !claudetrust.Accepted(out, "/new") {
		t.Fatalf("empty doc: out=%s changed=%v err=%v", out, changed, err)
	}
	if _, _, err := claudetrust.AcceptBytes([]byte("{"), "/new"); err == nil {
		t.Fatal("malformed JSON must fail closed, not clobber")
	}
	if _, _, err := claudetrust.AcceptBytes([]byte(`{}`), ""); err == nil {
		t.Fatal("empty workdir must fail")
	}
}

func TestAcceptWritesTempConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	workdir := filepath.Join(dir, "squz-ge")
	changed, err := claudetrust.Accept(path, workdir)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first accept of a missing file must write")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !claudetrust.Accepted(data, workdir) {
		t.Fatalf("wrote but not accepted: %s", data)
	}
	changed, err = claudetrust.Accept(path, workdir)
	if err != nil || changed {
		t.Fatalf("second accept changed=%v err=%v", changed, err)
	}
}

func TestPrepareLaunchOnlyClaude(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	wd := filepath.Join(dir, "ge")
	if err := os.Mkdir(wd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "ge-po", WorkDir: wd, SessionID: "s1",
		Provider: claudia.ProviderClaude, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "grok-po", WorkDir: wd, SessionID: "s2",
		Provider: claudia.ProviderGrok, Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}

	claudetrust.PrepareLaunchAt(reg, "grok-po", path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Grok launch must not touch the Claude config")
	}
	claudetrust.PrepareLaunchAt(reg, "ge-po", path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !claudetrust.Accepted(data, wd) {
		t.Fatalf("Claude PO workdir not trusted: %s", data)
	}
	if !strings.Contains(string(data), "hasTrustDialogAccepted") {
		t.Fatal("expected hasTrustDialogAccepted in written config")
	}
}
