// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// 🎯T810 — A REPEATED FLEET-HEALTH NOTICE DOES NOT START ANOTHER OVERSEER TURN.
//
// 2026-09-22: the stalled-backlog sweep ran every 35–60 s and re-announced
// the same hold each time. The notice embeds the queue's age ("the oldest
// waiting 2h29m36s"), so every copy was byte-different, and the byte-exact
// T428/T568 guard (which says on purpose that a changed timestamp is new
// content) admitted all of them. jevonsd.log for the 21.5 h to 2026-09-22
// 04:38 holds 677 such notices across 17 agents — about 660 repeats, each an
// overseer turn that answered "nothing has changed".
//
// THE RULE. Per occurrence key, a notice is delivered when its MATERIAL digest
// differs from the last one delivered, and otherwise coalesced: not sent, and
// counted. Material = the text with every duration replaced by the threshold
// band it falls in (so age drifting inside a band is not news, and crossing a
// band is), leaving counts and subjects verbatim (so a new count or subject
// is news). There is no disposition tool to consult and parsing the
// overseer's reply would be fragile, so delivery of this digest is the
// disposition: the overseer has been told this and has had its turn on it.
//
// STATE CLEARING. A live condition is re-offered every sweep, so a key not
// offered for NoticeClearedAfter means the condition cleared; its next offer
// is a recurrence and is delivered. No per-emitter hook is needed.
//
// THE COUNTER. Saved turns per key are persisted with the memory, exposed by
// FormatNoticeSavings (agent_list) and each coalesce is a `notice_coalesced`
// eventlog record carrying the running count. Owner messages never pass here:
// this sits in notifyFleetHealth only.

// NoticeClearedAfter is how long a key may go un-offered before its next offer
// counts as a new occurrence of the condition.
const NoticeClearedAfter = 10 * time.Minute

const noticeCoalesceFile = "notice-coalesce.json"

// noticeAgeBands are the thresholds a duration crossing makes a notice
// materially new.
var noticeAgeBands = []time.Duration{15 * time.Minute, time.Hour, 6 * time.Hour, 24 * time.Hour}

var noticeDurationRE = regexp.MustCompile(`\b\d+(?:\.\d+)?(?:h|m|s|ms)(?:\d+(?:\.\d+)?(?:h|m|s|ms))*\b`)

type noticeRecord810 struct {
	Digest      string    `json:"digest"`
	DeliveredAt time.Time `json:"delivered_at"`
	LastOffered time.Time `json:"last_offered"`
	Saved       int       `json:"saved"`
	// Unconfirmed marks a delivery the receiver's records never showed; it is
	// retried after notifyReplayUnconfirmedGrace rather than held forever.
	Unconfirmed bool `json:"unconfirmed,omitempty"`
}

type noticeCoalescer struct {
	mu      sync.Mutex
	loaded  bool
	dir     string
	entries map[string]*noticeRecord810
	now     func() time.Time
}

// NoticeMaterialDigest hashes a notice with durations reduced to their band.
func NoticeMaterialDigest(line string) string {
	norm := noticeDurationRE.ReplaceAllStringFunc(strings.TrimSpace(line), func(tok string) string {
		d, err := time.ParseDuration(tok)
		if err != nil {
			return tok
		}
		band := 0
		for _, b := range noticeAgeBands {
			if d >= b {
				band++
			}
		}
		return fmt.Sprintf("<age-band-%d>", band)
	})
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

func (s *Server) noticeCoalescer() *noticeCoalescer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.noticeCo == nil {
		s.noticeCo = &noticeCoalescer{now: func() time.Time { return time.Now().UTC() }}
	}
	return s.noticeCo
}

// SetNoticeCoalesceClock overrides the coalescer clock (test seam).
func (s *Server) SetNoticeCoalesceClock(now func() time.Time) {
	c := s.noticeCoalescer()
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

// loadLocked reads the persisted memory once the state dir is known. A
// malformed file is reported and ignored: failing open means an extra notice,
// never a silenced one.
func (c *noticeCoalescer) loadLocked(dir string) {
	if c.loaded && c.dir == dir {
		return
	}
	c.loaded, c.dir = true, dir
	c.entries = map[string]*noticeRecord810{}
	if dir == "" {
		return
	}
	raw, err := os.ReadFile(filepath.Join(dir, noticeCoalesceFile))
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("notice coalesce: read", "err", err)
		}
		return
	}
	if err := json.Unmarshal(raw, &c.entries); err != nil {
		// Malformed state is never reset in place: the file is left for
		// inspection, persistence is off for this process, and notices flow
		// uncoalesced (an extra notice, never a silenced one).
		slog.Error("notice coalesce: malformed state; coalescing memory is in-process only", "err", err)
		c.entries = map[string]*noticeRecord810{}
		c.dir = ""
	}
}

func (c *noticeCoalescer) saveLocked() {
	if c.dir == "" {
		return
	}
	raw, err := json.Marshal(c.entries)
	if err == nil {
		tmp := filepath.Join(c.dir, noticeCoalesceFile+".tmp")
		if err = os.WriteFile(tmp, raw, 0o644); err == nil {
			err = os.Rename(tmp, filepath.Join(c.dir, noticeCoalesceFile))
		}
	}
	if err != nil {
		slog.Error("notice coalesce: persist", "err", err)
	}
}

// coalesce reports whether the notice is materially unchanged since the one
// already delivered for key, counting the saved turn when it is.
func (s *Server) coalesceNotice(key, line string) (coalesced bool, saved int) {
	c := s.noticeCoalescer()
	dir := s.deliveryStateDir()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked(dir)
	now := c.now()
	digest := NoticeMaterialDigest(line)
	e, ok := c.entries[key]
	if !ok {
		return false, 0
	}
	last := e.LastOffered
	e.LastOffered = now
	cleared := now.Sub(last) > NoticeClearedAfter
	retry := e.Unconfirmed && now.Sub(e.DeliveredAt) >= notifyReplayUnconfirmedGrace
	if e.Digest != digest || cleared || retry {
		return false, e.Saved
	}
	e.Saved++
	c.saveLocked()
	return true, e.Saved
}

// noteNoticeDelivered records that the overseer was given this digest.
func (s *Server) noteNoticeDelivered(key, line string, confirmed bool) {
	c := s.noticeCoalescer()
	dir := s.deliveryStateDir()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked(dir)
	now := c.now()
	e := c.entries[key]
	if e == nil {
		e = &noticeRecord810{}
		c.entries[key] = e
	}
	e.Digest, e.DeliveredAt, e.LastOffered, e.Unconfirmed = NoticeMaterialDigest(line), now, now, !confirmed
	c.saveLocked()
}

// NoticeSavedTurns is the per-key count of overseer turns saved.
func (s *Server) NoticeSavedTurns() map[string]int {
	c := s.noticeCoalescer()
	dir := s.deliveryStateDir()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked(dir)
	out := map[string]int{}
	for k, e := range c.entries {
		if e.Saved > 0 {
			out[k] = e.Saved
		}
	}
	return out
}

// FormatNoticeSavings is the agent_list line; empty when nothing was saved.
func (s *Server) FormatNoticeSavings() string {
	saved := s.NoticeSavedTurns()
	if len(saved) == 0 {
		return ""
	}
	keys := make([]string, 0, len(saved))
	total := 0
	for k, n := range saved {
		keys = append(keys, k)
		total += n
	}
	sort.Slice(keys, func(i, j int) bool { return saved[keys[i]] > saved[keys[j]] || saved[keys[i]] == saved[keys[j]] && keys[i] < keys[j] })
	const shown = 5
	parts := []string{}
	for i, k := range keys {
		if i == shown {
			parts = append(parts, fmt.Sprintf("+%d more", len(keys)-shown))
			break
		}
		parts = append(parts, fmt.Sprintf("%s=%d", k, saved[k]))
	}
	return fmt.Sprintf("Fleet-health notices coalesced (🎯T810): %d overseer turn(s) saved (%s)", total, strings.Join(parts, ", "))
}
