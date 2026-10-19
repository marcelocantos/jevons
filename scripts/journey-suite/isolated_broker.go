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
	"syscall"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/omp"
	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

// A subscription sidecar seat needs Claudia's credential shot at broker
// startup. The isolate owns that broker and its sidecar; migration journeys
// get a second disposable broker so they cannot change the primary fleet.
type isolatedBroker struct {
	cmd    *exec.Cmd
	done   chan error
	log    *os.File
	root   string
	socket string
}

func journeyNeedsBroker(provider claudia.Provider) bool {
	switch provider {
	case claudia.ProviderGrok, claudia.ProviderCursor,
		claudia.Provider(omp.Anthropic), claudia.Provider(omp.OpenAICodex),
		claudia.Provider(omp.XAIOAuth):
		return true
	default:
		return false
	}
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
		// 🎯T1082: this child's agents are scanned before their store goes.
		guardErr := s.devGuard.noteStore(state)
		result = errors.Join(result, stopErr, brokerErr, logErr, guardErr, os.RemoveAll(state))
	}()
	b, err = child.startIsolatedBroker()
	if err != nil {
		return err
	}
	child.brokerSocket = b.socket
	child.broker = b
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
	sidecarScript, err := s.stageIsolatedSidecar()
	if err != nil {
		cleanup()
		return nil, err
	}
	s.daemonEnv = append(s.daemonEnv, "CLAUDIA_OMP_SERVER="+sidecarScript)
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
	// The broker creates and owns its control file (🎯T1074.9). The
	// isolate daemon keeps its own control-replica.db in its own state
	// directory and links to claudia.ControlSocketPath(socket); nothing
	// here opens, creates or links the broker's file.
	cmd := isolatedBrokerCommand(os.Environ(), bin, root, s.stateDir, sidecarScript)
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

// A clean checkout has no sidecar/node_modules. Copy only the locked source
// into the journey state and install there, so Bun cannot silently resolve a
// different version from its global cache and the test never mutates a repo.
//
// patches/ is part of that lock. package.json names those files, and
// `bun install --frozen-lockfile` refuses to start when they are absent.
// node_modules and other directories stay behind.
func (s *suite) stageIsolatedSidecar() (string, error) {
	if override := strings.TrimSpace(os.Getenv("CLAUDIA_OMP_SERVER")); override != "" {
		return override, nil
	}
	src := filepath.Dir(omp.ServerScript())
	dst := filepath.Join(s.stateDir, "omp-sidecar")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return "", fmt.Errorf("stage isolated sidecar: %w", err)
	}
	if err := copyIsolatedSidecarSource(src, dst); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	install := exec.CommandContext(ctx, "bun", "install", "--frozen-lockfile")
	install.Dir = dst
	if out, err := install.CombinedOutput(); err != nil {
		return "", fmt.Errorf("install isolated Claudia sidecar: %w: %s", err, trim(string(out), 500))
	}
	script := filepath.Join(dst, "server.ts")
	if _, err := os.Stat(script); err != nil {
		return "", fmt.Errorf("isolated Claudia sidecar script: %w", err)
	}
	return script, nil
}

// copyIsolatedSidecarSource copies the locked sidecar inputs. Top-level
// TypeScript, the package manifest, and either lockfile name are included.
// patches/ is the only directory: those files are named by the manifest.
func copyIsolatedSidecarSource(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read Claudia sidecar source: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		from := filepath.Join(src, name)
		to := filepath.Join(dst, name)
		if entry.IsDir() {
			if name != "patches" {
				continue
			}
			if err := os.MkdirAll(to, 0o700); err != nil {
				return fmt.Errorf("stage Claudia sidecar patches: %w", err)
			}
			patches, err := os.ReadDir(from)
			if err != nil {
				return fmt.Errorf("read Claudia sidecar patches: %w", err)
			}
			for _, patch := range patches {
				if patch.IsDir() {
					continue
				}
				if err := copySidecarFile(filepath.Join(from, patch.Name()), filepath.Join(to, patch.Name())); err != nil {
					return err
				}
			}
			continue
		}
		if name != "package.json" && name != "bun.lock" && name != "bun.lockb" && !strings.HasSuffix(name, ".ts") {
			continue
		}
		if err := copySidecarFile(from, to); err != nil {
			return err
		}
	}
	return nil
}

func copySidecarFile(src, dst string) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read Claudia sidecar %s: %w", filepath.Base(src), err)
	}
	if err := os.WriteFile(dst, body, 0o600); err != nil {
		return fmt.Errorf("stage Claudia sidecar %s: %w", filepath.Base(dst), err)
	}
	return nil
}

// snapshotControlFile copies the broker control file, including any WAL
// siblings, into dest, after the broker has stopped. A missing file is not
// an error: a broker that failed before opening it never created one.
func snapshotControlFile(root, dest string) error {
	state := filepath.Join(root, "state")
	entries, err := os.ReadDir(state)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), claudia.ClaudiaDBFile) {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return nil
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(state, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dest, name), body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// isolatedBrokerCommand configures the separately launched broker, including
// consumer settings that the Jevons client process cannot supply afterwards.
func isolatedBrokerCommand(base []string, bin, root, stateDir, sidecarScript string) *exec.Cmd {
	socket := filepath.Join(root, "broker.sock")
	cmd := exec.Command(bin, "broker", "serve", "-state-dir", filepath.Join(root, "state"),
		"-socket", socket, "-no-resume", "-restart-nudge", "-",
		"-mcp-seed-dir", stateDir, "-mcp-consumer-prefix", "jevonsmcp")
	cmd.Env = isolatedBrokerEnv(base, socket, filepath.Join(root, "omp.sock"), sidecarScript, root, filepath.Join(stateDir, "spool"))
	return cmd
}

// isolatedBrokerEnv builds the environment for a journey's isolated Claudia
// broker: base with any prior isolate/broker vars stripped, plus this run's
// own socket, sidecar and state paths.
//
// 🎯T940: omp.DefaultDataPath does not follow XDG_STATE_HOME (the Keychain
// key is per user, not per process env), so this broker's Anthropic /
// OpenAI-Codex / Cursor / xAI plan login is the real, shared one a
// production broker also holds. CLAUDIA_OMP_NO_REFRESH refuses every
// login/refresh call against the OAuth provider, so this broker can only
// ever use whatever unexpired access token that shared item already holds
// — it can never rotate (and so invalidate) the shared refresh token, even
// racing a production broker's own refresh.
func isolatedBrokerEnv(base []string, socket, ompSocket, sidecarScript, stateHome, spoolDir string) []string {
	strip := []string{
		"CLAUDIA_BROKER_SOCKET=", "CLAUDIA_NO_BROKER=", "CLAUDIA_OMP_SOCKET=",
		"CLAUDIA_OMP_SERVER=", "JEVONS_SPOOL_DIR=", "CLAUDIA_OMP_SPOOL_DIR=", "XDG_STATE_HOME=", "CLAUDIA_OMP_NO_REFRESH=",
	}
	env := make([]string, 0, len(base)+6)
	for _, entry := range base {
		stripped := false
		for _, prefix := range strip {
			if strings.HasPrefix(entry, prefix) {
				stripped = true
				break
			}
		}
		if !stripped {
			env = append(env, entry)
		}
	}
	return append(env, "CLAUDIA_BROKER_SOCKET="+socket, "CLAUDIA_NO_BROKER=0",
		"CLAUDIA_OMP_SOCKET="+ompSocket, "CLAUDIA_OMP_SERVER="+sidecarScript, "XDG_STATE_HOME="+stateHome,
		"JEVONS_SPOOL_DIR="+spoolDir, "CLAUDIA_OMP_SPOOL_DIR="+spoolDir, "CLAUDIA_OMP_NO_REFRESH=1")
}

// controlFile is the broker's own control-plane file. Only the broker opens
// it as a database; the journey reads byte copies (copyControlFile).
func (b *isolatedBroker) controlFile() string {
	return filepath.Join(b.root, "state", claudia.ClaudiaDBFile)
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
	// The broker root is deleted below. The control file is the only record
	// of whether the seat pump wrote a receipt. Keep it beside the daemon
	// log, which the suite retains when -keep is set.
	stopped = errors.Join(stopped, snapshotControlFile(b.root, filepath.Join(filepath.Dir(b.log.Name()), "control-snapshot")))
	if body, err := os.ReadFile(filepath.Join(b.root, "omp-sidecar.log")); err == nil {
		stopped = errors.Join(stopped, os.WriteFile(filepath.Join(filepath.Dir(b.log.Name()), "omp-sidecar.log"), body, 0o600))
	}
	stopped = errors.Join(stopped, stopIsolatedSidecar(filepath.Join(b.root, "omp.sock"), sidecarExitWait))
	killPrivateClaudiaTmux(b.root)
	stopped = errors.Join(stopped, b.log.Close(), os.RemoveAll(b.root))
	return stopped
}

// sidecarExitWait bounds how long teardown waits for the broker's sidecar to
// follow the broker out before signalling it.
const sidecarExitWait = 10 * time.Second

// stopIsolatedSidecar makes sure no sidecar still serves socket once its
// broker has exited.
//
// The sidecar is the broker's child and exits when the broker does: its
// stdin is a lifeline only the broker holds (claudia 🎯T166). So by the
// time teardown gets here it is usually already gone, or going, and the pid
// file it wrote is left behind naming a dead process. Signalling that pid
// unconditionally (omp.StopSidecar) then fails with "os: process already
// finished" — the J38 teardown failure of gate 252cf6ac, after the journey's
// own checks had passed — and could signal an unrelated process if the pid
// had been reused. The socket's lock is the evidence instead: a sidecar holds
// it for its whole life and the kernel drops it on exit. Only a sidecar that
// still holds it after the wait is signalled.
func stopIsolatedSidecar(socket string, wait time.Duration) error {
	if released, err := waitSidecarLockFree(socket, wait); err != nil || released {
		return err
	}
	if err := omp.StopSidecar(socket); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	released, err := waitSidecarLockFree(socket, wait)
	if err != nil {
		return err
	}
	if !released {
		return fmt.Errorf("isolated Claudia sidecar still holds %s after SIGTERM", socket)
	}
	return nil
}

// waitSidecarLockFree polls the sidecar's socket lock until no process holds
// it or wait elapses. A free lock means no sidecar serves the socket,
// whatever the pid file says.
func waitSidecarLockFree(socket string, wait time.Duration) (bool, error) {
	deadline := time.Now().Add(wait)
	for {
		free, err := sidecarLockFree(socket)
		if err != nil || free {
			return free, err
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// sidecarLockFree takes the socket's lock and releases it at once, so it
// never keeps a sidecar out.
func sidecarLockFree(socket string) (bool, error) {
	f, err := os.OpenFile(socket+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, fmt.Errorf("sidecar lock: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil
		}
		return false, fmt.Errorf("sidecar lock: %w", err)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return true, nil
}
