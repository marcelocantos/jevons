// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
)

// 🎯T806: the owner's undelivered messages live only in s.notifyQueue, so a
// daemon bounce lost exactly the messages the cockpit had already displayed.
// This persists the owner subset (atomic write-and-rename) with the ids of
// recently delivered messages, so a replay after a restart neither loses nor
// double-delivers. Malformed state is a hard error, never a silent reset.

const ownerDeliveredKeep = 200

type ownerQueueRecord struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type ownerQueueFile struct {
	Pending   []ownerQueueRecord `json:"pending"`
	Delivered []string           `json:"delivered"`
}

// LoadOwnerQueue reads the persisted owner queue at path and re-enqueues every
// pending message whose id is not already recorded as delivered.
func (s *Server) LoadOwnerQueue(path string) error {
	var f ownerQueueFile
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &f); err != nil {
			return fmt.Errorf("owner queue %s is malformed (not reset): %w", path, err)
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("owner queue %s: %w", path, err)
	}
	s.ownerQueueMu.Lock()
	s.ownerQueuePath = path
	s.ownerDelivered = f.Delivered
	s.ownerQueueMu.Unlock()

	replayed := 0
	for _, r := range f.Pending {
		if r.ID != "" && slices.Contains(f.Delivered, r.ID) {
			continue
		}
		if r.ID != "" {
			s.registerOwnerMessageID(r.Text, r.ID)
		}
		s.mu.Lock()
		s.notifyQueue = append(s.notifyQueue, r.Text)
		s.mu.Unlock()
		replayed++
	}
	if replayed > 0 {
		slog.Info("owner_queue replay", "component", "owner_queue", "replayed", replayed)
		s.scheduleNotifyRetry()
	}
	return nil
}

// persistOwnerQueue writes the owner subset of the notify queue plus the
// delivered ids. No-op until LoadOwnerQueue named a path.
func (s *Server) persistOwnerQueue() {
	s.ownerQueueMu.Lock()
	defer s.ownerQueueMu.Unlock()
	if s.ownerQueuePath == "" {
		return
	}
	s.mu.RLock()
	next := map[string]int{}
	var pending []ownerQueueRecord
	for _, text := range s.notifyQueue {
		if !isOwnerNotifyText(text) {
			continue
		}
		id := ""
		if ids := s.notifyOwnerIDs[text]; next[text] < len(ids) {
			id = ids[next[text]]
		}
		next[text]++
		pending = append(pending, ownerQueueRecord{ID: id, Text: text})
	}
	s.mu.RUnlock()
	if len(s.ownerDelivered) > ownerDeliveredKeep {
		s.ownerDelivered = s.ownerDelivered[len(s.ownerDelivered)-ownerDeliveredKeep:]
	}
	b, err := json.Marshal(ownerQueueFile{Pending: pending, Delivered: s.ownerDelivered})
	if err == nil {
		err = writeFileAtomic(s.ownerQueuePath, b)
	}
	if err != nil {
		slog.Error("owner_queue persist failed", "component", "owner_queue", "err", err)
	}
}

func (s *Server) noteOwnerDeliveredID(id string) {
	if id == "" {
		return
	}
	s.ownerQueueMu.Lock()
	s.ownerDelivered = append(s.ownerDelivered, id)
	s.ownerQueueMu.Unlock()
}

func writeFileAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".owner_queue-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
