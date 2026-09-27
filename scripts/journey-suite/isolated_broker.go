// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/claudia/omp"
)

// A live migration needs Claudia's disposable summary seat, which deliberately
// runs through a broker. Keep that broker and its sidecar separate from the
// owner's services; the rest of the suite still exercises direct-mode drains.
type isolatedBroker struct {
	cmd    *exec.Cmd
	done   chan error
	log    *os.File
	root   string
	socket string
}

func (s *suite) withIsolatedBroker(run func() error) (result error) {
	if err := s.signalStop(8 * time.Second); err != nil {
		return fmt.Errorf("stop direct-mode isolate: %w", err)
	}
	// A direct Codex or Grok session cannot be adopted as the subscription
	// sidecar's session. Give the brokered journey a fresh registry and workdir,
	// then restore the surrounding suite's direct-mode state afterward.
	oldState, oldConfig, oldLogPath, oldLog, oldWork := s.stateDir, s.cfgPath, s.logPath, s.logFile, s.workdir
	state, err := os.MkdirTemp("", "jevons-broker-journey-")
	if err != nil {
		return errors.Join(err, s.startDaemon())
	}
	config, err := os.ReadFile(oldConfig)
	if err != nil {
		_ = os.RemoveAll(state)
		return errors.Join(err, s.startDaemon())
	}
	config = []byte(strings.ReplaceAll(string(config), oldState, state))
	if err := os.WriteFile(filepath.Join(state, "config.yaml"), config, 0o600); err != nil {
		_ = os.RemoveAll(state)
		return errors.Join(err, s.startDaemon())
	}
	logFile, err := os.Create(filepath.Join(state, "jevonsd.log"))
	if err != nil {
		_ = os.RemoveAll(state)
		return errors.Join(err, s.startDaemon())
	}
	s.stateDir, s.cfgPath, s.logPath, s.logFile, s.workdir =
		state, filepath.Join(state, "config.yaml"), filepath.Join(state, "jevonsd.log"), logFile, state
	var b *isolatedBroker
	defer func() {
		stopErr := s.signalStop(8 * time.Second)
		s.brokerSocket = ""
		brokerErr := b.close()
		if result != nil {
			for _, name := range []string{"jevonsd.log", "claudia-broker.log", "omp-sidecar.log"} {
				if body, err := os.ReadFile(filepath.Join(state, name)); err == nil {
					_ = os.WriteFile(filepath.Join(oldState, "migration-"+name), body, 0o600)
				}
			}
		}
		logErr := logFile.Close()
		s.stateDir, s.cfgPath, s.logPath, s.logFile, s.workdir =
			oldState, oldConfig, oldLogPath, oldLog, oldWork
		restartErr := s.startDaemon()
		result = errors.Join(result, stopErr, brokerErr, logErr, restartErr, os.RemoveAll(state))
	}()
	b, err = s.startIsolatedBroker()
	if err != nil {
		return err
	}
	s.brokerSocket = b.socket
	if err := s.startDaemon(); err != nil {
		return fmt.Errorf("start brokered isolate: %w", err)
	}
	return run()
}

func (s *suite) startIsolatedBroker() (*isolatedBroker, error) {
	root, err := os.MkdirTemp("/tmp", "jevons-broker-")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	bin := filepath.Join(s.stateDir, "claudia-broker")
	buildCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(buildCtx, "go", "build", "-o", bin, "github.com/marcelocantos/claudia/cmd/claudia")
	output, err := build.CombinedOutput()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("build isolated Claudia broker: %w: %s", err, trim(string(output), 500))
	}
	logFile, err := os.Create(filepath.Join(s.stateDir, "claudia-broker.log"))
	if err != nil {
		cleanup()
		return nil, err
	}
	socket := filepath.Join(root, "broker.sock")
	ompSocket := filepath.Join(root, "omp.sock")
	cmd := exec.Command(bin, "broker", "serve", "-state-dir", filepath.Join(root, "state"),
		"-socket", socket, "-no-resume", "-restart-nudge", "-")
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "CLAUDIA_BROKER_SOCKET=") ||
			strings.HasPrefix(entry, "CLAUDIA_NO_BROKER=") ||
			strings.HasPrefix(entry, "CLAUDIA_OMP_SOCKET=") ||
			strings.HasPrefix(entry, "XDG_STATE_HOME=") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "CLAUDIA_BROKER_SOCKET="+socket, "CLAUDIA_NO_BROKER=0",
		"CLAUDIA_OMP_SOCKET="+ompSocket, "XDG_STATE_HOME="+root)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		cleanup()
		return nil, err
	}
	b := &isolatedBroker{cmd: cmd, done: make(chan error, 1), log: logFile, root: root, socket: socket}
	go func() { b.done <- cmd.Wait() }()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-b.done:
			_ = logFile.Close()
			body, _ := os.ReadFile(filepath.Join(s.stateDir, "claudia-broker.log"))
			cleanup()
			return nil, fmt.Errorf("isolated Claudia broker exited before ready: %v: %s", err, trim(string(body), 500))
		default:
		}
		conn, err := net.DialTimeout("unix", socket, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return b, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, errors.Join(fmt.Errorf("isolated Claudia broker did not listen at %s", socket), b.close())
}

func (b *isolatedBroker) close() error {
	if b == nil {
		return nil
	}
	var stopped error
	if b.cmd != nil && b.cmd.Process != nil {
		if err := b.cmd.Process.Signal(os.Interrupt); err != nil && !errors.Is(err, os.ErrProcessDone) {
			stopped = err
		}
		select {
		case err := <-b.done:
			stopped = errors.Join(stopped, err)
		case <-time.After(10 * time.Second):
			stopped = errors.Join(stopped, fmt.Errorf("isolated Claudia broker did not stop"))
			_ = b.cmd.Process.Kill()
			<-b.done
		}
	}
	if body, err := os.ReadFile(filepath.Join(b.root, "omp-sidecar.log")); err == nil {
		stopped = errors.Join(stopped, os.WriteFile(filepath.Join(filepath.Dir(b.log.Name()), "omp-sidecar.log"), body, 0o600))
	}
	if err := omp.StopSidecar(filepath.Join(b.root, "omp.sock")); err != nil && !errors.Is(err, os.ErrNotExist) {
		stopped = errors.Join(stopped, err)
	}
	stopped = errors.Join(stopped, b.log.Close(), os.RemoveAll(b.root))
	return stopped
}
