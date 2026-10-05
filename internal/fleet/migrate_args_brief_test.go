// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import "testing"

// A live migration hands Claudia the brief and retained history the
// successor starts from. A stand-in written for claudia v0.42.0 forwarded
// only provider, model, reason and force, so from v0.14.0 every live
// successor started blank.
func TestMigrateRequestCarriesBriefAndRetainedHistory(t *testing.T) {
	args := MigrateRequest{
		Provider: "anthropic", Model: "m", Reason: "r", Force: true,
		ContextBrief: "brief", RetainedTranscript: "history",
	}.migrateArgs()
	if args.ContextBrief != "brief" || args.RetainedTranscript != "history" {
		t.Fatalf("migrate args dropped the handover: %+v", args)
	}
	if args.Provider != "anthropic" || args.Model != "m" || args.Reason != "r" || !args.Force {
		t.Fatalf("migrate args = %+v", args)
	}
}
