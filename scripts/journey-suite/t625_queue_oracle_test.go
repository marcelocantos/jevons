// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/sendq"
)

func TestT625QueueProviderRejectsWrongAndUnrelatedLaunches(t *testing.T) {
	for _, tc := range []struct {
		name, logs string
		wantOK     bool
	}{
		{"matching captured format", `time=2026-09-06T08:52:02.532+10:00 level=INFO msg="agent started" name=worker provider=grok session=fixture connect_pid=2916 connect_url_set=true materialized=false`, true},
		{"wrong provider", `msg="agent started" name=worker provider=claude`, false},
		{"another worker", `msg="agent started" name=other provider=grok`, false},
		{"generic recovery", `msg="recovered a queued message" name=worker provider=grok`, false},
		{"missing provider", `msg="agent started" name=worker`, false},
		{"malformed", `msg="agent started name=worker provider=grok`, false},
		{"quoted embedded fields", `msg="agent started name=worker provider=grok" name=other provider=claude`, false},
		{"duplicate field", `msg="agent started" name=worker provider=claude provider=grok`, false},
		{"mixed providers", "msg=\"agent started\" name=worker provider=grok\nmsg=\"agent started\" name=worker provider=claude", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := queueJourneyProvider([]byte(tc.logs), "worker", "grok"); (err == nil) != tc.wantOK {
				t.Fatalf("provider evidence: error=%v, wantOK=%v", err, tc.wantOK)
			}
		})
	}
	// Generate evidence with the daemon's actual handler as an independent
	// compatibility check, including quoted/escaped names and other fields.
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	logger.Info("agent started", "name", `quoted "worker"`, "provider", "grok", "detail", "irrelevant name=other")
	if err := queueJourneyProvider(output.Bytes(), `quoted "worker"`, "grok"); err != nil {
		t.Fatal(err)
	}
}

func TestT625QueueAcceptanceRejectsUnestablishedPreconditions(t *testing.T) {
	accepted := sendq.Entry{ID: "acceptance", Text: "fresh payload"}
	for _, tc := range []struct {
		name    string
		entries []sendq.Entry
		wantOK  bool
	}{
		{"one pending request", []sendq.Entry{accepted}, true},
		{"queued zero", nil, false},
		{"duplicate sends", []sendq.Entry{accepted, accepted}, false},
		{"stale payload", []sendq.Entry{{ID: "acceptance", Text: "old payload"}}, false},
		{"no durable identity", []sendq.Entry{{Text: "fresh payload"}}, false},
		{"already attempting", []sendq.Entry{{ID: "acceptance", Text: "fresh payload", State: sendq.Attempting}}, false},
		{"uncertain delivery", []sendq.Entry{{ID: "acceptance", Text: "fresh payload", State: sendq.Uncertain}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := queueJourneyAcceptance(tc.entries, "fresh payload")
			if (err == nil) != tc.wantOK {
				t.Fatalf("acceptance evidence: error=%v, wantOK=%v", err, tc.wantOK)
			}
		})
	}
}

func TestT625QueueResultMustBeAbsentBeforeRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result")
	if err := queueJourneyNoResult(path); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"", "fresh payload\n", "unrelated\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := queueJourneyNoResult(path); err == nil {
			t.Fatalf("pre-restart result %q incorrectly accepted", body)
		}
	}
}
