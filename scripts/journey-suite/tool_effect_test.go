// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/marcelocantos/claudia"
)

func TestT625ToolObservationRequiresCurrentRequestCall(t *testing.T) {
	const prompt, token = "capture this fresh idea", "fresh-idea"
	owner := ownerMuxFixture(t, ownerMuxChannel, "user", 2, prompt, "", "owner", "put")
	reply := ownerMuxFixture(t, ownerMuxChannel, "assistant", 5, token, "end_turn", "", "put")
	tool := func(channel, kind, name string, input any, index int) []byte {
		data := ownerMuxFixture(t, channel, kind, index, "", "", "", "put")
		var envelope map[string]any
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		event := envelope["body"].(map[string]any)["event"].(map[string]any)
		event["name"], event["input"] = name, input
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	args := map[string]any{"text": token, "source": "mcp"}
	call := tool(ownerMuxChannel, "tool_use", "jevons_idea_capture", args, 3)
	for _, tc := range []struct {
		name   string
		frames [][]byte
		want   bool
	}{
		{"reply alone", [][]byte{owner, reply}, false},
		{"matching MCP call", [][]byte{owner, call, reply}, true},
		{"namespaced MCP call", [][]byte{owner, tool(ownerMuxChannel, "tool_use", "mcp__"+mcpName+"__jevons_idea_capture", args, 3), reply}, true},
		{"Grok discovered tool", [][]byte{owner, tool(ownerMuxChannel, "tool_use", mcpName+"__jevons_idea_capture", map[string]any{"tool_name": mcpName + "__jevons_idea_capture", "tool_input": args}, 3), reply}, true},
		{"Grok wrong dispatch", [][]byte{owner, tool(ownerMuxChannel, "tool_use", mcpName+"__jevons_idea_capture", map[string]any{"tool_name": "other", "tool_input": args}, 3), reply}, false},
		{"Grok malformed arguments", [][]byte{owner, tool(ownerMuxChannel, "tool_use", mcpName+"__jevons_idea_capture", map[string]any{"tool_name": mcpName + "__jevons_idea_capture", "tool_input": []any{args}}, 3), reply}, false},
		{"Cursor MCP dispatch", [][]byte{owner, tool(ownerMuxChannel, "tool_use", mcpName+": jevons_idea_capture", map[string]any{"providerIdentifier": mcpName, "toolName": "jevons_idea_capture", "args": args}, 3), reply}, true},
		{"Cursor wrong server", [][]byte{owner, tool(ownerMuxChannel, "tool_use", mcpName+": jevons_idea_capture", map[string]any{"providerIdentifier": "other", "toolName": "jevons_idea_capture", "args": args}, 3), reply}, false},
		{"Cursor wrong tool", [][]byte{owner, tool(ownerMuxChannel, "tool_use", mcpName+": jevons_idea_capture", map[string]any{"providerIdentifier": mcpName, "toolName": "other", "args": args}, 3), reply}, false},
		{"Cursor malformed arguments", [][]byte{owner, tool(ownerMuxChannel, "tool_use", mcpName+": jevons_idea_capture", map[string]any{"providerIdentifier": mcpName, "toolName": "jevons_idea_capture", "args": []any{args}}, 3), reply}, false},
		{"different tool", [][]byte{owner, tool(ownerMuxChannel, "tool_use", "shell", args, 3), reply}, false},
		{"assistant names tool", [][]byte{owner, tool(ownerMuxChannel, "assistant", "jevons_idea_capture", args, 3), reply}, false},
		{"wrong nonce", [][]byte{owner, tool(ownerMuxChannel, "tool_use", "jevons_idea_capture", map[string]any{"text": "stale-idea", "source": "mcp"}, 3), reply}, false},
		{"wrong source", [][]byte{owner, tool(ownerMuxChannel, "tool_use", "jevons_idea_capture", map[string]any{"text": token, "source": "api"}, 3), reply}, false},
		{"malformed input", [][]byte{owner, tool(ownerMuxChannel, "tool_use", "jevons_idea_capture", map[string]any{"text": []string{token}, "source": "mcp"}, 3), reply}, false},
		{"another conversation", [][]byte{owner, tool("transcript:worker", "tool_use", "jevons_idea_capture", args, 3), reply}, false},
		{"call before owner", [][]byte{call, owner, reply}, false},
		{"old call patched later", [][]byte{call, owner, call, reply}, false},
		{"old index after owner", [][]byte{owner, tool(ownerMuxChannel, "tool_use", "jevons_idea_capture", args, 1), reply}, false},
		{"late call after final", [][]byte{owner, reply, call}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := make(chan []byte, len(tc.frames))
			for _, data := range tc.frames {
				frames <- data
			}
			close(frames)
			saw := false
			err := waitOwnerMuxReplyObserved(context.Background(), frames, prompt, token, func(frame ownerMuxFrame) {
				saw = saw || matchingIdeaToolCall(frame, token)
			})
			if err != nil {
				t.Fatalf("reply: %v", err)
			}
			if saw != tc.want {
				t.Fatalf("matching current-request tool call=%v, want %v", saw, tc.want)
			}
		})
	}
}

// Exercise the entire journey against an adversarial peer. These are tests of
// the oracle's bindings, not evidence that the product can execute a tool.
func TestT625ToolJourneyRejectsFalseGreens(t *testing.T) {
	for _, tc := range []struct {
		name, before, effect string
		call, wantOK         bool
	}{
		{"exact call and durable effect", `{"ideas":[]}`, "durable", true, true},
		{"reply and effect without call", `{"ideas":[]}`, "durable", false, false},
		{"API only effect", `{"ideas":[]}`, "api-only", true, false},
		{"different durable identity", `{"ideas":[]}`, "different-id", true, false},
		{"no effect", `{"ideas":[]}`, "absent", true, false},
		{"wrong returned identity", `{"ideas":[]}`, "durable", true, false},
		{"nonce echo without result identity", `{"ideas":[]}`, "durable", true, false},
		{"commentary before proof", `{"ideas":[]}`, "durable", true, true},
		{"duplicate nonce", `{"ideas":[]}`, "durable", true, false},
		{"trailing commentary", `{"ideas":[]}`, "durable", true, false},
		{"quoted proof", `{"ideas":[]}`, "durable", true, false},
		{"nonterminal proof", `{"ideas":[]}`, "durable", true, false},
		{"truncated proof", `{"ideas":[]}`, "durable", true, false},
		{"null pre-state", `null`, "durable", true, false},
		{"missing pre-state array", `{}`, "durable", true, false},
		{"null pre-state array", `{"ideas":null}`, "durable", true, false},
		{"trailing JSON", `{"ideas":[]} {}`, "durable", true, false},
		{"trailing garbage", `{"ideas":[]} garbage`, "durable", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens := map[string]bool{}
			for invocation := range 2 {
				state := t.TempDir()
				logPath := filepath.Join(state, "jevonsd.log")
				if err := os.WriteFile(logPath, []byte("msg=\"agent started\" name=jevons provider=grok\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				var mu sync.Mutex
				var token string
				peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/ideas" {
						mu.Lock()
						defer mu.Unlock()
						if token == "" {
							_, _ = w.Write([]byte(tc.before))
							return
						}
						records := []journeyIdeaRecord{}
						if tc.effect != "absent" {
							records = append(records, journeyIdeaRecord{ID: "captured", Text: token, Source: "mcp"})
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"ideas": records})
						return
					}
					if r.URL.Path != "/ws/mux" {
						http.NotFound(w, r)
						return
					}
					conn, err := websocket.Accept(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.CloseNow()
					ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
					defer cancel()
					if _, _, err := conn.Read(ctx); err != nil {
						t.Error(err)
						return
					}
					if err := writeOwnerMux(ctx, conn, "meta", map[string]any{"n": 0, "lo": 1, "hi": 0, "following": true}); err != nil {
						t.Error(err)
						return
					}
					_, data, err := conn.Read(ctx)
					if err != nil {
						// A malformed pre-state must close before submitting.
						return
					}
					var send struct {
						Body struct{ Text string } `json:"body"`
					}
					if err := json.Unmarshal(data, &send); err != nil {
						t.Error(err)
						return
					}
					_, rest, ok := strings.Cut(send.Body.Text, " with text ")
					quoted, _, end := strings.Cut(rest, " and source mcp.")
					var fresh string
					if !ok || !end || json.Unmarshal([]byte(quoted), &fresh) != nil || !strings.HasPrefix(fresh, "journey-tool-effect-") {
						t.Errorf("missing fresh request token: %q", send.Body.Text)
						return
					}
					mu.Lock()
					if tokens[fresh] {
						t.Error("journey reused its request token")
					}
					tokens[fresh], token = true, fresh
					mu.Unlock()
					if tc.effect == "durable" || tc.effect == "different-id" {
						id := "captured"
						if tc.effect == "different-id" {
							id = "different"
						}
						data, _ := json.Marshal(map[string]any{"ideas": []journeyIdeaRecord{{ID: id, Text: fresh, Source: "mcp"}}})
						if err := os.WriteFile(filepath.Join(state, "ideas.json"), data, 0o600); err != nil {
							t.Error(err)
							return
						}
					}
					frames := [][]byte{ownerMuxFixture(t, ownerMuxChannel, "user", 1, send.Body.Text, "", "owner", "put")}
					if tc.call {
						call, _ := json.Marshal(map[string]any{
							"v": 1, "ch": ownerMuxChannel, "t": "frame",
							"body": map[string]any{"id": "e:2", "index": 2, "op": "put", "type": "tool_use",
								"event": map[string]any{"type": "tool_use", "name": "jevons_idea_capture", "input": map[string]string{"text": fresh, "source": "mcp"}}},
						})
						frames = append(frames, call)
					}
					reply := fresh + " captured"
					stop := "end_turn"
					switch tc.name {
					case "wrong returned identity":
						reply = fresh + " wrong"
					case "nonce echo without result identity":
						reply = fresh
					case "commentary before proof":
						reply = "I will capture the supplied text." + reply
					case "duplicate nonce":
						reply = fresh + ": " + reply
					case "trailing commentary":
						reply += " was requested"
					case "quoted proof":
						reply = "The proof would be \"" + reply + "\""
					case "nonterminal proof":
						stop = ""
					case "truncated proof":
						stop = "max_tokens"
					}
					frames = append(frames, ownerMuxFixture(t, ownerMuxChannel, "assistant", 3, reply, stop, "", "put"))
					for _, frame := range frames {
						if err := conn.Write(ctx, websocket.MessageText, frame); err != nil {
							t.Error(err)
							return
						}
					}
					if tc.wantOK || tc.name == "wrong returned identity" || tc.name == "quoted proof" || tc.name == "truncated proof" {
						_, _, _ = conn.Read(ctx)
					}
				}))
				s := &suite{stateDir: state, logPath: logPath, provider: claudia.ProviderGrok, host: strings.TrimPrefix(peer.URL, "http://")}
				err := s.jOverseerToolsAttached()
				peer.Close()
				if (err == nil) != tc.wantOK {
					t.Errorf("invocation %d: J6c error=%v, want success=%v", invocation, err, tc.wantOK)
				}
			}
		})
	}
}

func TestT625ToolEffectRequiresExactUniqueDurableRecord(t *testing.T) {
	const token = "fresh-idea"
	good := journeyIdeaRecord{ID: "idea-one", Text: token, Source: "mcp"}
	for _, tc := range []struct {
		name    string
		records []journeyIdeaRecord
		want    bool
	}{
		{"missing effect", nil, false},
		{"exact effect", []journeyIdeaRecord{good}, true},
		{"unrelated effect", []journeyIdeaRecord{{ID: "old", Text: "stale-idea", Source: "mcp"}}, false},
		{"missing identity", []journeyIdeaRecord{{Text: token, Source: "mcp"}}, false},
		{"HTTP effect", []journeyIdeaRecord{{ID: "idea-one", Text: token, Source: "api"}}, false},
		{"duplicate record", []journeyIdeaRecord{good, good}, false},
		{"two captures", []journeyIdeaRecord{good, {ID: "idea-two", Text: token, Source: "mcp"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := uniqueIdeaEffect(tc.records, token)
			if (err == nil) != tc.want {
				t.Fatalf("effect id=%q err=%v, want success=%v", id, err, tc.want)
			}
			if tc.want && id != good.ID {
				t.Fatalf("wrong effect identity %q", id)
			}
		})
	}
}
