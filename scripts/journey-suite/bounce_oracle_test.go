// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

type bouncePeerState struct {
	Starts                int
	ID, Secret, Ack, Post string
	Removed               bool
}

// This peer is an adversarial test of the whole J14 oracle, not a product
// journey. It survives an actual subprocess restart and can selectively lie
// about a reply, registry row, launch or shutdown. No provider is launched.
func runBounceOraclePeer() error {
	flags := flag.NewFlagSet("bounce-peer", flag.ContinueOnError)
	port := flags.Int("port", 0, "")
	stateDir := flags.String("workdir", "", "")
	flags.String("bind", "", "")
	flags.String("config", "", "")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	mode := os.Getenv("JOURNEY_BOUNCE_FAULT")
	statePath := filepath.Join(*stateDir, "peer.json")
	var state bouncePeerState
	if data, err := os.ReadFile(statePath); err == nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	state.Starts++
	save := func() error {
		data, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return os.WriteFile(statePath, data, 0o600)
	}
	if err := save(); err != nil {
		return err
	}
	registry, err := claudia.NewRegistry(filepath.Join(*stateDir, "agents.json"))
	if err != nil {
		return err
	}
	register := func(id, session string, provider claudia.Provider) error {
		return registry.Register(claudia.AgentDef{Name: id, SessionID: session,
			Provider: provider, WorkDir: *stateDir, Materialized: true})
	}
	if err := register(overseerName, "overseer-session", claudia.ProviderClaude); err != nil {
		return err
	}
	if state.ID != "" {
		provider := claudia.ProviderGrok
		if mode == "wrong provider" {
			provider = claudia.ProviderClaude
		}
		if mode != "missing replacement launch" {
			fmt.Printf("msg=\"agent started\" name=%s provider=%s\n", state.ID, provider)
		}
		if mode == "new handover" {
			dir := filepath.Join(*stateDir, "handover")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, state.ID+".json"), []byte(`{"seed":"unexpected"}`), 0o600); err != nil {
				return err
			}
		}
	}
	var mu sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/agents" {
			agents := []AgentInfo{{Name: overseerName, Status: "running"}}
			if state.ID != "" && (!state.Removed || mode == "registry survives cleanup") {
				agents = append(agents, AgentInfo{Name: state.ID, Status: "running"})
			}
			_ = json.NewEncoder(w).Encode(agents)
			return
		}
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			Params struct {
				Name string            `json:"name"`
				Args map[string]string `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var reply string
		var callErr error
		switch request.Params.Name {
		case "jevons_thread_spawn":
			state.ID = request.Params.Args["id"]
			if state.ID == "" || request.Params.Args["provider"] != "grok" {
				callErr = fmt.Errorf("fixture did not select a fresh Grok seat")
				break
			}
			callErr = register(state.ID, "aside-session", claudia.ProviderGrok)
			fmt.Printf("msg=\"agent started\" name=%s provider=grok\n", state.ID)
			reply = "Spawned thread " + state.ID + " session aside-session"
		case "jevons_thread_direct":
			prompt := request.Params.Args["text"]
			if request.Params.Args["id"] != state.ID {
				callErr = fmt.Errorf("direct targeted another seat")
				break
			}
			if state.Starts == 1 {
				prefix := "Remember this journey continuity secret for my next question: "
				secret, ack, ok := strings.Cut(strings.TrimPrefix(prompt, prefix), ". Reply with exactly: ")
				if !strings.HasPrefix(prompt, prefix) || !ok || secret == "" || ack == "" {
					callErr = fmt.Errorf("missing pre-restart secret/ack")
					break
				}
				state.Secret, state.Ack = secret, ack
				reply = ack
			} else {
				_, rest, ok := strings.Cut(prompt, "the saved secret, then ")
				challenge, _, end := strings.Cut(rest, ". No labels or punctuation.")
				if !ok || !end || challenge == "" || strings.Contains(prompt, state.Secret) {
					callErr = fmt.Errorf("post-restart request leaked its expected memory or lacks a fresh challenge")
					break
				}
				state.Post = challenge
				reply = state.Secret + " " + challenge
				switch mode {
				case "unreadable registry":
					registryPath := filepath.Join(*stateDir, "agents.json")
					callErr = os.Rename(registryPath, registryPath+".saved")
					if callErr == nil {
						callErr = os.Mkdir(registryPath, 0o700)
					}
				case "session rotation then outage":
					callErr = register(state.ID, "rotated-before-timeout", claudia.ProviderGrok)
					if callErr == nil {
						_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]string{"message": "jevons_thread_direct timed out or cancelled after 30s"}})
						return
					}
				case "no post reply":
					reply = ""
				case "unrelated reply":
					reply = "I am running"
				case "lost retained fact":
					reply = "unknown " + challenge
				case "stale pre-restart reply":
					reply = state.Ack
				case "truncated reply":
					reply = reply[:len(reply)/2]
				case "late session rotation":
					callErr = register(state.ID, "rotated-during-answer", claudia.ProviderGrok)
				case "late provider rotation":
					callErr = register(state.ID, "aside-session", claudia.ProviderClaude)
				}
			}
		case "jevons_thread_remove":
			state.Removed = true
		case "jevons_thread_list":
			if !state.Removed {
				reply = state.ID
			}
		default:
			callErr = fmt.Errorf("unexpected tool %s", request.Params.Name)
		}
		if err := save(); err != nil {
			callErr = err
		}
		if callErr != nil {
			http.Error(w, callErr.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1,
			"result": map[string]any{"content": []map[string]string{{"type": "text", "text": reply}}}})
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	<-ctx.Done()
	if mode == "forced stop" && state.Starts == 1 {
		select {}
	}
	if mode == "upgrade stop" {
		fmt.Println(`msg="shutting down" exit_mode=upgrade stop_agents=false`)
	} else {
		fmt.Println(`msg="shutting down" exit_mode=normal stop_agents=true`)
	}
	_ = server.Close()
	<-done
	if mode == "bad exit" {
		return fmt.Errorf("fixture abnormal shutdown")
	}
	return nil
}

func TestT625BounceJourneyRejectsFalseGreens(t *testing.T) {
	identities := map[string]bool{}
	for _, mode := range []string{"valid", "valid again", "no post reply", "unrelated reply", "lost retained fact",
		"stale pre-restart reply", "truncated reply", "wrong provider", "missing replacement launch",
		"late session rotation", "late provider rotation", "session rotation then outage", "unreadable registry", "new handover", "registry survives cleanup",
		"bad exit", "upgrade stop", "forced stop"} {
		t.Run(mode, func(t *testing.T) {
			state := t.TempDir()
			port, err := freePort()
			if err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(state, "daemon.log")
			logFile, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			defer logFile.Close()
			s := &suite{host: fmt.Sprintf("127.0.0.1:%d", port), port: port, stateDir: state,
				workdir: state, provider: claudia.ProviderGrok, daemonBin: os.Args[0], logPath: logPath,
				logFile: logFile, daemonEnv: []string{childEnv + "=bounce-peer", "JOURNEY_BOUNCE_FAULT=" + mode}}
			if err := s.startDaemon(); err != nil {
				t.Fatal(err)
			}
			// Race-instrumented subprocesses pause for a second at exit. Give
			// fixture teardown the same drain budget as the journey itself.
			defer s.signalStop(8 * time.Second)
			err = s.jBounceResume()
			wantOK := strings.HasPrefix(mode, "valid")
			if (err == nil) != wantOK {
				t.Errorf("J14 error=%v, want success=%v", err, wantOK)
			}
			if mode == "session rotation then outage" && isOutage(err) {
				t.Errorf("observed session drift misclassified as outage: %v", err)
			}
			if !wantOK && err != nil {
				wantFailure := "post-bounce aside turn"
				switch mode {
				case "wrong provider", "missing replacement launch":
					wantFailure = "replacement aside"
				case "late session rotation", "late provider rotation", "session rotation then outage":
					wantFailure = "changed existing session/provider"
				case "unreadable registry":
					wantFailure = "snapshot after bounce"
				case "new handover":
					wantFailure = "new T285 handover"
				case "registry survives cleanup":
					wantFailure = "remains in agent registry"
				case "bad exit":
					wantFailure = "normal drain failed: daemon exited"
				case "upgrade stop":
					wantFailure = "drain lacks normal"
				case "forced stop":
					wantFailure = "normal drain failed: daemon did not exit"
				}
				if !strings.Contains(err.Error(), wantFailure) {
					t.Errorf("wrong rejection: %v, want %q", err, wantFailure)
				}
			}
			if err := s.signalStop(8 * time.Second); err != nil && wantOK {
				t.Error(err)
			}
			data, err := os.ReadFile(filepath.Join(state, "peer.json"))
			if err != nil {
				t.Fatal(err)
			}
			var observed bouncePeerState
			if err := json.Unmarshal(data, &observed); err != nil {
				t.Fatal(err)
			}
			if wantOK && (observed.Starts != 2 || observed.Secret == "" || observed.Post == "" || !observed.Removed) {
				t.Fatalf("journey omitted an actual phase: %+v", observed)
			}
			for _, id := range []string{observed.ID, observed.Secret, observed.Ack, observed.Post} {
				if id == "" {
					continue
				}
				if identities[id] {
					t.Errorf("journey reused identity %q", id)
				}
				identities[id] = true
			}
		})
	}
}
