// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// HeavyLeaseOwner is the acquisition record, not proof that its PID holds a lock.
type HeavyLeaseOwner struct {
	PID     int       `json:"pid"`
	Command []string  `json:"command"`
	CWD     string    `json:"cwd"`
	Started time.Time `json:"started"`
	Token   string    `json:"token"`
}

// A capability must still have its gate-owned responder. File text (even a
// matching PID) survives death; neither it nor a retained wrapper FD suffices.
func leaseOwnerAlive(token string) bool {
	parts := strings.Split(token, ":")
	if len(parts) != 3 {
		return false
	}
	port, err := strconv.Atoi(parts[2])
	if err != nil || port < 1 || port > 65535 {
		return false
	}
	c, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", parts[2]), time.Second)
	if err != nil {
		return false
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err := fmt.Fprintln(c, token); err != nil {
		return false
	}
	answer, err := bufio.NewReader(io.LimitReader(c, 256)).ReadString('\n')
	return err == nil && answer == token+"\n"
}

func serveLeaseOwner(listener net.Listener, token string) {
	for {
		c, err := listener.Accept()
		if err != nil {
			return
		}
		// Bound each request; no unbounded goroutine fan-out on this local endpoint.
		_ = c.SetDeadline(time.Now().Add(time.Second))
		request, err := bufio.NewReader(io.LimitReader(c, 256)).ReadString('\n')
		if err == nil && request == token+"\n" {
			_, _ = fmt.Fprintln(c, token)
		}
		_ = c.Close()
	}
}

// WriteHeavyLeaseStatus never locks, unlocks, deletes, or repairs the lease.
// lsof supplies actual open descriptors including retained non-gate wrappers.
// An open descriptor is labelled as such, not invented into a lock owner.
func WriteHeavyLeaseStatus(w io.Writer, root string) error {
	path := heavyLockPath(root)
	token, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		_, err = fmt.Fprintln(w, "heavy lease: no acquisition recorded")
		return err
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "heavy lease: recorded token=%q responder_alive=%t\n", string(token), leaseOwnerAlive(string(token)))
	var owner HeavyLeaseOwner
	data, readErr := os.ReadFile(path + ".owner")
	if readErr == nil {
		if err := json.Unmarshal(data, &owner); err != nil {
			return fmt.Errorf("heavy lease owner record: %w", err)
		}
		if owner.Token == string(token) {
			fmt.Fprintf(w, "recorded gate: pid=%d command=%q cwd=%q acquisition_age=%s\n", owner.PID, owner.Command, owner.CWD, time.Since(owner.Started).Round(time.Second))
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	out, inspectErr := exec.Command("lsof", "-n", "-P", "-Fpcf", path).CombinedOutput()
	fmt.Fprintf(w, "open descriptors (not proof of exclusive ownership):\n%s", out)
	if inspectErr != nil {
		fmt.Fprintf(w, "descriptor inspection: %v\n", inspectErr)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "p") {
			continue
		}
		pid := strings.TrimPrefix(line, "p")
		if _, err := strconv.Atoi(pid); err != nil {
			continue
		}
		cwd, cwdErr := exec.Command("lsof", "-a", "-p", pid, "-d", "cwd", "-Fn").CombinedOutput()
		process, procErr := exec.Command("ps", "-p", pid, "-o", "pid=,ppid=,etime=,command=").CombinedOutput()
		fmt.Fprintf(w, "descriptor process %s: cwd=%sprocess=%s", pid, cwd, process)
		if cwdErr != nil || procErr != nil {
			fmt.Fprintf(w, "inspection incomplete: cwd=%v process=%v\n", cwdErr, procErr)
		}
	}
	return nil
}
