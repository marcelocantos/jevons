// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
