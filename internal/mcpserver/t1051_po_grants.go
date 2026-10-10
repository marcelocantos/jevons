// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/landinggrant"
	"github.com/marcelocantos/jevons/internal/worktree"
)

// SetLandingGrantStore supplies daemon-owned durable grant/epoch state. Nil
// (the default) makes every purported approval inert. The caller must wire
// SnapshotEpoch to T1050's SAME landingauth.Store used at redemption.
func (s *Server) SetLandingGrantStore(store *landinggrant.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.landingGrants = store
}

// observePOApproval only runs at a completed terminal event on the process
// object subscribed by the daemon, not on MCP calls, worker reports or a
// claimed actor header. A stale predecessor's terminal event cannot mint.
func (s *Server) observePOApproval(name string, proc *claudia.Agent, ev claudia.Event, text string) error {
	if !strings.HasPrefix(text, "```jevons-integration-approval\n") {
		return nil
	}
	if proc == nil || s.registry == nil || name != "jevons-po" || ev.Type != "assistant" || ev.StopReason != "end_turn" || ev.IsError || ev.SessionID == "" || ev.TurnID == "" {
		return errors.New("approval: no completed current PO-process turn witness")
	}
	s.mu.Lock()
	configured := s.landingGrants
	s.mu.Unlock()
	if configured == nil {
		return worktree.ErrNoAuthority
	}
	approval, err := landinggrant.ParseApproval(text)
	if err != nil {
		return err
	}
	witness := ev.SessionID + "/" + ev.TurnID
	// A copy avoids changing the global verifier while simultaneous PO turns
	// or a registry rotation are observed. The verifier runs again inside Issue
	// immediately before the epoch-locked durable mint.
	grantStore := *configured
	grantStore.VerifyPOEvent = func(id string, _ worktree.BatchReview) error {
		def := s.registry.Def(name)
		if id != witness || def == nil || def.Role != "product-owner" || def.SessionID != ev.SessionID || s.lookupAgentProc(name) != proc { // review tuple is checked by Issue itself
			return errors.New("approval: emitter is not the current registered PO process")
		}
		return nil
	}
	grant, err := grantStore.MintApproval(approval, witness)
	if err != nil {
		return err
	}
	s.notify(name, fmt.Sprintf("PO integration grant %s minted for %s %s, expiring %s; exact reviewed worker tips and base are bound. Redemption transport is not yet installed.", grant.ID, approval.Review.Repo, approval.Review.Target, grant.Expires.Format("2006-01-02T15:04:05Z07:00")))
	return nil
}
