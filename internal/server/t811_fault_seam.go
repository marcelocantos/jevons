// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// 🎯T811: the smallest seam an isolate journey needs to reproduce the broker's
// real not_owner refusal (claudia T124/T125) without touching the live overseer's
// grant. The journey writes <dir>/fault-owner-not-owner; a matching owner
// delivery attempt consumes it and fails with the broker's own error text.
// The seam exists only when main wires it, which requires JEVONS_TEST_FAULTS=1
// and a port other than the development one; a production daemon never looks.
//
// The file holds either a count (each owner delivery attempt consumes one) or
// a token (the first owner delivery whose text contains it is refused, once).
// 🎯T920: a journey uses the token. A count hits whatever owner message is at
// the head of the queue, and on a Claude overseer — which cannot steer, so an
// owner message waits behind the running turn — that was a message an earlier
// journey left queued. J33's own message was then delivered normally and its
// bubble never showed undelivered.

const brokerFaultFile = "fault-owner-not-owner"

// brokerNotOwnerText is the refusal the broker actually returned (see the
// T806 specimen), so classification and the cockpit reason are the real ones.
const brokerNotOwnerText = `broker protocol: not_owner (name="jevons"): grant jevons is not owned by this connection`

var brokerFaultMu sync.Mutex

// EnableBrokerFaultSeam arms the file-driven not_owner fault under dir.
func (s *Server) EnableBrokerFaultSeam(dir string) {
	s.mu.Lock()
	s.brokerFaultDir = dir
	s.mu.Unlock()
}

// injectedBrokerFault returns the injected refusal for an owner batch, or nil.
func (s *Server) injectedBrokerFault(text string) error {
	s.mu.RLock()
	dir := s.brokerFaultDir
	s.mu.RUnlock()
	if dir == "" || !isOwnerNotifyText(text) {
		return nil
	}
	brokerFaultMu.Lock()
	defer brokerFaultMu.Unlock()
	path := filepath.Join(dir, brokerFaultFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	armed := strings.TrimSpace(string(b))
	n, err := strconv.Atoi(armed)
	switch {
	case err == nil && n > 0:
		_ = os.WriteFile(path, []byte(strconv.Itoa(n-1)), 0o600)
	case err != nil && armed != "" && strings.Contains(text, armed):
		_ = os.WriteFile(path, []byte("0"), 0o600)
	default:
		return nil
	}
	// The journey's failure report reads this line to say whether the fault fired.
	slog.Info("🎯T811 fault seam refused an owner delivery", "component", "fault_seam",
		"armed", armed, "text_len", len(text))
	return fmt.Errorf("%s", brokerNotOwnerText)
}
