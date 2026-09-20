// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package portown

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClassifyStubWildcardNamesPid(t *testing.T) {
	c := Classify([]Listener{
		{PID: 100, Cmd: "jevonsd", Addr: "127.0.0.1:13705"},
		{PID: 200, Cmd: "jevond", Addr: "*:13705"},
	})
	if c.Quiet() {
		t.Fatal("wildcard beside 127.0.0.1 must be a conflict")
	}
	text := c.Text()
	if !strings.Contains(text, "200") {
		t.Fatalf("must name the squatting pid, got %q", text)
	}
	if !strings.Contains(text, "jevond") {
		t.Fatalf("must name the squatting command, got %q", text)
	}
}

func TestClassifyQuietAfterStubRemoved(t *testing.T) {
	with := Classify([]Listener{
		{PID: 100, Cmd: "jevonsd", Addr: "127.0.0.1:59090"},
		{PID: 200, Cmd: "jevond", Addr: "*:59090"},
	})
	if with.Quiet() {
		t.Fatal("expected conflict while stub is up")
	}
	gone := Classify([]Listener{
		{PID: 100, Cmd: "jevonsd", Addr: "127.0.0.1:59090"},
	})
	if !gone.Quiet() {
		t.Fatalf("removing the stub must go quiet, got %+v", gone)
	}
}

func TestClassifyNoLoopbackHolderIsNotThisAlarm(t *testing.T) {
	c := Classify([]Listener{
		{PID: 200, Cmd: "jevond", Addr: "*:13705"},
	})
	if !c.Quiet() {
		t.Fatalf("unserved loopback is T405, not a T710 squatter: %+v", c)
	}
}

func TestClassifyIPv6Wildcard(t *testing.T) {
	c := Classify([]Listener{
		{PID: 1, Cmd: "jevonsd", Addr: "127.0.0.1:13705"},
		{PID: 9, Cmd: "ghost", Addr: "[::]:13705"},
	})
	if c.Quiet() || !strings.Contains(c.Text(), "9") {
		t.Fatalf("IPv6 wildcard must name pid 9, got %q", c.Text())
	}
}

func TestClassifySamePidWildcardStillFlags(t *testing.T) {
	// The live hermetic holds both sockets in one process.
	c := Classify([]Listener{
		{PID: 42, Cmd: "test", Addr: "127.0.0.1:1"},
		{PID: 42, Cmd: "test", Addr: "*:1"},
	})
	if c.Quiet() {
		t.Fatal("wildcard is a squatter even when the pid matches the loopback holder")
	}
}

func TestFingerprintAgentsSortsNames(t *testing.T) {
	body, _ := json.Marshal([]map[string]string{
		{"name": "jevon-po"},
		{"name": "csp-po"},
	})
	got := FingerprintAgents(body)
	if got != "csp-po,jevon-po" {
		t.Fatalf("got %q", got)
	}
}

func TestFleetMismatchDifferentNames(t *testing.T) {
	f := Fleet{
		Loopback:  Probe{OK: true, Fingerprint: "jevons,jevons-po"},
		Localhost: Probe{OK: true, Fingerprint: "csp-po,jevon-po"},
	}
	if !f.Mismatch() {
		t.Fatal("different fleets must mismatch")
	}
	if !strings.Contains(f.Text(), "localhost") {
		t.Fatalf("mismatch text: %q", f.Text())
	}
}

func TestFleetBothDownIsNotMismatch(t *testing.T) {
	f := Fleet{
		Loopback:  Probe{Err: "refused"},
		Localhost: Probe{Err: "refused"},
	}
	if f.Mismatch() {
		t.Fatal("both down is T405, not T710")
	}
}

func TestFleetOneSidedIsMismatch(t *testing.T) {
	f := Fleet{
		Loopback:  Probe{OK: true, Fingerprint: "jevons"},
		Localhost: Probe{Err: "refused"},
	}
	if !f.Mismatch() {
		t.Fatal("localhost unreachable while loopback serves is a mismatch")
	}
}

func TestAlarmTextComposes(t *testing.T) {
	c := Classify([]Listener{
		{PID: 1, Cmd: "jevonsd", Addr: "127.0.0.1:1"},
		{PID: 2, Cmd: "jevond", Addr: "*:1"},
	})
	f := Fleet{
		Loopback:  Probe{OK: true, Fingerprint: "a"},
		Localhost: Probe{OK: true, Fingerprint: "b"},
	}
	text := AlarmText(c, f)
	if !strings.Contains(text, "2") || !strings.Contains(text, "disagree") {
		t.Fatalf("composed alarm: %q", text)
	}
	if AlarmText(Conflict{}, Fleet{}) != "" {
		t.Fatal("quiet must be empty")
	}
}

func TestProbeAgentsHitsBothHosts(t *testing.T) {
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agents" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]string{{"name": "jevons"}})
	}))
	t.Cleanup(loop.Close)
	u := strings.TrimPrefix(loop.URL, "http://")
	host, portStr, ok := strings.Cut(u, ":")
	if !ok || host != "127.0.0.1" {
		t.Fatalf("httptest URL %q", loop.URL)
	}
	port, _ := strconv.Atoi(portStr)
	f := ProbeAgentsWith(loop.Client(), port)
	if !f.Loopback.OK {
		t.Fatalf("loopback: %+v", f.Loopback)
	}
	if f.Loopback.Fingerprint != "jevons" {
		t.Fatalf("fingerprint %q", f.Loopback.Fingerprint)
	}
}

func TestWatchLoopNotifiesThenQuiets(t *testing.T) {
	var listed []Listener
	list := func(int) ([]Listener, error) { return listed, nil }
	prober := func(int) Fleet {
		return Fleet{
			Loopback:  Probe{OK: true, Fingerprint: "a"},
			Localhost: Probe{OK: true, Fingerprint: "a"},
		}
	}
	var got []string
	notify := func(subject, kind, text string) bool {
		if subject != NoticeSubject || kind != NoticeKind {
			t.Errorf("notice %s/%s", subject, kind)
		}
		got = append(got, text)
		return true
	}
	listed = []Listener{
		{PID: 1, Cmd: "jevonsd", Addr: "127.0.0.1:9"},
		{PID: 99, Cmd: "jevond", Addr: "*:9"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		WatchLoopInspect(ctx, InspectArgs{Port: 9, List: list, Prober: prober}, time.Hour, notify)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(got) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(got) != 1 || !strings.Contains(got[0], "99") {
		t.Fatalf("first notify: %q", got)
	}
	listed = []Listener{{PID: 1, Cmd: "jevonsd", Addr: "127.0.0.1:9"}}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WatchLoop did not return")
	}
}

func TestLiveStubWildcardNamesPid(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	stubPID, stop := startWildcardStub(t, port)
	defer stop()

	var listeners []Listener
	var c Conflict
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		listeners, err = ListLSOF(port)
		if err != nil {
			t.Fatalf("ListLSOF: %v", err)
		}
		c = Classify(listeners)
		if !c.Quiet() && strings.Contains(c.Text(), strconv.Itoa(stubPID)) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if c.Quiet() {
		t.Fatalf("live * stub must conflict, listeners=%+v", listeners)
	}
	if !strings.Contains(c.Text(), strconv.Itoa(stubPID)) {
		t.Fatalf("must name squatting pid %d, got %q (listeners=%+v)", stubPID, c.Text(), listeners)
	}
	stop()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		listeners, err = ListLSOF(port)
		if err != nil {
			t.Fatalf("ListLSOF after close: %v", err)
		}
		c = Classify(listeners)
		if c.Quiet() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("after removing stub want quiet, got %q listeners=%+v", c.Text(), listeners)
}

func startWildcardStub(t *testing.T, port int) (pid int, stop func()) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required to hold a * listener in another pid")
	}
	script := fmt.Sprintf(`
import socket, time, os, sys
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("0.0.0.0", %d))
s.listen(1)
sys.stdout.write(str(os.getpid()) + "\n")
sys.stdout.flush()
time.sleep(60)
`, port)
	cmd := exec.Command("python3", "-c", script)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Skipf("wildcard stub: %v", err)
	}
	buf := make([]byte, 32)
	n, err := out.Read(buf)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Skipf("kernel refused * beside 127.0.0.1: %v", err)
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("stub pid: %v (%q)", err, buf[:n])
	}
	var stopped bool
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	return pid, stop
}

func TestParseLSOF(t *testing.T) {
	got := parseLSOF("p100\ncjevonsd\nn127.0.0.1:13705\np200\ncjevond\nn*:13705\n")
	if len(got) != 2 || got[0].PID != 100 || got[1].Addr != "*:13705" {
		t.Fatalf("%+v", got)
	}
}
