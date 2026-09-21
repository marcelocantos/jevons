// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// 🎯T806: the owner's echo lands in the transcript before delivery, so a
// refused or deferred owner message must say so in the same transcript. The
// frames reuse the send_error diagnostic the React reducer already renders;
// msg_id/state let a client key a row to its message.

const (
	notifyRetryBase = 15 * time.Second
	notifyRetryMax  = 2 * time.Minute
)

func (s *Server) registerOwnerMessageID(text, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerOwnerMessageIDLocked(text, id)
}

// registerOwnerMessageIDLocked is the enqueue-side half: SendToOverseerAs calls
// it under the same lock hold that appends the text, so the id FIFO for a text
// and the queue order of that text can never be interleaved by another sender.
func (s *Server) registerOwnerMessageIDLocked(text, id string) {
	if s.notifyOwnerIDs == nil {
		s.notifyOwnerIDs = map[string][]string{}
	}
	s.notifyOwnerIDs[text] = append(s.notifyOwnerIDs[text], id)
}

func (s *Server) peekOwnerMessageID(text string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ids := s.notifyOwnerIDs[text]; len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// ownerStateKey keys per-message announce state by id, so two identical texts
// are two messages; text is only the fallback for an id-less enqueue.
func ownerStateKey(id, text string) string {
	if id != "" {
		return "id:" + id
	}
	return "text:" + text
}

func (s *Server) broadcastDeliveryFrame(state, id, text, reason string) {
	frame := map[string]any{
		"type":   "send_error",
		"state":  state,
		"msg_id": id,
		"text":   text,
	}
	if reason != "" {
		frame["reason"] = reason // 🎯T811: the cockpit renders "not delivered: <reason>"
	}
	b, _ := json.Marshal(frame)
	s.BroadcastChat(string(b))
}

// announceOwnerUndelivered tells the owner once per message that delivery was
// refused, naming the reason. The message itself stays queued for retry.
func (s *Server) announceOwnerUndelivered(text string, err error) {
	id := s.peekOwnerMessageID(text)
	key := ownerStateKey(id, text)
	s.mu.Lock()
	if s.ownerUndelivered == nil {
		s.ownerUndelivered = map[string]bool{}
	}
	if s.ownerUndelivered[key] {
		s.mu.Unlock()
		return
	}
	s.ownerUndelivered[key] = true
	s.mu.Unlock()
	reason := strings.TrimSpace(err.Error())
	s.broadcastDeliveryFrame("undelivered", id,
		fmt.Sprintf("message not delivered — will retry: %s", reason), reason)
}

// announceOwnerDelivered flips a previously announced refusal to delivered,
// exactly once, and retires the id.
func (s *Server) announceOwnerDelivered(text string) {
	id := s.peekOwnerMessageID(text)
	s.mu.Lock()
	if ids := s.notifyOwnerIDs[text]; len(ids) > 1 {
		s.notifyOwnerIDs[text] = ids[1:]
	} else {
		delete(s.notifyOwnerIDs, text)
	}
	key := ownerStateKey(id, text)
	was := s.ownerUndelivered[key]
	delete(s.ownerUndelivered, key)
	s.notifyRetryN = 0
	s.mu.Unlock()
	s.noteOwnerDeliveredID(id)
	s.persistOwnerQueue()
	if was {
		s.broadcastDeliveryFrame("delivered", id, "message delivered to the overseer after retry", "")
	}
}

// scheduleNotifyRetry re-drains a refused owner batch with backoff; deferral
// used to have no timer, so a refusal waited for unrelated traffic.
func (s *Server) scheduleNotifyRetry() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notifyRetryTimer != nil {
		return
	}
	d := s.notifyRetryDelay
	if d == 0 {
		d = notifyRetryBase << min(s.notifyRetryN, 3)
		d = min(d, notifyRetryMax)
	}
	s.notifyRetryN++
	s.notifyRetryTimer = time.AfterFunc(d, func() {
		s.mu.Lock()
		s.notifyRetryTimer = nil
		s.mu.Unlock()
		s.drainOverseerNotes()
	})
}

var ownerMsgSeq atomic.Uint64

func newOwnerMessageID() string {
	return fmt.Sprintf("om-%d-%d", time.Now().UnixMilli(), ownerMsgSeq.Add(1))
}

// SetOverseerReattachWait bounds how long an owner send waits for a
// momentarily absent overseer to re-attach before it is nacked (🎯T806).
func (s *Server) SetOverseerReattachWait(d time.Duration) {
	s.mu.Lock()
	s.overseerReattachWait = d
	s.mu.Unlock()
}

func (s *Server) awaitOverseerProcess() bool {
	s.mu.RLock()
	wait := s.overseerReattachWait
	s.mu.RUnlock()
	deadline := time.Now().Add(wait)
	for {
		if proc := s.CurrentProcess(); proc != nil && proc.Alive() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ResendOwnerMessage retries a still-queued owner message now (the cockpit's
// Resend). It never re-enqueues the text: the message is the one already
// queued, so a resend racing the automatic retry cannot double-deliver. An id
// that is not queued (delivered, or unknown) is an error, not a silent no-op.
func (s *Server) ResendOwnerMessage(id string) error {
	s.mu.Lock()
	queued := false
	for _, ids := range s.notifyOwnerIDs {
		if slices.Contains(ids, id) {
			queued = true
			break
		}
	}
	if !queued {
		s.mu.Unlock()
		return fmt.Errorf("message %s is not waiting for delivery", id)
	}
	if s.notifyRetryTimer != nil {
		s.notifyRetryTimer.Stop()
		s.notifyRetryTimer = nil
	}
	s.mu.Unlock()
	s.drainOverseerNotes()
	return nil
}
