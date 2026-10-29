// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// observeTargetLedgerOwnerBlockers applies T1042 tier-2/3 intake to a single
// bullseye target row after a ledger mutation (assign/unassign/apply).
func (s *Server) observeTargetLedgerOwnerBlockers(cwd, targetID string) {
	if s == nil || strings.TrimSpace(s.ownerQuestionsDir) == "" {
		return
	}
	targetID = strings.TrimSpace(strings.TrimPrefix(targetID, "🎯"))
	if targetID == "" {
		return
	}
	row, ok := targetfile.LoadGateRowFromCwd(cwd, targetID)
	s.observeTargetLedgerOwnerBlockersRow(cwd, targetID, row, ok)
}

func (s *Server) observeTargetLedgerOwnerBlockersRow(cwd, targetID string, row targetfile.GateRow, ok bool) {
	if s == nil || strings.TrimSpace(s.ownerQuestionsDir) == "" {
		return
	}
	targetID = strings.TrimSpace(strings.TrimPrefix(targetID, "🎯"))
	if targetID == "" || !ok {
		return
	}
	repo, err := ownerquestion.SharedRepo(cwd)
	if err != nil {
		slog.Warn("target ledger intake repo lookup failed", "target", targetID, "err", err)
		return
	}
	if s.hasOpenOwnerGateQuestion(repo, targetID) {
		s.mootTargetLedgerIntake(repo, targetID, "superseded by typed owner-gate question")
		return
	}
	q, review, ok, qerr := ownerquestion.FromTargetLedger(row, cwd)
	if qerr != nil {
		slog.Warn("target ledger intake rejected", "target", targetID, "err", qerr)
		return
	}
	if !ok {
		s.mootTargetLedgerIntake(repo, targetID, "ledger no longer owner-shaped")
		return
	}
	s.emitOwnerQuestionNotification(q, review)
}

func (s *Server) effectiveOwnerQuestionsDir() string {
	if dir := strings.TrimSpace(s.ownerQuestionsDir); dir != "" {
		return dir
	}
	return s.agentReportStateDir()
}

func (s *Server) emitOwnerQuestionNotification(q ownerquestion.Question, review *ownerquestion.ReviewEvent) {
	now := time.Now()
	shouldNotify := true
	if strings.TrimSpace(s.effectiveOwnerQuestionsDir()) == "" {
		shouldNotify = true
	} else if err := s.recordOwnerQuestionWithReview(q, review); err != nil {
		slog.Error("owner question view intake failed", "target", q.Identity.Target, "err", err)
		shouldNotify = false
	} else {
		open, err := ownerquestionview.New(s.effectiveOwnerQuestionsDir()).OpenVersion(ownerquestionview.Identity{Repo: q.Identity.Repo, Target: q.Identity.Target, ID: q.Identity.ID, Version: q.Identity.Version})
		if err != nil {
			slog.Error("owner question view read failed", "err", err)
		}
		shouldNotify = err == nil && open
	}
	if !shouldNotify {
		return
	}
	dir := s.ownerQuestionNotifyDir()
	if dir == "" {
		return
	}
	key := q.Identity.Repo + "#" + q.Identity.Target + "#" + q.Identity.ID
	if _, err := ownerquestions.New(dir).Observe(ownerquestions.Question{Key: key, Text: q.NoticeBody(), Link: q.Link}, now); err != nil {
		slog.Error("owner question spool failed (retry retained)", "key", key, "err", err)
	}
}

func (s *Server) ownerQuestionNotifyDir() string {
	if dir := s.agentReportStateDir(); dir != "" {
		return dir
	}
	return s.ownerQuestionsDir
}

func (s *Server) hasOpenOwnerGateQuestion(repo, target string) bool {
	rows, err := ownerquestionview.New(s.ownerQuestionsDir).List(true)
	if err != nil {
		return false
	}
	for _, r := range rows {
		if r.Identity.Repo == repo && r.Identity.Target == target && r.Identity.ID == "owner-gate" {
			return true
		}
	}
	return false
}

// scanLedgerOwnerBlockers reads the repo ledger once and applies T1042 intake
// for every target row. Frontier-consume passes the sweep snapshot via
// scanLedgerOwnerBlockersSnapshot (🎯T1152) so hand-edited bullseye
// assignments are not silent when no finish-report arrives.
func (s *Server) scanLedgerOwnerBlockers(cwd string) {
	data, _, err := targetfile.ReadLedgerFromCwd(cwd)
	if err != nil {
		return
	}
	snap, ok := targetfile.NewLedgerSnapshot(data)
	if !ok {
		return
	}
	s.scanLedgerOwnerBlockersSnapshot(cwd, snap)
}

func (s *Server) scanLedgerOwnerBlockersSnapshot(cwd string, snap *targetfile.LedgerSnapshot) {
	if s == nil || strings.TrimSpace(s.ownerQuestionsDir) == "" || snap == nil {
		return
	}
	for _, id := range snap.TargetIDs() {
		row, ok := snap.GateRow(id)
		s.observeTargetLedgerOwnerBlockersRow(cwd, id, row, ok)
	}
}

func (s *Server) mootTargetLedgerIntake(repo, target, note string) {
	st, err := s.ownerQuestionStore()
	if err != nil {
		return
	}
	rows, err := st.List(true)
	if err != nil {
		return
	}
	for _, r := range rows {
		if r.Identity.Repo != repo || r.Identity.Target != target || r.Asker != ownerquestion.TargetLedgerAsker {
			continue
		}
		if r.Identity.ID != "hardware-visual-review" && r.Identity.ID != "explicit-owner-blocker" {
			continue
		}
		if err := st.Resolve(r.Identity, ownerquestionview.Moot, note); err != nil {
			slog.Error("target ledger review close failed", "err", err)
			continue
		}
		if err := ownerquestions.New(s.ownerQuestionNotifyDir()).Resolve(r.Identity.Repo + "#" + r.Identity.Target + "#" + r.Identity.ID); err != nil {
			slog.Error("target ledger notification close failed", "err", err)
		}
	}
}
