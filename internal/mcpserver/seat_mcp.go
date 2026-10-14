// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T797 — a seat that comes up without a required MCP server is marked in
// agent_list and its parent is told once.
//
// Observation: a Claude seat records each MCP server that attached in its
// own session file — `deferred_tools_delta` attachments carry the tool names
// (`mcp__<server>__<tool>`) and `mcp_instructions_delta` carries server
// names. A server in CONNECT_TIMEOUT never appears, so "required and never
// seen after the grace" is the failure, read from evidence the seat already
// writes rather than a second detector. A late attach (the client retried
// and won) clears the flag.
//
// Reaction is surfacing, deliberately not relaunch: a relaunch would have to
// be provably safe against 🎯T796 (two live clients on one session), and the
// seat's parent can re-mint with the whole picture. This never stops, kills,
// re-sends, or migrates a seat.
//
// Residual, declared: only Claude seats are observable (other providers
// record no such attachment); a transcript with no delta record at all is
// unobservable, never "missing"; the required set is AgentDef.MCPServers plus
// intersected with RequiredServers, not every registration — a stale entry the owner never uses must not flag
// every seat.

// SeatMCPGrace is how long after the seat's first transcript record a
// required server may still be "not yet". The client's own connect timeout
// is 30s under normal load; the host runs heavily loaded, so this is headroom
// rather than a measured percentile.
const SeatMCPGrace = 5 * time.Minute

// RequiredServers are the fleet-critical servers whose absence leaves a seat
// unable to do its job (the 2026-09-22 incident: no bullseye for hours). A
// server counts only when the seat is actually configured with it
// (AgentDef.MCPServers or a ~/.claude.json registration); everything else the
// owner has registered — including entries that are permanently dead — is
// not this target's business and would flag every seat.
var RequiredServers = []string{"jevonsmcp", "bullseye"}

type seatMCPDiagnosis struct {
	Missing []string
	Age     time.Duration
	// Observed is false when the seat's records cannot say (wrong provider,
	// no transcript, no delta record, unreadable).
	Observed bool
}

type seatMCPSeen struct {
	servers map[string]bool
	first   time.Time
	deltas  int
}

type seatMCPCacheEntry struct {
	size   int64
	mtime  time.Time
	offset int64 // bytes consumed: complete lines only (🎯T804)
	seen   seatMCPSeen
}

var seatMCPCache sync.Map // transcript path -> seatMCPCacheEntry

// requiredSeatServers is the fleet-critical set this seat is configured with.
func requiredSeatServers(d claudia.AgentDef) []string {
	configured := map[string]bool{}
	for _, m := range d.MCPServers {
		configured[strings.TrimSpace(m.Name)] = true
	}
	if regs, ok := mcpRegistrationsFor(d.Provider); ok {
		for _, r := range regs {
			configured[r.Name] = true
		}
	}
	var out []string
	for _, want := range RequiredServers {
		if configured[want] {
			out = append(out, want)
		}
	}
	return out
}

// seatMCPScans counts transcript reads (🎯T804 test seam: the request path
// must not add to it).
var seatMCPScans atomic.Int64

// seatMCPWarming dedupes background scans by transcript path.
var seatMCPWarming sync.Map

// scanSeatMCP reads the attach records out of a Claude session file. It is
// incremental (🎯T804): a live seat's transcript grows on every turn, and a
// cache keyed on size alone re-read the whole file each time. Only the
// bytes appended since the last scan are read; a file that shrank is read
// again from the start.
func scanSeatMCP(path string) (seatMCPSeen, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return seatMCPSeen{}, err
	}
	var prev seatMCPCacheEntry
	if v, ok := seatMCPCache.Load(path); ok {
		prev = v.(seatMCPCacheEntry)
		if prev.size == fi.Size() && prev.mtime.Equal(fi.ModTime()) {
			return prev.seen, nil
		}
		if fi.Size() < prev.offset {
			prev = seatMCPCacheEntry{}
		}
	}
	seatMCPScans.Add(1)
	f, err := os.Open(path)
	if err != nil {
		return seatMCPSeen{}, err
	}
	defer f.Close()
	seen := seatMCPSeen{servers: map[string]bool{}, first: prev.seen.first, deltas: prev.seen.deltas}
	for k, v := range prev.seen.servers {
		seen.servers[k] = v
	}
	offset := prev.offset
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return seatMCPSeen{}, err
	}
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			break // an incomplete last line is read next time
		}
		if err != nil {
			return seatMCPSeen{}, err
		}
		offset += int64(len(line))
		foldSeatMCPLine(&seen, line)
	}
	seatMCPCache.Store(path, seatMCPCacheEntry{size: fi.Size(), mtime: fi.ModTime(), offset: offset, seen: seen})
	return seen, nil
}

// cachedSeatMCP answers from what is already known about path, never
// reading the file on the caller's time (🎯T804): a missing or stale entry
// starts a background scan and the last known answer, if any, is used.
func cachedSeatMCP(path string) (seatMCPSeen, bool) {
	var entry seatMCPCacheEntry
	v, ok := seatMCPCache.Load(path)
	if ok {
		entry = v.(seatMCPCacheEntry)
	}
	fresh := false
	if fi, err := os.Stat(path); err == nil && ok {
		fresh = entry.size == fi.Size() && entry.mtime.Equal(fi.ModTime())
	}
	if !fresh {
		seatMCPWarm(path)
	}
	return entry.seen, ok
}

// seatMCPWarm starts one background scan of path (test seam).
var seatMCPWarm = seatMCPWarmDefault

func seatMCPWarmDefault(path string) {
	if _, busy := seatMCPWarming.LoadOrStore(path, struct{}{}); busy {
		return
	}
	go func() {
		defer seatMCPWarming.Delete(path)
		if _, err := scanSeatMCP(path); err != nil {
			slog.Debug("seat mcp: background scan failed", "path", path, "err", err)
		}
	}()
}

// foldSeatMCPLine applies one transcript line to seen.
func foldSeatMCPLine(seen *seatMCPSeen, line []byte) {
	if seen.first.IsZero() {
		var h struct {
			Timestamp time.Time `json:"timestamp"`
		}
		if json.Unmarshal(line, &h) == nil {
			seen.first = h.Timestamp
		}
	}
	if !strings.Contains(string(line), `_delta"`) {
		return
	}
	var rec struct {
		Attachment struct {
			Type         string   `json:"type"`
			AddedNames   []string `json:"addedNames"`
			RemovedNames []string `json:"removedNames"`
		} `json:"attachment"`
	}
	if json.Unmarshal(line, &rec) != nil {
		return
	}
	switch rec.Attachment.Type {
	case "deferred_tools_delta":
		seen.deltas++
		for _, n := range rec.Attachment.AddedNames {
			if srv, ok := mcpToolServer(n); ok {
				seen.servers[srv] = true
			}
		}
		for _, n := range rec.Attachment.RemovedNames {
			if srv, ok := mcpToolServer(n); ok {
				delete(seen.servers, srv)
			}
		}
	case "mcp_instructions_delta":
		seen.deltas++
		for _, n := range rec.Attachment.AddedNames {
			seen.servers[n] = true
		}
		for _, n := range rec.Attachment.RemovedNames {
			delete(seen.servers, n)
		}
	}
}

// mcpToolServer extracts <server> from mcp__<server>__<tool>.
func mcpToolServer(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, "mcp__")
	if !ok {
		return "", false
	}
	srv, _, ok := strings.Cut(rest, "__")
	return srv, ok && srv != ""
}

func (s *Server) diagnoseSeatMCP(d claudia.AgentDef, now time.Time) seatMCPDiagnosis {
	return s.diagnoseSeatMCPWith(d, now, true)
}

// diagnoseSeatMCPOnRequest is diagnoseSeatMCP for a caller waiting on the
// answer (jevons_agent_list): it never reads a transcript, only the cache
// the background sweep keeps warm (🎯T804).
func (s *Server) diagnoseSeatMCPOnRequest(d claudia.AgentDef, now time.Time) seatMCPDiagnosis {
	return s.diagnoseSeatMCPWith(d, now, false)
}

func (s *Server) diagnoseSeatMCPWith(d claudia.AgentDef, now time.Time, scan bool) seatMCPDiagnosis {
	var out seatMCPDiagnosis
	if d.Provider != claudia.ProviderClaude || strings.TrimSpace(d.SessionID) == "" {
		return out
	}
	ex := LookupTranscriptExistence(TranscriptExistenceQuery{
		Name: d.Name, Provider: d.Provider, SessionID: d.SessionID, WorkDir: d.WorkDir,
		Roots: s.transcriptRoots(),
	})
	if ex.Verdict != ExistencePresent || ex.Path == "" {
		return out
	}
	var seen seatMCPSeen
	if scan {
		var err error
		if seen, err = scanSeatMCP(ex.Path); err != nil {
			slog.Debug("seat mcp: unreadable transcript", "agent", d.Name, "err", err)
			return out
		}
	} else if seen, _ = cachedSeatMCP(ex.Path); seen.servers == nil {
		return out // not known yet; the background scan is under way
	}
	if seen.deltas == 0 || seen.first.IsZero() {
		return out
	}
	out.Observed = true
	if now.IsZero() {
		now = s.birthClock()
	}
	out.Age = now.Sub(seen.first)
	if out.Age < SeatMCPGrace {
		return out
	}
	for _, want := range requiredSeatServers(d) {
		if !seen.servers[want] {
			out.Missing = append(out.Missing, want)
		}
	}
	return out
}

// FormatSeatMCPNotice is the one parent message and the agent_list line.
func FormatSeatMCPNotice(d claudia.AgentDef, missing []string, age time.Duration) string {
	return fmt.Sprintf(
		"mcp-missing: %s (session=%s age=%s) never attached MCP server(s): %s — its tools are absent; re-mint the seat or run /mcp in it",
		d.Name, strings.TrimSpace(d.SessionID), age.Round(time.Second), strings.Join(missing, ", "))
}

func (s *Server) notifySeatMCPIfDue(d claudia.AgentDef, now time.Time, scan bool) {
	diag := s.diagnoseSeatMCPWith(d, now, scan)
	if len(diag.Missing) == 0 {
		return
	}
	key := "mcp-missing:" + d.Name + ":" + strings.TrimSpace(d.SessionID) + ":" + strings.Join(diag.Missing, ",")
	if s.noticeAlreadySubmitted(key) {
		return
	}
	text := FormatSeatMCPNotice(d, diag.Missing, diag.Age)
	slog.Error("seat missing MCP server", "agent", d.Name, "missing", diag.Missing)
	parent := strings.TrimSpace(d.Parent)
	if parent == "" {
		parent = s.overseerName()
	}
	if parent == "" || parent == d.Name {
		s.markNoticeOutcome(key, false, fmt.Errorf("no registry parent"), now)
		return
	}
	res, err := s.deliverByName(parent, text, OriginAgent, false)
	if noticeSubmitted(res.Status, err) {
		s.markNoticeOutcome(key, true, nil, now)
		return
	}
	if err == nil {
		err = fmt.Errorf("notice status %q is not submitted", res.Status)
	}
	s.markNoticeOutcome(key, false, err, now)
}

// sweepSeatMCP diagnoses live Claude seats and delivers at most one parent
// notice per (seat, session, missing set).
func (s *Server) sweepSeatMCP() { s.sweepSeatMCPWith(true) }

// sweepSeatMCPOnRequest is the sweep a waiting caller runs: cache only
// (🎯T804).
func (s *Server) sweepSeatMCPOnRequest() { s.sweepSeatMCPWith(false) }

func (s *Server) sweepSeatMCPWith(scan bool) {
	if s == nil || s.registry == nil {
		return
	}
	// A background diagnosis needs its own liveness feed: agent_list no
	// longer performs a fleet sweep to prime this observer.
	s.observeRegistryLiveness()
	now := s.birthClock()
	for _, d := range s.registry.List() {
		if strings.TrimSpace(d.Name) == "" || !s.seatAlive(d.Name) {
			continue
		}
		s.notifySeatMCPIfDue(d, now, scan)
	}
}
