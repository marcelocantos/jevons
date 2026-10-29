package server

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/marcelocantos/jevons/internal/planusage"
	"os"
	"strings"
	"testing"
	"time"
)

func TestT1244RelayPlanUsageResponseUsesHTTPPayload(t *testing.T) {
	now := time.Now().UTC()
	remaining := 42.0
	s := &Server{}
	s.SetPlanUsageSource(func() any {
		return planusage.Snapshot{At: now, Backends: []planusage.Backend{{Provider: "claude", Status: "available", FetchedAt: now, Windows: []planusage.Window{{Name: "weekly", RemainingPercent: &remaining}}}}}
	})
	b, err := json.Marshal(s.relayPlanUsageResponse("request_1"))
	if err != nil {
		t.Fatal(err)
	}
	var msg struct {
		Type    string             `json:"type"`
		ID      string             `json:"id"`
		Payload planusage.Snapshot `json:"payload"`
	}
	if err := json.Unmarshal(b, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "plan_usage_response" || msg.ID != "request_1" || len(msg.Payload.Backends) != 1 || *msg.Payload.Backends[0].Windows[0].RemainingPercent != 42 {
		t.Fatalf("response: %s", b)
	}
	if validRelayRequestID("../bad") || validRelayRequestID("") || !validRelayRequestID("abc_12") {
		t.Fatal("request id validation")
	}
}

// Drive the actual read-loop dispatcher, not just its response formatter.
// A credential existing on disk is not proof the current connection is paired.
func TestT1244RelayInboundRejectsEveryUnpairedMessage(t *testing.T) {
	// An existing credential does not upgrade a relay connection established
	// before pairing. Check both credential-present and credential-absent paths.
	for _, credentialPresent := range []bool{false, true} {
		s := &Server{creds: NewCredentialStore(t.TempDir() + "/credential.json")}
		if credentialPresent {
			if err := s.creds.Save(testPairingRecord(t, "existing")); err != nil {
				t.Fatal(err)
			}
		}
		sent := 0
		for _, frame := range []string{
			`{"type":"action","action":"send_message","value":"injected"}`,
			`{"type":"user_message","text":"injected"}`,
			`{"type":"plan_usage_request","id":"widget_1"}`,
		} {
			s.handleRelayInbound([]byte(frame), false, func(any) { sent++ })
		}
		if sent != 0 {
			t.Fatalf("credentialPresent=%v: unpaired relay sent %d messages", credentialPresent, sent)
		}
	}
}

func TestT1244PairedRelayDispatcherServesPlanUsage(t *testing.T) {
	s := &Server{}
	s.SetPlanUsageSource(func() any { return json.RawMessage(`{"backends":[{"provider":"codex"}]}`) })
	var response any
	s.handleRelayInbound([]byte(`{"type":"plan_usage_request","id":"widget_2"}`), true, func(v any) { response = v })
	b, err := json.Marshal(response)
	if err != nil || !json.Valid(b) || !bytes.Contains(b, []byte(`"id":"widget_2"`)) || !bytes.Contains(b, []byte(`"provider":"codex"`)) {
		t.Fatalf("response %s, %v", b, err)
	}
}

func TestT1244MalformedCredentialsFailBeforeRelayDial(t *testing.T) {
	path := t.TempDir() + "/credential.json"
	if err := os.WriteFile(path, []byte(`{"version":2,"primary":"x","peers":{"x":{"peer_instance_id":"other"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{creds: NewCredentialStore(path)}
	if _, err := s.ConnectRelay(context.Background(), "https://relay.invalid", "", "primary"); err == nil || !strings.Contains(err.Error(), "credential") {
		t.Fatalf("malformed store must reject before network dial: %v", err)
	}
}
