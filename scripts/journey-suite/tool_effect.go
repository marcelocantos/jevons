// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Independent readers of the public idea API and durable record format. The
// harness never calls capture or constructs the effect on the agent's behalf.
type journeyIdeaRecord struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Source string `json:"source"`
}

func (s *suite) journeyIdeas(ctx context.Context) ([]journeyIdeaRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.host+"/api/ideas", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("idea API status %d", resp.StatusCode)
	}
	var body struct {
		Ideas []journeyIdeaRecord `json:"ideas"`
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&body); err != nil {
		return nil, err
	}
	if body.Ideas == nil {
		return nil, fmt.Errorf("idea API response must contain an ideas array")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("idea API response has trailing content: %v", err)
	}
	return body.Ideas, nil
}

func matchingIdeaToolCall(frame ownerMuxFrame, token string) bool {
	const tool = "jevons_idea_capture"
	if frame.Type != "tool_use" {
		return false
	}
	input := frame.Event.Input
	switch frame.Event.Name {
	case tool, "mcp__" + mcpName + "__" + tool:
	case mcpName + "__" + tool:
		// Grok's discovered-tool invocation retains the dispatch envelope in
		// canonical mux events. Match both names and its actual tool arguments.
		if input["tool_name"] != frame.Event.Name {
			return false
		}
		var ok bool
		input, ok = input["tool_input"].(map[string]any)
		if !ok {
			return false
		}
	case mcpName + ": " + tool:
		// Cursor preserves its MCP dispatch arguments in the canonical event.
		if input["providerIdentifier"] != mcpName || input["toolName"] != tool {
			return false
		}
		var ok bool
		input, ok = input["args"].(map[string]any)
		if !ok {
			return false
		}
	default:
		return false
	}
	return input["text"] == token && input["source"] == "mcp"
}

func uniqueIdeaEffect(records []journeyIdeaRecord, token string) (string, error) {
	id := ""
	for _, record := range records {
		if record.Text != token {
			continue
		}
		if id != "" {
			return "", fmt.Errorf("duplicate matching capture")
		}
		if record.ID == "" || record.Source != "mcp" {
			return "", fmt.Errorf("matching capture lacks identity or MCP source")
		}
		id = record.ID
	}
	if id == "" {
		return "", fmt.Errorf("no matching capture")
	}
	return id, nil
}
