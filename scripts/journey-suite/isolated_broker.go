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
	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
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

func (s *suite) withIsolatedBroker(run func(*suite) error) (result error) {
	// A direct Codex session cannot be adopted as a subscription sidecar
	// session. Run this journey on a second port and state tree so the
	// surrounding direct-mode suite never changes provider or registry.
	state, err := os.MkdirTemp("", "jevons-broker-journey-")
	if err != nil {
		return err
	}
	port, err := freePort()
	if err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	if err := portguard.RefuseDevelopment(port); err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	config, err := os.ReadFile(s.cfgPath)
	if err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	configText := strings.ReplaceAll(string(config), s.stateDir, state)
	oldPort := fmt.Sprintf("port: %d\n", s.port)
	if !strings.Contains(configText, oldPort) {
		_ = os.RemoveAll(state)
		return fmt.Errorf("migration isolate config has no port %d", s.port)
	}
	configText = strings.Replace(configText, oldPort, fmt.Sprintf("port: %d\n", port), 1)
	if err := os.WriteFile(filepath.Join(state, "config.yaml"), []byte(configText), 0o600); err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	logFile, err := os.Create(filepath.Join(state, "jevonsd.log"))
	if err != nil {
		_ = os.RemoveAll(state)
		return err
	}
	child := &suite{
		host: fmt.Sprintf("127.0.0.1:%d", port), stateDir: state, provider: s.provider,
		port: port, cfgPath: filepath.Join(state, "config.yaml"),
		logPath: filepath.Join(state, "jevonsd.log"), logFile: logFile,
		workdir: state, daemonBin: s.daemonBin,
		daemonEnv: append([]string(nil), s.daemonEnv...),
	}
	var b *isolatedBroker
	defer func() {
		stopErr := child.signalStop(8 * time.Second)
		brokerErr := b.close()
		if result != nil {
			for _, name := range []string{"jevonsd.log", "claudia-broker.log", "omp-sidecar.log"} {
				if body, err := os.ReadFile(filepath.Join(state, name)); err == nil {
					_ = os.WriteFile(filepath.Join(s.stateDir, "migration-"+name), body, 0o600)
				}
			}
		}
		logErr := logFile.Close()
		result = errors.Join(result, stopErr, brokerErr, logErr, os.RemoveAll(state))
	}()
	b, err = child.startIsolatedBroker()
	if err != nil {
		return err
	}
	child.brokerSocket = b.socket
	if err := child.startDaemon(); err != nil {
		return fmt.Errorf("start brokered isolate: %w", err)
	}
	return run(child)
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
			strings.HasPrefix(entry, "JEVONS_SPOOL_DIR=") ||
			strings.HasPrefix(entry, "XDG_STATE_HOME=") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "CLAUDIA_BROKER_SOCKET="+socket, "CLAUDIA_NO_BROKER=0",
		"CLAUDIA_OMP_SOCKET="+ompSocket, "XDG_STATE_HOME="+root,
		"JEVONS_SPOOL_DIR="+filepath.Join(s.stateDir, "spool"))
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
