// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

// Package brokerfixture runs a real Claudia broker in its own process for
// Jevons tests of the control-plane pair (🎯T1074.9). The broker owns
// claudia-control.db in its own state directory and serves the control
// socket beside its broker socket; the test process keeps Jevons' own
// control-replica.db and links to that socket. No test process opens the
// broker's file, which is the topology production runs.
//
// Only tests import this package. jevonsd and the other binaries under
// cmd/ do not, and scripts/docratchet ratchets that the broker's file stays
// out of their reach.
package brokerfixture

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

const fixtureMain = "github.com/marcelocantos/jevons/internal/brokerfixture/fixturebroker"

// startTimeout bounds one broker start or stop; replyTimeout one command.
const (
	startTimeout = 60 * time.Second
	replyTimeout = 30 * time.Second
)

// Broker is one fixture broker. Restart keeps its state directory and
// socket, as a supervised broker coming back would.
type Broker struct {
	t        testing.TB
	bin      string
	StateDir string // the broker's state directory; holds claudia-control.db
	Socket   string // the broker socket; the control socket sits beside it
	logPath  string

	cmd     *exec.Cmd
	stdin   io.WriteCloser
	replies chan string
	exited  chan error
}

// Start builds the fixture broker from the Claudia module this test binary
// was built against (GOWORK, GOFLAGS and the cache are inherited) and starts
// it on a fresh state directory and socket. Cleanup stops it.
func Start(t testing.TB) *Broker {
	t.Helper()
	work := t.TempDir()
	bin := filepath.Join(work, "fixturebroker")
	build := exec.Command("go", "build", "-tags", "jevons_claudia_next", "-o", bin, fixtureMain)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixture broker: %v\n%s", err, out)
	}
	// A unix socket path is limited to ~104 bytes; t.TempDir() on macOS is
	// longer, so the socket lives in a short directory under /tmp.
	sockDir, err := os.MkdirTemp("/tmp", "jbf-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	b := &Broker{
		t:        t,
		bin:      bin,
		StateDir: filepath.Join(work, "state"),
		Socket:   filepath.Join(sockDir, "broker.sock"),
		logPath:  filepath.Join(work, "broker.log"),
	}
	if err := os.MkdirAll(b.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b.Stop()
		if t.Failed() {
			if body, err := os.ReadFile(b.logPath); err == nil {
				t.Logf("fixture broker log:\n%s", body)
			}
		}
	})
	b.start()
	return b
}

// ControlSocket is the socket a consumer's Replica.Link dials.
func (b *Broker) ControlSocket() string { return claudia.ControlSocketPath(b.Socket) }

// ControlFile is the broker's own control file. Tests name it to ask who
// holds it open; they never open it.
func (b *Broker) ControlFile() string { return filepath.Join(b.StateDir, claudia.ClaudiaDBFile) }

// Pid is the running broker's process id, or 0 when it is stopped.
func (b *Broker) Pid() int {
	if b.cmd == nil || b.cmd.Process == nil {
		return 0
	}
	return b.cmd.Process.Pid
}

func (b *Broker) start() {
	b.t.Helper()
	logFile, err := os.OpenFile(b.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		b.t.Fatal(err)
	}
	defer logFile.Close()
	cmd := exec.Command(b.bin, "-state-dir", b.StateDir, "-socket", b.Socket)
	cmd.Env = brokerEnv(os.Environ(), b.StateDir, b.Socket)
	cmd.Stderr = logFile
	stdin, err := cmd.StdinPipe()
	if err != nil {
		b.t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		b.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		b.t.Fatal(err)
	}
	b.cmd, b.stdin = cmd, stdin
	b.replies, b.exited = make(chan string, 16), make(chan error, 1)
	replies := b.replies
	go func() {
		out := bufio.NewScanner(stdout)
		for out.Scan() {
			replies <- out.Text()
		}
		close(replies)
	}()
	exited := b.exited
	go func() { exited <- cmd.Wait() }()
	ready := b.reply(startTimeout)
	if ready != "READY "+strconv.Itoa(cmd.Process.Pid) {
		body, _ := os.ReadFile(b.logPath)
		b.t.Fatalf("fixture broker did not start (%q):\n%s", ready, body)
	}
}

// brokerEnv is base with any inherited broker routing replaced by this
// broker's own: its socket, and its state directory as XDG_STATE_HOME, so
// nothing it does can reach the machine's broker or state.
func brokerEnv(base []string, stateDir, socket string) []string {
	env := make([]string, 0, len(base)+3)
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "CLAUDIA_BROKER_SOCKET", "CLAUDIA_NO_BROKER", "XDG_STATE_HOME":
			continue
		}
		env = append(env, entry)
	}
	return append(env, "CLAUDIA_BROKER_SOCKET="+socket, "CLAUDIA_NO_BROKER=0", "XDG_STATE_HOME="+stateDir)
}

func (b *Broker) reply(d time.Duration) string {
	select {
	case line, ok := <-b.replies:
		if !ok {
			return "<broker stdout closed>"
		}
		return line
	case <-time.After(d):
		return "<no reply>"
	}
}

func (b *Broker) send(cmd any) {
	b.t.Helper()
	if b.cmd == nil {
		b.t.Fatal("fixture broker is stopped")
	}
	line, err := json.Marshal(cmd)
	if err != nil {
		b.t.Fatal(err)
	}
	if _, err := fmt.Fprintf(b.stdin, "%s\n", line); err != nil {
		b.t.Fatalf("fixture broker command: %v", err)
	}
	if got := b.reply(replyTimeout); got != "OK" {
		b.t.Fatalf("fixture broker command %s: %s", line, got)
	}
}

// PublishUsage has the broker record one usage fact, through the seat
// pump's publish path, into its own file.
func (b *Broker) PublishUsage(fact claudia.UsageFact) {
	b.t.Helper()
	b.send(map[string]any{"op": "usage", "usage": fact})
}

// PublishReceipt has the broker record one request receipt in its own file.
func (b *Broker) PublishReceipt(receipt claudia.RequestReceipt) {
	b.t.Helper()
	b.send(map[string]any{"op": "receipt", "receipt": receipt})
}

// Stop closes the broker the way a supervisor's SIGINT does: started
// obligations are settled and the file and the socket released. A stopped
// broker stays stopped.
func (b *Broker) Stop() {
	b.t.Helper()
	if b.cmd == nil {
		return
	}
	cmd := b.cmd
	b.cmd = nil
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case err := <-b.exited:
		if err != nil {
			b.t.Errorf("fixture broker exit: %v", err)
		}
	case <-time.After(startTimeout):
		_ = cmd.Process.Kill()
		<-b.exited
		b.t.Errorf("fixture broker did not stop on SIGINT")
	}
	_ = b.stdin.Close()
}

// Restart stops the broker if it is running and starts it again on the
// same state directory and socket: a new process, the same broker.
func (b *Broker) Restart() {
	b.t.Helper()
	b.Stop()
	b.start()
}

// Holders lists the processes holding path open, by lsof, sorted. lsof
// exits 1 when no process holds it; that is an empty list, not an error.
func Holders(path string) ([]int, error) {
	out, err := exec.Command("lsof", "-t", "--", path).Output()
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 1 || len(out) != 0 {
			return nil, fmt.Errorf("lsof %s: %w", path, err)
		}
	}
	var pids []int
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("lsof %s: pid %q", path, field)
		}
		pids = append(pids, pid)
	}
	slices.Sort(pids)
	return pids, nil
}

// AwaitLinkEstablished waits until state reports an established broker link
// with an epoch newer than after, and returns that epoch. state is a
// replica's link state (cost.ReplicaSource.LinkState, unpacked: this package
// cannot import cost, whose own tests import it).
//
// It is the precondition consumer tests put before a broker write that
// follows a (re)dial. Claudia's link mishandles a broker write that lands
// during the handshake: the row is delivered in the diff and again as a
// changeset, the consumer's apply aborts on the conflict, and after the
// redial the broker can stop sending changesets on that link. Those defects
// are reported with 🎯T1074.9 and reproduced, unguarded, by cmd/jevonsd
// TestT10749BrokerWriteDuringHandshakeKeepsFlowing. Remove this
// precondition once the Claudia link is fixed.
func AwaitLinkEstablished(t testing.TB, state func() (linked bool, epoch uint64), after uint64) uint64 {
	t.Helper()
	deadline := time.Now().Add(startTimeout)
	for {
		linked, epoch := state()
		if linked && epoch > after {
			return epoch
		}
		if time.Now().After(deadline) {
			t.Fatalf("no broker link established after epoch %d", after)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
