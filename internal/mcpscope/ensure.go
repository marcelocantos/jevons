// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpscope

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/marcelocantos/jevons/internal/mcphealth"
)

// ProbeTimeout bounds the reachability dial. Short: the answer is only ever
// used to choose between two sentences, and a diagnosis that takes long
// enough to be skipped is a diagnosis nobody reads.
const ProbeTimeout = 700 * time.Millisecond

// ConfigPath is the Claude Code config every agent of that provider reads at
// start. Empty when the home directory cannot be determined, which callers
// must treat as "cannot tell" rather than "not registered".
func ConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude.json")
}

// Probe establishes whether anything is listening at the endpoint.
//
// It opens and immediately closes a TCP connection and does not speak MCP,
// for the reason 🎯T379 gives: reachability is the question, and a protocol
// handshake would make a slow-but-real server look broken. Classification is
// mcphealth's, so refused and timed-out cannot come apart between the two
// packages that both have to distinguish them.
func Probe(endpoint string, timeout time.Duration) (Reach, string) {
	addr, err := mcphealth.DialAddress(endpoint)
	if err != nil {
		return ReachUnknown, err.Error()
	}
	if timeout <= 0 {
		timeout = ProbeTimeout
	}
	conn, dialErr := net.DialTimeout("tcp", addr, timeout)
	if conn != nil {
		_ = conn.Close()
	}
	status, detail := mcphealth.ClassifyDialErr(dialErr)
	switch status {
	case mcphealth.StatusLive:
		return ReachUp, detail
	case mcphealth.StatusDead:
		return ReachDown, detail
	default:
		// StatusSlow and StatusUnknown both mean the dial proved nothing.
		// Neither may become ReachDown: that is the single mapping which,
		// if widened, reintroduces the false outage report.
		return ReachUnknown, detail
	}
}

// Diagnosis is a whole answer: what was found, what was probed, and the
// sentence to say about it.
type Diagnosis struct {
	Server   string
	Cwd      string
	Endpoint string
	Scope    Scope
	Reach    Reach
	Detail   string
	Verdict  Verdict
	Message  string
	// LocalDirs are the directories that register this server in local
	// scope, named so a reader can see the shape of the misconfiguration
	// rather than inferring it.
	LocalDirs []string
}

// DiagnoseSession reads the config, probes the endpoint, and returns the
// whole answer for a session running in cwd.
//
// A config that cannot be read yields VerdictUnknown with the read error in
// Detail, never VerdictOutOfScope: those differ in what the reader should do
// next, and this package's entire reason for existing is that two states
// which feel the same from inside must not be reported the same.
func DiagnoseSession(configPath, server, cwd, endpoint string) Diagnosis {
	d := Diagnosis{Server: server, Cwd: cwd, Endpoint: endpoint}
	d.Reach, d.Detail = Probe(endpoint, ProbeTimeout)

	data, err := os.ReadFile(configPath)
	if err != nil {
		d.Scope = ScopeNone
		d.Verdict = VerdictUnknown
		d.Message = fmt.Sprintf("cannot read %s (%v), so registration for %s is undetermined; the endpoint %s probed %s. No outage may be reported on this evidence.",
			configPath, err, server, endpoint, d.Reach)
		return d
	}
	scope, _, err := FindScope(data, server, cwd)
	if err != nil {
		d.Scope = ScopeNone
		d.Verdict = VerdictUnknown
		d.Message = fmt.Sprintf("cannot parse %s (%v), so registration for %s is undetermined; the endpoint %s probed %s. No outage may be reported on this evidence.",
			configPath, err, server, endpoint, d.Reach)
		return d
	}
	d.Scope = scope
	d.LocalDirs, _ = LocalScopeDirs(data, server)
	d.Verdict = Diagnose(scope, d.Reach)
	d.Message = Explain(d.Verdict, server, cwd, endpoint)
	return d
}

// WriteEnsure registers server in the user scope of the Claude config,
// reporting whether it had to change anything.
//
// ~/.claude.json is shared hot state (🎯T376): every concurrent agent reads
// and writes it, and a full-file write derived from a stale read would drop
// whatever another session registered in between. Three things keep this
// honest, in the order they matter:
//
//  1. The common case does not write. When the entry is already present and
//     equal the function returns before opening anything, so a file this
//     contended is touched only when there is a repair to make.
//  2. The read that a write is derived from happens INSIDE the lock. The
//     unlocked check above is a fast path and is deliberately re-done, so
//     nothing here is computed from bytes that could have gone stale.
//  3. The edit is a single key add, not a document replacement, so
//     re-applying it to freshly read bytes is always the right merge.
//
// The residual, declared rather than hidden: Claude Code itself does not
// take this lock, so a write it performs between the read and the rename
// here is still lost. The window is the few hundred microseconds of a repair
// that almost never runs, against a failure mode that silently removes fleet
// control from every agent outside one directory. Where the vendor grows a
// supported concurrent-safe write, this should defer to it.
func WriteEnsure(configPath, server string, entry Entry) (changed bool, err error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", configPath, err)
	}
	if _, changed, err := EnsureUserScope(data, server, entry); err != nil {
		return false, err
	} else if !changed {
		return false, nil
	}

	unlock, err := lockFile(configPath + ".jevons-lock")
	if err != nil {
		return false, err
	}
	defer unlock()

	// Re-read under the lock: everything below is derived from these bytes,
	// not from the fast-path read above.
	fresh, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("re-read %s: %w", configPath, err)
	}
	out, changed, err := EnsureUserScope(fresh, server, entry)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if err := writeAtomic(configPath, out); err != nil {
		return false, err
	}
	return true, nil
}

// WriteRemove deletes server from the user-scope `mcpServers` map of the
// Claude (or Cursor-shaped) JSON config. Same lock + re-read as WriteEnsure.
func WriteRemove(configPath, server string) (changed bool, err error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", configPath, err)
	}
	if _, changed, err := RemoveUserScope(data, server); err != nil {
		return false, err
	} else if !changed {
		return false, nil
	}

	unlock, err := lockFile(configPath + ".jevons-lock")
	if err != nil {
		return false, err
	}
	defer unlock()

	fresh, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("re-read %s: %w", configPath, err)
	}
	out, changed, err := RemoveUserScope(fresh, server)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if err := writeAtomic(configPath, out); err != nil {
		return false, err
	}
	return true, nil
}

// lockFile takes an exclusive advisory lock and returns its release. The
// lock lives beside the config rather than on it, so that a stale lock can
// be removed without anyone being tempted to remove the config.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// writeAtomic replaces path in one rename, preserving its mode. A partial
// write here would leave the owner's Claude config truncated, which is a
// worse outage than the one this package fixes.
func writeAtomic(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create temp beside %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", path, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod temp for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename onto %s: %w", path, err)
	}
	return nil
}

// ErrNoConfigPath is returned when the home directory cannot be resolved, so
// callers report "cannot tell" rather than "not registered".
var ErrNoConfigPath = errors.New("cannot determine the Claude config path")
