// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/sendq"
	"github.com/marcelocantos/jevons/internal/statedb"
	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

const (
	t952Hold      = "t952-hold"
	t952Stall     = "t952-stall"
	t952HoldText  = "t952-hold preserve this parked obligation"
	t952StallText = "t952-stall integrate the worker reports"
)

// jT952Hold is the bounded isolate for 🎯T952. Twenty simulated minutes and
// one process restart must leave the parked obligation queued and unsubmitted.
// The working stall's alert is the open notice plus, while that queue is still
// pending at the end, the 15-minute band. A live overseer that starts, drains,
// kills, or marks the stall undelivered ends the episode; one or two
// agent_note deliveries are then enough. A queue that vanishes with no notice,
// or with no such record, is a failure. The clock file is read only when the
// suite's JEVONS_TEST_FAULTS=1 is set.
func (s *suite) jT952Hold() error {
	if err := portguard.RefuseDevelopment(s.port); err != nil {
		return err
	}
	prevEnv := append([]string(nil), s.daemonEnv...)
	defer func() {
		s.daemonEnv = prevEnv
		_ = s.signalStop(8 * time.Second)
		_ = os.Remove(filepath.Join(s.stateDir, "sendq", t952Hold+".json"))
		_ = os.Remove(filepath.Join(s.stateDir, "sendq", t952Stall+".json"))
		if reg, err := claudia.NewRegistry(s.agentsPath()); err == nil {
			_ = reg.Remove(t952Hold)
			_ = reg.Remove(t952Stall)
		}
		if err := s.startDaemon(); err != nil {
			fmt.Printf("J39 restore daemon: %v\n", err)
		}
	}()

	clock := filepath.Join(s.stateDir, "t952-sweep-now")
	t0 := time.Date(2026, 10, 14, 5, 45, 26, 0, time.UTC)
	if err := os.WriteFile(clock, []byte(t0.Format(time.RFC3339Nano)), 0o644); err != nil {
		return err
	}
	s.daemonEnv = append(s.daemonEnv, mcpserver.IsolateSweepNowFileEnv+"="+clock)
	if err := s.signalStop(8 * time.Second); err != nil {
		return fmt.Errorf("stop before planting the hold: %w", err)
	}
	if err := s.plantT952(t0); err != nil {
		return err
	}
	if err := s.startDaemon(); err != nil {
		return fmt.Errorf("start with the simulated sweep clock: %w", err)
	}

	holdAge := 77*time.Hour + 6*time.Minute + 41*time.Second
	for minute := 0; minute <= 20; minute++ {
		if minute == 10 {
			if err := s.bounceDrain(); err != nil {
				return fmt.Errorf("restart at simulated minute 10: %w", err)
			}
		}
		now := t0.Add(time.Duration(minute) * time.Minute)
		if err := os.WriteFile(clock, []byte(now.Format(time.RFC3339Nano)), 0o644); err != nil {
			return err
		}
		after := s.logSize()
		if err := s.waitForSweepAge(t952Hold, holdAge+time.Duration(minute)*time.Minute, after); err != nil {
			return fmt.Errorf("minute %d: %w", minute, err)
		}
	}

	holdRows, stallRows, err := s.t952QueueRows()
	if err != nil {
		return err
	}
	if !t952PendingText(holdRows, t952HoldText) {
		return s.t952Fail(fmt.Sprintf("%s obligation changed: %+v", t952Hold, holdRows))
	}
	if stallMutated(stallRows) {
		return s.t952Fail(fmt.Sprintf("%s obligation changed: %+v", t952Stall, stallRows))
	}
	show, err := s.mcpText("jevons_sendq_reconcile", map[string]any{"name": t952Hold})
	if err != nil {
		return fmt.Errorf("held status: %w", err)
	}
	if !strings.Contains(show, t952HoldText) {
		return s.t952Fail(fmt.Sprintf("held obligation is not on the reconcile path: %s", trim(show, 400)))
	}
	return s.waitForT952Transcript()
}

func (s *suite) plantT952(t0 time.Time) error {
	for _, name := range []string{t952Hold, t952Stall} {
		if err := os.MkdirAll(filepath.Join(s.stateDir, name+"-work"), 0o755); err != nil {
			return err
		}
	}
	reg, err := claudia.NewRegistry(s.agentsPath())
	if err != nil {
		return err
	}
	if reg.Def(overseerName) == nil {
		return fmt.Errorf("planting would replace a registry that has no overseer")
	}
	for _, name := range []string{t952Hold, t952Stall} {
		if err := reg.Register(claudia.AgentDef{
			Name:      name,
			WorkDir:   filepath.Join(s.stateDir, name+"-work"),
			SessionID: uuid.NewString(),
			Provider:  s.provider,
			AutoStart: false,
			Parent:    overseerName,
			Purpose:   claudia.PurposeAside,
		}); err != nil {
			return fmt.Errorf("register %s: %w", name, err)
		}
	}
	store, err := fleetintent.Open(s.stateDir)
	if err != nil {
		return err
	}
	if err := store.SetAgent(t952Hold, fleetintent.Parked, "owner", "intentional hold", t0); err != nil {
		return err
	}
	q := sendq.NewStore(filepath.Join(s.stateDir, "sendq"))
	if _, _, err := q.Append(t952Hold, t952HoldText, t0.Add(-(77*time.Hour + 6*time.Minute + 41*time.Second))); err != nil {
		return err
	}
	if _, _, err := q.Append(t952Stall, t952StallText, t0.Add(-(10*time.Minute + 25*time.Second))); err != nil {
		return err
	}
	return nil
}

func (s *suite) waitForSweepAge(agent string, age time.Duration, after int64) error {
	wantAgent := "agent=" + agent
	wantAge := "oldest_age=" + age.Round(time.Second).String()
	deadline := time.Now().Add(45 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(s.logPath)
		if err != nil {
			return err
		}
		if int64(len(body)) > after {
			chunk := string(body[after:])
			last = chunk
			for _, line := range strings.Split(chunk, "\n") {
				if strings.Contains(line, wantAgent) && strings.Contains(line, wantAge) {
					return nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("no sweep line %s %s; log tail: %s", wantAgent, wantAge, trim(last, 500))
}

func (s *suite) t952QueueRows() (hold, stall []sendq.Entry, err error) {
	q := sendq.NewStore(filepath.Join(s.stateDir, "sendq"))
	hold, err = q.Snapshot(t952Hold)
	if err != nil {
		return nil, nil, err
	}
	stall, err = q.Snapshot(t952Stall)
	return hold, stall, err
}

func t952PendingText(rows []sendq.Entry, want string) bool {
	return len(rows) == 1 && rows[0].Text == want && rows[0].State == sendq.Pending
}

// stallMutated is a queue that is neither the original pending text nor gone.
// An empty snapshot is the overseer resolving the stall, judged separately.
func stallMutated(rows []sendq.Entry) bool {
	return len(rows) != 0 && !t952PendingText(rows, t952StallText)
}

// t952Facts is the isolate's reading after the simulated twenty minutes.
type t952Facts struct {
	HoldIntact    bool
	HoldSubmitted bool
	StallPending  bool
	StallNotices  int
	HoldNamed     int
	StallResolved bool
}

// t952IsolateVerdict is the 🎯T952 allowance. A pending stall must show the
// open notice and the 15-minute band. A resolved stall must show one or two
// agent_note deliveries and a daemon record of how the queue left. The parked
// hold is never a submission.
func t952IsolateVerdict(f t952Facts) (bool, string) {
	if !f.HoldIntact {
		return false, "parked hold queue changed"
	}
	if f.HoldSubmitted {
		return false, "parked hold was submitted"
	}
	if f.HoldNamed > 0 {
		return false, fmt.Sprintf("hold named in fleet-health bodies=%d", f.HoldNamed)
	}
	if f.StallNotices > 2 {
		return false, fmt.Sprintf("stall notices=%d exceed the two age-band allowance", f.StallNotices)
	}
	if f.StallNotices < 1 {
		return false, "stall notices=0; the first alert was lost"
	}
	if f.StallPending {
		if f.StallNotices != 2 {
			return false, fmt.Sprintf("stall still pending but notices=%d want the open notice and the 15-minute band", f.StallNotices)
		}
		return true, ""
	}
	if !f.StallResolved {
		return false, fmt.Sprintf("stall queue is empty with notices=%d and no start, drain, kill, reconcile, or undelivered record", f.StallNotices)
	}
	return true, ""
}

func t952HoldSubmitted(logText string) bool {
	return t952LogHas(logText, t952Hold, []string{
		"re-offering a held message",
		"agent started",
		"rehydrated dead/stopped",
	})
}

// t952StallResolved reads the daemon's own record of how the stall queue
// left. decision=sendq_reconcile is the overseer settling it through
// jevons_sendq_reconcile (🎯T726), which logs the removed payload with the
// actor and evidence: in gate 252cf6ac the overseer's start of t952-stall was
// refused (its planted session has no conversation to resume) and it then
// dropped the entry on the record. That is a recorded disposition, not a
// queue that vanished; it was missing from this list, so J39 failed in the
// suite and passed alone only because the model chose differently.
func t952StallResolved(logText string) bool {
	return t952LogHas(logText, t952Stall, []string{
		"re-offering a held message",
		"agent started",
		"rehydrated dead/stopped",
		"kill_discard_sendq",
		"backlog_undelivered",
		"decision=sendq_reconcile",
	})
}

func t952LogHas(logText, agent string, markers []string) bool {
	for _, line := range strings.Split(logText, "\n") {
		if !strings.Contains(line, agent) {
			continue
		}
		for _, marker := range markers {
			if strings.Contains(line, marker) {
				return true
			}
		}
	}
	return false
}

func (s *suite) waitForT952Transcript() error {
	deadline := time.Now().Add(45 * time.Second)
	var bodies []string
	var why string
	for {
		holdRows, stallRows, qerr := s.t952QueueRows()
		if qerr != nil {
			if time.Now().After(deadline) {
				return qerr
			}
			time.Sleep(2 * time.Second)
			continue
		}
		logText := ""
		if raw, err := os.ReadFile(s.logPath); err == nil {
			logText = string(raw)
		}
		var err error
		bodies, err = t952OverseerBodies(s.stateDir)
		if err != nil {
			if time.Now().After(deadline) {
				return err
			}
			time.Sleep(2 * time.Second)
			continue
		}
		stalled, holdNamed := t952NoticeCounts(bodies)
		var ok bool
		ok, why = t952IsolateVerdict(t952Facts{
			HoldIntact:    t952PendingText(holdRows, t952HoldText),
			HoldSubmitted: t952HoldSubmitted(logText),
			StallPending:  t952PendingText(stallRows, t952StallText),
			StallNotices:  stalled,
			HoldNamed:     holdNamed,
			StallResolved: t952StallResolved(logText),
		})
		if stallMutated(stallRows) {
			ok = false
			why = fmt.Sprintf("%s obligation changed: %+v", t952Stall, stallRows)
		}
		if ok {
			return nil
		}
		hard := stalled > 2 || holdNamed > 0 || t952HoldSubmitted(logText) || !t952PendingText(holdRows, t952HoldText) || stallMutated(stallRows)
		if hard || time.Now().After(deadline) {
			return s.t952TranscriptErr(bodies, why)
		}
		time.Sleep(2 * time.Second)
	}
}

func (s *suite) t952Fail(why string) error {
	bodies, _ := t952OverseerBodies(s.stateDir)
	return s.t952TranscriptErr(bodies, why)
}

// t952TranscriptErr attaches the owner-visible store and the notify-queue
// decisions to a failed count. The suite deletes the state dir on the way
// out, so the counts have to be taken before return. T952_DIAG_DIR, when
// set, receives a copy of the daemon log; the path is never committed.
func (s *suite) t952TranscriptErr(bodies []string, why string) error {
	logText := ""
	if raw, err := os.ReadFile(s.logPath); err == nil {
		logText = string(raw)
	}
	detail := t952Diag(bodies, logText)
	shape := t952StoreShape(s.stateDir)
	if dir := strings.TrimSpace(os.Getenv("T952_DIAG_DIR")); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, "t952-diag.txt"), []byte(why+"\n"+detail+"\n"+shape+"\n"), 0o644)
		if logText != "" {
			_ = os.WriteFile(filepath.Join(dir, "t952-jevonsd.log"), []byte(logText), 0o644)
		}
	}
	return fmt.Errorf("%s; %s; %s", why, detail, shape)
}

// t952Diag summarises transcript bodies and notify-queue log lines. It does
// not decide the journey: t952IsolateVerdict does.
func t952Diag(bodies []string, logText string) string {
	stalled, holdNamed := t952NoticeCounts(bodies)
	noAction, fleetish, named := 0, 0, 0
	sample := ""
	for _, body := range bodies {
		flat := strings.ReplaceAll(body, `\"`, `"`)
		if strings.Contains(flat, "no action") {
			noAction++
		}
		if strings.Contains(flat, "[Fleet health]") || strings.Contains(flat, "Stalled backlog") {
			fleetish++
		}
		if strings.Contains(flat, t952Stall) || strings.Contains(flat, t952Hold) {
			named++
			if sample == "" {
				sample = trim(flat, 180)
			}
		}
	}
	if sample == "" {
		for _, body := range bodies {
			flat := strings.TrimSpace(strings.ReplaceAll(body, `\"`, `"`))
			if flat != "" {
				sample = trim(flat, 180)
				break
			}
		}
	}
	return fmt.Sprintf(
		"bodies=%d stalled=%d holdNamed=%d noAction=%d fleetish=%d named=%d sample=%q logDrain=%d logDefer=%d logEnqueue=%d logUnconfirmed=%d logCoalesced=%d logStuck=%d",
		len(bodies), stalled, holdNamed, noAction, fleetish, named, sample,
		strings.Count(logText, "decision=drain"),
		strings.Count(logText, "decision=defer"),
		strings.Count(logText, "decision=enqueue"),
		strings.Count(logText, "delivered_unconfirmed"),
		strings.Count(logText, "notice_coalesced"),
		strings.Count(logText, "stuck-busy"),
	)
}

func t952StoreShape(stateDir string) string {
	uri := url.URL{Scheme: "file", Path: statedb.DefaultPath(stateDir), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return "store=" + err.Error()
	}
	defer db.Close()
	rows, err := db.Query("SELECT agent, typ, COUNT(*) FROM transcript_events GROUP BY agent, typ ORDER BY agent, typ")
	if err != nil {
		return "store=" + err.Error()
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var agent, typ string
		var n int
		if err := rows.Scan(&agent, &typ, &n); err != nil {
			return "store=" + err.Error()
		}
		parts = append(parts, fmt.Sprintf("%s/%s=%d", agent, typ, n))
	}
	if err := rows.Err(); err != nil {
		return "store=" + err.Error()
	}
	if len(parts) == 0 {
		parts = append(parts, "empty")
	}
	for _, rel := range []string{
		filepath.Join("chatlog", overseerName+".jsonl"),
		filepath.Join("agent-chatlogs", overseerName+".jsonl"),
		"notice-coalesce.json",
	} {
		raw, err := os.ReadFile(filepath.Join(stateDir, rel))
		if err != nil {
			parts = append(parts, rel+"=absent")
			continue
		}
		text := strings.ReplaceAll(string(raw), `\"`, `"`)
		parts = append(parts, fmt.Sprintf("%s bytes=%d stall=%t noaction=%t hold=%t",
			rel, len(raw),
			strings.Contains(text, `Stalled backlog on "`+t952Stall+`"`),
			strings.Contains(text, "no action"),
			strings.Contains(text, t952Hold),
		))
	}
	return "store " + strings.Join(parts, " ")
}

func t952NoticeCounts(bodies []string) (stalled, holdNamed int) {
	needle := `Stalled backlog on "` + t952Stall + `"`
	var joined strings.Builder
	for _, body := range bodies {
		flat := strings.ReplaceAll(body, `\"`, `"`)
		// The model may quote the alert. The episode count is the agent_note
		// written when the notice was admitted, not that paraphrase.
		if strings.Contains(flat, `"type":"agent_note"`) {
			joined.WriteString(flat)
			joined.WriteByte('\n')
		}
		// The words of the row, not its whole body: in the second full suite
		// on 024bbdf1 the stall's notice row (agent_note, naming only
		// t952-stall) carried the input of a concurrent overseer tool call,
		// {"name":"t952-hold"} for jevons_sendq_reconcile, and was counted as
		// a notice naming the hold (🎯T1088 records the fold).
		words := t952RowWords(body)
		if strings.Contains(words, t952Hold) && (strings.Contains(words, "Stalled backlog") || strings.Contains(words, "[Fleet health]") || strings.Contains(words, "no action")) {
			holdNamed++
		}
	}
	return strings.Count(joined.String(), needle), holdNamed
}

func t952OverseerBodies(stateDir string) ([]string, error) {
	uri := url.URL{Scheme: "file", Path: statedb.DefaultPath(stateDir), RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT body FROM transcript_events WHERE agent = ? ORDER BY idx", overseerName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bodies []string
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		bodies = append(bodies, body)
	}
	return bodies, rows.Err()
}

// t952RowWords is the text a transcript row says: an agent_note's text, or
// a message's text content. A body that is not JSON is its own text.
func t952RowWords(body string) string {
	var row struct {
		Text    string `json:"text"`
		Message struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &row); err != nil {
		return body
	}
	parts := []string{row.Text}
	for _, c := range row.Message.Content {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, "\n")
}
