// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !jevons_claudia_next

package claudiawire

import (
	"fmt"
	"strings"
)

const (
	GenerationPageLimit     = 128
	generationCursorLimit   = 96
	generationIdentityLimit = 1024
	generationIssueLimit    = 512
	DiscoveryCursorExpired  = "discovery_cursor_expired"
	DiscoveryBusy           = "discovery_busy"
)

// GenerationDiscoveryError is a typed discovery refusal. Copied from Claudia
// omp at 2b10c121.
type GenerationDiscoveryError struct{ Code, Message string }

func (e *GenerationDiscoveryError) Error() string { return e.Code + ": " + e.Message }

type GenerationIdentity struct {
	Seat      string `json:"seat"`
	SessionID string `json:"session_id"`
}

type GenerationIssue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// GenerationPage is a weakly consistent directory traversal.
type GenerationPage struct {
	Generations []GenerationIdentity `json:"generations"`
	Issues      []GenerationIssue    `json:"issues"`
	Cursor      string               `json:"cursor"`
	More        bool                 `json:"more"`
	Complete    bool                 `json:"complete"`
}

func (p *GenerationPage) Validate(cursor string, limit int) error {
	if p == nil || limit < 1 || limit > GenerationPageLimit || len(cursor) > generationCursorLimit || p.Generations == nil || p.Issues == nil || len(p.Generations)+len(p.Issues) > limit || len(p.Cursor) > generationCursorLimit || p.More != (p.Cursor != "") || (p.More && (p.Cursor == cursor || p.Complete)) || (p.Complete && len(p.Issues) > 0) {
		return fmt.Errorf("invalid generation discovery page")
	}
	for _, g := range p.Generations {
		if strings.TrimSpace(g.Seat) == "" || strings.TrimSpace(g.SessionID) == "" || len(g.Seat) > generationIdentityLimit || len(g.SessionID) > generationIdentityLimit {
			return fmt.Errorf("invalid generation identity")
		}
	}
	for _, i := range p.Issues {
		if i.Code == "" || len(i.Code) > generationIssueLimit || len(i.Path) > generationIssueLimit || len(i.Message) > generationIssueLimit {
			return fmt.Errorf("invalid generation issue")
		}
	}
	return nil
}

// GenerationQuery starts with an empty Cursor.
type GenerationQuery struct {
	Cursor string
	Limit  int
}
