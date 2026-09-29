// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const ownerMuxChannel = "transcript:" + overseerName

type ownerMuxEnvelope struct {
	Version int             `json:"v"`
	Channel string          `json:"ch"`
	Type    string          `json:"t"`
	Body    json.RawMessage `json:"body"`
}

type ownerMuxFrame struct {
	ID    string `json:"id"`
	Index int    `json:"index"`
	Op    string `json:"op"`
	Type  string `json:"type"`
	Event struct {
		Type    string         `json:"type"`
		Origin  string         `json:"turn_origin"`
		Name    string         `json:"name"`
		Input   map[string]any `json:"input"`
		Message struct {
			Content any    `json:"content"`
			Stop    string `json:"stop_reason"`
		} `json:"message"`
	} `json:"event"`
}

func dialOwnerMux(ctx context.Context, host string) (*websocket.Conn, <-chan []byte, error) {
	conn, frames, err := dialJourneySocket(ctx, "ws://"+host+"/ws/mux")
	if err != nil {
		return nil, nil, err
	}
	// Match useConversation's initial request, including its bounded window.
	if err := writeOwnerMux(ctx, conn, "open", map[string]int{"lo": -30, "hi": 0}); err != nil {
		conn.CloseNow()
		return nil, nil, err
	}
	return conn, frames, nil
}

func writeOwnerMux(ctx context.Context, conn *websocket.Conn, kind string, body any) error {
	data, err := json.Marshal(map[string]any{"v": 1, "ch": ownerMuxChannel, "t": kind, "body": body})
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

// Decode the public mux contract independently of the server normalizer and
// React reducer. A frame's event is a FULL coalesced snapshot even for append;
// its optional text delta must never be concatenated with that snapshot.
func decodeOwnerMux(data []byte) (ownerMuxEnvelope, ownerMuxFrame, error) {
	var env ownerMuxEnvelope
	var frame ownerMuxFrame
	if err := json.Unmarshal(data, &env); err != nil {
		return env, frame, fmt.Errorf("invalid mux envelope: %w", err)
	}
	if env.Channel != ownerMuxChannel {
		if env.Type == "error" && env.Channel == "" {
			return env, frame, fmt.Errorf("mux connection error: %s", env.Body)
		}
		return env, frame, nil
	}
	if env.Version != 1 {
		return env, frame, fmt.Errorf("unsupported mux version %d", env.Version)
	}
	if env.Type == "error" || env.Type == "reset" {
		return env, frame, fmt.Errorf("owner mux %s: %s", env.Type, env.Body)
	}
	if env.Type != "frame" {
		return env, frame, nil
	}
	if err := json.Unmarshal(env.Body, &frame); err != nil {
		return env, frame, err
	}
	if frame.ID == "" || frame.Index < 1 || (frame.Op != "put" && frame.Op != "append") || frame.Type == "" {
		return env, frame, fmt.Errorf("mux frame lacks valid identity, index, operation or type")
	}
	if frame.Event.Type != "" && frame.Event.Type != frame.Type {
		return env, frame, fmt.Errorf("mux frame and event types disagree")
	}
	return env, frame, nil
}

// The server ends an open window with complete window metadata. Partial live
// status metas can interleave with replay and are not that boundary. Quiet time
// is not evidence of completion; later live replies cannot repair missing replay.
func collectOwnerMuxReplay(ctx context.Context, frames <-chan []byte) ([][]byte, error) {
	var replay [][]byte
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return nil, fmt.Errorf("mux closed before replay meta")
			}
			env, _, err := decodeOwnerMux(data)
			if err != nil {
				return nil, err
			}
			if env.Channel != ownerMuxChannel {
				continue
			}
			if env.Type == "meta" {
				var window struct {
					N         *int  `json:"n"`
					Lo        *int  `json:"lo"`
					Hi        *int  `json:"hi"`
					Following *bool `json:"following"`
				}
				if err := json.Unmarshal(env.Body, &window); err != nil {
					return nil, fmt.Errorf("invalid mux window meta: %w", err)
				}
				if window.N == nil && window.Lo == nil && window.Hi == nil && window.Following == nil {
					continue
				}
				if window.N == nil || window.Lo == nil || window.Hi == nil || window.Following == nil ||
					*window.N < 0 || *window.Lo < 1 || *window.Lo > *window.N+1 || *window.Hi != 0 || !*window.Following {
					return nil, fmt.Errorf("invalid mux tail-window bounds")
				}
				return replay, nil
			}
			if env.Type == "frame" {
				replay = append(replay, data)
				if len(replay) > maxReplayFrames {
					return nil, fmt.Errorf("mux replay exceeded %d frames", maxReplayFrames)
				}
			}
		case <-ctx.Done():
			return nil, fmt.Errorf("mux replay: %w", ctx.Err())
		}
	}
}

func waitOwnerMuxReply(ctx context.Context, frames <-chan []byte, prompt, expected string) error {
	return waitOwnerMuxReplyObserved(ctx, frames, prompt, expected, nil)
}

// An observer sees only validated frames after this request's owner echo.
// This lets tool-effect journeys retain the same strict reply correlation.
func waitOwnerMuxReplyObserved(ctx context.Context, frames <-chan []byte, prompt, expected string, observe func(ownerMuxFrame)) error {
	return waitOwnerMuxReplyMatching(ctx, frames, prompt, expected, func(text string) bool { return text == expected }, observe)
}

// Tool-effect replies include an identity minted by the handler, so their
// complete text cannot be known in advance. The caller validates that identity
// against the independently observed effect after this request ends.
func waitOwnerMuxReplyMatching(ctx context.Context, frames <-chan []byte, prompt, expected string, accept func(string) bool, observe func(ownerMuxFrame)) error {
	ownerIndex := 0
	ended := make(map[string]bool)
	indices := make(map[string]int)
	ids := make(map[int]string)
	beforeOwner := make(map[string]bool)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return fmt.Errorf("mux closed without exact completed reply (owner index=%d)", ownerIndex)
			}
			env, frame, err := decodeOwnerMux(data)
			if err != nil {
				return err
			}
			if env.Channel != ownerMuxChannel || env.Type != "frame" {
				continue
			}
			if index, seen := indices[frame.ID]; seen && index != frame.Index {
				return fmt.Errorf("mux row %s changed index", frame.ID)
			}
			if id, seen := ids[frame.Index]; seen && id != frame.ID {
				return fmt.Errorf("mux index %d changed identity", frame.Index)
			}
			indices[frame.ID] = frame.Index
			ids[frame.Index] = frame.ID
			if ownerIndex == 0 && frame.Type != "user" {
				beforeOwner[frame.ID] = true
			}
			text := journeyContentText(frame.Event.Message.Content)
			if frame.Type == "user" && (frame.Event.Origin == "owner" || frame.Event.Origin == "") && text == prompt {
				ownerIndex = frame.Index
			}
			if observe != nil && ownerIndex > 0 && frame.Index > ownerIndex && !beforeOwner[frame.ID] {
				observe(frame)
			}
			if frame.Type != "assistant" || ownerIndex == 0 || frame.Index <= ownerIndex || ended[frame.ID] || beforeOwner[frame.ID] {
				continue
			}
			stop := frame.Event.Message.Stop
			if stop != "end_turn" && stop != "stop_sequence" && stop != "max_tokens" {
				continue
			}
			ended[frame.ID] = true
			if outage := replyOutage("owner mux reply", text); outage != nil {
				return outage
			}
			if stop == "max_tokens" {
				if strings.Contains(text, expected) {
					return fmt.Errorf("requested mux reply was truncated")
				}
				continue
			}
			if accept(strings.TrimSpace(text)) {
				return nil
			}
		case <-ctx.Done():
			return fmt.Errorf("owner mux reply (owner index=%d): %w", ownerIndex, ctx.Err())
		}
	}
}

func assertOwnerMuxReplay(replay [][]byte, prompt, reply string) error {
	frames := make(chan []byte, len(replay))
	for _, data := range replay {
		frames <- data
	}
	close(frames)
	return waitOwnerMuxReply(context.Background(), frames, prompt, reply)
}

// ownerMuxPhase reads the overseer turn-state sample the canonical socket
// publishes alongside transcript frames (🎯T555.2 fanMeta). A window meta
// carries no phase and reports false rather than an empty phase, so a
// replay boundary can never be read as "idle".
func ownerMuxPhase(body json.RawMessage) (string, bool) {
	var meta struct {
		Phase *struct {
			Phase string `json:"phase"`
		} `json:"phase"`
	}
	if err := json.Unmarshal(body, &meta); err != nil || meta.Phase == nil {
		return "", false
	}
	return meta.Phase.Phase, true
}

// waitOwnerMuxTurnWorking establishes that THIS request is the one in
// flight, and returns its owner echo index.
//
// 🎯T625: waiting for any assistant activity accepts a previous journey's
// turn still streaming, so a cancel journey could interrupt — and then
// cancel-settle — a turn it never sent. The owner echo's index bounds the
// answer: only an assistant row after that index belongs to this request.
// The phase sample must also say the overseer is working, so the interrupt
// lands on a live turn rather than on an idle seat.
//
// A request that has already reached a terminal is refused rather than
// cancelled: interrupting a finished turn proves nothing about cancel
// ordering. The residual race — the turn ending between this observation
// and the interrupt arriving — is real and declared, not hidden.
func waitOwnerMuxTurnWorking(ctx context.Context, frames <-chan []byte, prompt string, d time.Duration) (int, error) {
	deadline := time.After(d)
	ownerIndex, working, started := 0, false, false
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return 0, fmt.Errorf("mux closed before the request to cancel began working")
			}
			env, frame, err := decodeOwnerMux(data)
			if err != nil {
				return 0, err
			}
			if env.Channel != ownerMuxChannel {
				continue
			}
			if env.Type == "meta" {
				if phase, ok := ownerMuxPhase(env.Body); ok {
					working = phase != "" && phase != "idle" && phase != "error"
				}
				// The phase sample and the first streamed row race; either
				// order completes the observation.
				if started && working {
					return ownerIndex, nil
				}
				continue
			}
			if env.Type != "frame" {
				continue
			}
			if frame.Type == "user" && (frame.Event.Origin == "owner" || frame.Event.Origin == "") &&
				journeyContentText(frame.Event.Message.Content) == prompt {
				ownerIndex = frame.Index
			}
			if frame.Type != "assistant" || ownerIndex == 0 || frame.Index <= ownerIndex {
				continue
			}
			switch frame.Event.Message.Stop {
			case "end_turn", "stop_sequence", "max_tokens":
				return 0, fmt.Errorf("the request to cancel ended before it could be interrupted")
			}
			started = true
			if working {
				return ownerIndex, nil
			}
		case <-deadline:
			return 0, fmt.Errorf("request to cancel never worked (echo=%d streaming=%v working=%v)",
				ownerIndex, started, working)
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

// cancelHoldSeconds bounds J3's held tool call. It outlasts the cancel's
// settle deadline, so a turn that reaches idle while the hold is unreleased
// was ended by the cancel, not by the tool returning (🎯T840).
const cancelHoldSeconds = 150

// cancelHoldCommand is the shell hold J3's long turn runs: it announces
// itself on ready, waits for release (which the journey writes only after
// the cancel has settled), and records completed only if it returned on
// its own.
func cancelHoldCommand(ready, release, completed, nonce string) string {
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	return fmt.Sprintf("printf '%%s\\n' %s > %s; n=0; while [ ! -f %s ] && [ \"$n\" -lt %d ]; do sleep 1; n=$((n+1)); done; printf '%%s\\n' %s > %s",
		quote(nonce), quote(ready), quote(release), cancelHoldSeconds, quote(nonce), quote(completed))
}

// waitOwnerMuxHold waits until the held tool call is observably running —
// its ready marker carries this request's nonce — while refusing a turn
// that ended first: an assistant terminal after this request's echo means
// the provider answered without the hold, and there is nothing left to
// cancel (🎯T840).
func waitOwnerMuxHold(ctx context.Context, frames <-chan []byte, ownerIndex int, ready, nonce string, d time.Duration) error {
	deadline := time.After(d)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if body, err := os.ReadFile(ready); err == nil && strings.TrimSpace(string(body)) == nonce {
			return nil
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		select {
		case data, ok := <-frames:
			if !ok {
				return fmt.Errorf("mux closed before the held tool call started")
			}
			env, frame, err := decodeOwnerMux(data)
			if err != nil {
				return err
			}
			if env.Channel != ownerMuxChannel || env.Type != "frame" ||
				frame.Type != "assistant" || frame.Index <= ownerIndex {
				continue
			}
			switch frame.Event.Message.Stop {
			case "end_turn", "stop_sequence", "max_tokens":
				return fmt.Errorf("the request to cancel ended before its held tool call started")
			}
		case <-tick.C:
		case <-deadline:
			return fmt.Errorf("held tool call never started (no ready marker at %s)", ready)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// waitOwnerMuxSettled waits for the owner's cancel to settle on the
// canonical socket — the same level the React cockpit reduces. Asserting
// the settle on /ws/chat would prove a signal the owner's actual UI never
// sees (🎯T540). Reaching idle is a real transition here because the
// caller has already observed this request working.
func waitOwnerMuxSettled(ctx context.Context, frames <-chan []byte, d time.Duration) error {
	deadline := time.After(d)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return fmt.Errorf("mux closed before the cancel settled")
			}
			env, _, err := decodeOwnerMux(data)
			if err != nil {
				return err
			}
			if env.Channel != ownerMuxChannel || env.Type != "meta" {
				continue
			}
			if phase, ok := ownerMuxPhase(env.Body); ok && (phase == "idle" || phase == "") {
				return nil
			}
		case <-deadline:
			return fmt.Errorf("cancel never settled on the canonical owner level")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Boot-sweep bounds for J3 (🎯T840). The post-boot sweep hands the overseer
// its restart event about 12s after boot; the turn it starts can run for
// minutes under load, and the quiet window must outlast the gap between the
// daemon logging the hand-off and the phase sample saying the turn began.
const (
	bootSweepDeadline = 3 * time.Minute
	bootSweepQuiet    = 5 * time.Second
)

// bootSweepLineRE matches the daemon's own record that its post-boot sweep
// is finished with the overseer: the restart event was handed over (or
// failed to be), or a broker holds the fleet and no event is sent. The
// target is anchored so a jevons-po line does not answer for jevons.
var bootSweepLineRE = regexp.MustCompile(
	`msg="daemon restart resume event deliver(?:ed| failed)"[^\n]* target=` +
		regexp.QuoteMeta(overseerName) + `(?:\s|$)` +
		`|msg="claudia daemon holds fleet; reclaimed seats stay silent"`)

// bootSweepHandled reports whether the daemon log records that the post-boot
// sweep is done with the overseer.
func bootSweepHandled(logs []byte) bool {
	return bootSweepLineRE.Match(logs)
}

// waitBootSweepQuiet holds J3 until the post-boot sweep has reached the
// overseer and the turn it started is over (🎯T840). Run in isolation, J3's
// held turn otherwise overlaps the sweep: the restart event queues inside
// the provider behind the held turn, the owner's cancel lets it run as the
// next turn, and the owner's replacement waits behind it (🎯T915 owns that
// product defect). J3 asserts cancel ordering for an owner turn, so it
// starts from the same quiescent overseer the full suite reaches by the
// time J3 runs.
//
// A fresh subscription carries no phase sample until the phase changes, so
// the overseer counts as idle until a sample says otherwise, and the
// overseer must also have been silent for the quiet window: no phase
// sample and no transcript frame.
func waitBootSweepQuiet(ctx context.Context, frames <-chan []byte, logPath string, quiet, d time.Duration) error {
	deadline := time.After(d)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	phase, lastActivity := "idle", time.Now()
	var lastLevel json.RawMessage // the most recent phase-bearing meta, for the failure
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return fmt.Errorf("mux closed before the post-boot sweep settled")
			}
			env, _, err := decodeOwnerMux(data)
			if err != nil {
				return err
			}
			if env.Channel != ownerMuxChannel {
				continue
			}
			if env.Type == "meta" {
				p, ok := ownerMuxPhase(env.Body)
				if !ok {
					continue
				}
				lastLevel = env.Body
				// The cockpit converge loop republishes the level every few
				// seconds (4da3b8ae never saw 5s of quiet on an idle seat);
				// an unchanged idle sample is not a sign of life.
				wasIdle := phase == "idle" || phase == ""
				nowIdle := p == "idle" || p == ""
				phase = p
				if wasIdle && nowIdle {
					continue
				}
			}
			lastActivity = time.Now()
		case <-tick.C:
			idle := phase == "idle" || phase == ""
			if !idle || time.Since(lastActivity) < quiet {
				continue
			}
			logs, err := os.ReadFile(logPath)
			if err != nil {
				return err
			}
			if bootSweepHandled(logs) {
				return nil
			}
		case <-deadline:
			logs, _ := os.ReadFile(logPath)
			return fmt.Errorf("post-boot sweep never settled (sweep logged=%v phase=%q quiet for %s level=%s)",
				bootSweepHandled(logs), phase, time.Since(lastActivity).Round(time.Second), trim(string(lastLevel), 400))
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
