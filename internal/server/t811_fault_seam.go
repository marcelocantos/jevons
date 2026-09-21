// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// 🎯T811: the smallest seam an isolate journey needs to reproduce the broker's
// real not_owner refusal (claudia T124/T125) without touching the live overseer's
// grant. The journey writes a count into <dir>/fault-owner-not-owner; each owner
// delivery attempt consumes one and fails with the broker's own error text.
// The seam exists only when main wires it, which requires JEVONS_TEST_FAULTS=1
// and a port other than the development one; a production daemon never looks.

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
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n <= 0 {
		return nil
	}
	_ = os.WriteFile(path, []byte(strconv.Itoa(n-1)), 0o600)
	return fmt.Errorf("%s", brokerNotOwnerText)
}
