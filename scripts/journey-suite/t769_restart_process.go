// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"
	"time"
)

// waitAgentStatus polls /api/agents until name reports the given status
// (e.g. "running"). Used by 🎯T769 to prove a seat regained a live process
// after a daemon bounce without jevons_agent_start.
func (s *suite) waitAgentStatus(name, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		agents, err := s.ListAgentsHTTP()
		if err != nil {
			return err
		}
		for _, a := range agents {
			if a.Name == name && a.Status == want {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	agents, _ := s.ListAgentsHTTP()
	return fmt.Errorf("agent %q did not reach status %q within %s (rows=%v)", name, want, timeout, agents)
}

// t769NoManualAgentStart scans post-restart daemon logs and rejects evidence
// that something called the jevons_agent_start MCP tool for this seat. Boot
// reattach logs "agent started" instead; a human or overseer start is the bug
// class from 2026-09-21 (jevons-po with dozens queued, no process).
func t769NoManualAgentStart(logs []byte, name string) error {
	for _, line := range strings.Split(string(logs), "\n") {
		if !strings.Contains(line, "jevons_agent_start") {
			continue
		}
		if strings.Contains(line, `"name":"`+name+`"`) ||
			strings.Contains(line, `"name": "`+name+`"`) ||
			strings.Contains(line, `name\":\"`+name+`\"`) ||
			strings.Contains(line, "name="+name) {
			return fmt.Errorf("post-restart log shows manual jevons_agent_start for %q: %s", name, trim(line, 240))
		}
	}
	return nil
}
