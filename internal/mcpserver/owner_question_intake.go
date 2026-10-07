// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/mark3labs/mcp-go/mcp"
)

// recordedGateQuestion returns the typed owner-question intake from a
// successful owner-gate record. Errors leave the existing gate response intact
// but surface the failure: consumers must not infer intake from ledger prose.
func recordedGateQuestion(cwd, target, question, by string, result *mcp.CallToolResult) (*mcp.CallToolResult, error) {
	if result == nil || result.IsError {
		return result, nil
	}
	if strings.TrimSpace(by) == "" {
		by = "unknown"
	}
	q, err := ownerquestion.FromOwnerGate(cwd, target, question, by)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("owner gate recorded but question intake failed: %v", err)), nil
	}
	data, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	result.Content = append(result.Content, mcp.TextContent{Type: "text", Text: "owner-question intake: " + string(data)})
	return result, nil
}
