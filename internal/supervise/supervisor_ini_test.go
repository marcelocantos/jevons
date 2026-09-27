// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func supervisorDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "supervisor")
	if _, err := os.Stat(filepath.Join(dir, "install.sh")); err != nil {
		t.Fatalf("supervisor/ missing from repo: %v", err)
	}
	return dir
}

func TestSupervisorTemplatesAreVellumShaped(t *testing.T) {
	dir := supervisorDir(t)
	for _, name := range []string{"jevonsd.ini"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		s := string(body)
		for _, want := range []string{
			"@REPO@",
			"command=@REPO@/supervisor/run-",
			"directory=@REPO@",
			"autostart=true",
			"autorestart=true",
			"%(ENV_HOME)s/.local/var/log/",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("%s missing %q\n%s", name, want, s)
			}
		}
		if strings.Contains(s, "5173") || strings.Contains(s, "npm run") {
			t.Errorf("%s must not be the Vite :5173 agent", name)
		}
		if strings.Contains(s, "restart-jevonsd") {
			t.Errorf("%s must not invoke the fat restart script", name)
		}
	}
	d, err := os.ReadFile(filepath.Join(dir, "jevonsd.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d), "[program:jevonsd]") {
		t.Fatal("jevonsd.ini must name program:jevonsd")
	}
	if _, err := os.Stat(filepath.Join(dir, "jevons-broker.ini")); !os.IsNotExist(err) {
		t.Fatal("jevons-broker must have no installable template")
	}
	if _, err := os.Stat(filepath.Join(dir, "jevons-vanilla.ini")); !os.IsNotExist(err) {
		t.Fatal("retired vanilla program must have no installable template")
	}
}

func TestSupervisorInstallRendersRepoRoot(t *testing.T) {
	dir := supervisorDir(t)
	conf := t.TempDir()
	legacy := filepath.Join(conf, "jevons-vanilla.ini")
	if err := os.WriteFile(legacy, []byte("[program:jevons-vanilla]"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", filepath.Join(dir, "install.sh"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"SUPERVISOR_CONF_DIR="+conf,
		"SUPERVISOR_SKIP_CTL=1",
		"HOME="+t.TempDir(),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("installer retained legacy comparison config: %v", err)
	}
	repo := filepath.Clean(filepath.Join(dir, ".."))
	for _, name := range []string{"jevonsd.ini"} {
		body, err := os.ReadFile(filepath.Join(conf, name))
		if err != nil {
			t.Fatalf("rendered %s: %v", name, err)
		}
		s := string(body)
		if strings.Contains(s, "@REPO@") {
			t.Fatalf("%s still has @REPO@:\n%s", name, s)
		}
		if !strings.Contains(s, repo+"/supervisor/run-") {
			t.Fatalf("%s command not expanded to %s:\n%s", name, repo, s)
		}
	}
	jevonsd, _ := os.ReadFile(filepath.Join(conf, "jevonsd.ini"))
	if strings.Contains(string(jevonsd), "/opt/homebrew/opt/jevons") {
		t.Fatal("rendered jevonsd must not point at Cellar")
	}
	if _, err := os.Stat(filepath.Join(conf, "jevons-broker.ini")); !os.IsNotExist(err) {
		t.Fatal("installer retained a jevons-broker definition")
	}
}

// Retirement must not apply another program's pending configuration changes.
func TestSupervisorRetirementOnlyTouchesComparisonGroup(t *testing.T) {
	dir := supervisorDir(t)
	home, conf, fakeBin := t.TempDir(), t.TempDir(), t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	primary := filepath.Join(conf, "jevonsd.ini")
	if err := os.WriteFile(primary, []byte("existing owner config"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(conf, "jevons-vanilla.ini")
	if err := os.WriteFile(legacy, []byte("old comparison config"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"supervisorctl": "#!/bin/sh\nprintf 'supervisorctl %s\\n' \"$*\" >>\"$CALLS\"\nif [ \"$1\" = reread ]; then printf 'jevons-vanilla: disappeared\\njevonsd: changed\\nother: changed\\n'; fi\n",
		"launchctl":     "#!/bin/sh\nprintf 'launchctl %s\\n' \"$*\" >>\"$CALLS\"\n",
	} {
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("/bin/sh", filepath.Join(dir, "install.sh"))
	cmd.Env = append(os.Environ(), "HOME="+home, "SUPERVISOR_CONF_DIR="+conf, "SUPERVISOR_RETIRE_VANILLA_ONLY=1", "SUPERVISOR_SKIP_CTL=0", "PATH="+fakeBin+":/usr/bin:/bin", "CALLS="+calls)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("retire: %v\n%s", err, out)
	}
	body, err := os.ReadFile(primary)
	if err != nil || string(body) != "existing owner config" {
		t.Fatalf("primary config changed: %s %v", body, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("comparison config remains")
	}
	out, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "launchctl bootout gui/") || !strings.HasSuffix(lines[0], "/com.marcelocantos.jevons-ui-vanilla") || lines[1] != "supervisorctl reread" || lines[2] != "supervisorctl update jevons-vanilla" {
		t.Fatalf("retirement touched unexpected service operations:\n%s", out)
	}
}

// A reread does not register a new group or replace its loaded definition.
func TestSupervisorInstallAppliesRenderedPrimaryDefinition(t *testing.T) {
	for _, state := range []string{"fresh", "changed"} {
		t.Run(state, func(t *testing.T) {
			home, conf, fakeBin := t.TempDir(), t.TempDir(), t.TempDir()
			loaded := filepath.Join(t.TempDir(), "loaded.ini")
			calls := filepath.Join(t.TempDir(), "calls")
			if err := os.WriteFile(filepath.Join(conf, "claudia.ini"), []byte("[program:claudia]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if state == "changed" {
				if err := os.WriteFile(loaded, []byte("old loaded definition"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for name, body := range map[string]string{
				"supervisorctl": `#!/bin/sh
set -e
printf '%s\n' "$*" >>"$CALLS"
case "$*" in
  reread) printf 'jevonsd: %s\nother: changed\n' "$INITIAL_STATE" ;;
  'stop jevons-broker'|'update jevons-broker'|'update claudia') : ;;
  'update jevonsd') cp "$SUPERVISOR_CONF_DIR/jevonsd.ini" "$LOADED" ;;
  'restart jevonsd'|'start jevonsd') cmp "$LOADED" "$SUPERVISOR_CONF_DIR/jevonsd.ini" ;;
  'status jevonsd') : ;;
  *) echo "unexpected supervisor operation: $*" >&2; exit 1 ;;
esac
`,
				"claudia": "#!/bin/sh\nprintf 'claudia %s\\n' \"$*\" >>\"$CALLS\"\n[ \"$*\" = 'broker status' ]\n",
				"launchctl": "#!/bin/sh\nexit 0\n",
				"brew":      "#!/bin/sh\nexit 0\n",
				"lsof":      "#!/bin/sh\nexit 1\n",
				"pgrep":     "#!/bin/sh\nexit 1\n",
			} {
				if err := os.WriteFile(filepath.Join(fakeBin, name), []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("/bin/sh", filepath.Join(supervisorDir(t), "install.sh"))
			cmd.Env = append(os.Environ(), "HOME="+home, "SUPERVISOR_CONF_DIR="+conf,
				"SUPERVISOR_RETIRE_VANILLA_ONLY=0", "SUPERVISOR_SKIP_CTL=0", "SUPERVISOR_NO_TAKEOVER=0",
				"PATH="+fakeBin+":/usr/bin:/bin", "CALLS="+calls, "LOADED="+loaded, "INITIAL_STATE="+state)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("install %s: %v\n%s", state, err, out)
			}
			out, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{
				"reread",
				"stop jevons-broker",
				"update jevons-broker",
				"update claudia",
				"claudia broker status",
				"claudia broker status",
				"claudia broker status",
				"update jevonsd",
				"restart jevonsd",
				"status jevonsd",
				"claudia broker status",
			}
			got := strings.Split(strings.TrimSpace(string(out)), "\n")
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Fatalf("primary definition not applied before restart: %q", got)
			}
		})
	}
}

func TestSupervisorInstallStartsClaudiaBeforeRestartingJevonsd(t *testing.T) {
	home, conf, fakeBin := t.TempDir(), t.TempDir(), t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	ready := filepath.Join(t.TempDir(), "broker-ready")
	if err := os.WriteFile(filepath.Join(conf, "claudia.ini"), []byte("[program:claudia]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"supervisorctl": `#!/bin/sh
printf '%s\n' "$*" >>"$CALLS"
case "$*" in
  reread|'stop jevons-broker'|'update jevons-broker'|'update claudia'|'update jevonsd'|'status jevonsd') : ;;
  'start claudia') touch "$READY" ;;
  'restart jevonsd') test -f "$READY" ;;
  *) echo "unexpected supervisor operation: $*" >&2; exit 1 ;;
esac
`,
		"claudia": `#!/bin/sh
printf 'claudia %s\n' "$*" >>"$CALLS"
test "$*" = 'broker status' && test -f "$READY"
`,
		"launchctl": "#!/bin/sh\nexit 0\n",
		"lsof":      "#!/bin/sh\nexit 1\n",
		"pgrep":     "#!/bin/sh\nexit 1\n",
	} {
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("/bin/sh", filepath.Join(supervisorDir(t), "install.sh"))
	cmd.Env = append(os.Environ(), "HOME="+home, "SUPERVISOR_CONF_DIR="+conf,
		"SUPERVISOR_RETIRE_VANILLA_ONLY=0", "SUPERVISOR_SKIP_CTL=0", "SUPERVISOR_NO_TAKEOVER=0",
		"PATH="+fakeBin+":/usr/bin:/bin", "CALLS="+calls, "READY="+ready)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	out, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{"stop jevons-broker", "update jevons-broker", "update claudia", "start claudia", "restart jevonsd"} {
		if !strings.Contains(got, want+"\n") {
			t.Fatalf("missing %q in installer operations:\n%s", want, got)
		}
	}
	if strings.Index(got, "start claudia\n") > strings.Index(got, "restart jevonsd\n") {
		t.Fatalf("jevonsd restarted before Claudia was started:\n%s", got)
	}
	if strings.Contains(got, "stop claudia") {
		t.Fatalf("installer evicted Claudia:\n%s", got)
	}
}

func TestSupervisorInstallKeepsOldDefinitionWithoutClaudia(t *testing.T) {
	home, conf, fakeBin := t.TempDir(), t.TempDir(), t.TempDir()
	old := filepath.Join(conf, "jevons-broker.ini")
	if err := os.WriteFile(old, []byte("[program:jevons-broker]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", filepath.Join(supervisorDir(t), "install.sh"))
	cmd.Env = append(os.Environ(), "HOME="+home, "SUPERVISOR_CONF_DIR="+conf,
		"SUPERVISOR_RETIRE_VANILLA_ONLY=0", "SUPERVISOR_SKIP_CTL=0", "SUPERVISOR_NO_TAKEOVER=0",
		"PATH="+fakeBin+":/usr/bin:/bin")
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "claudia is required") {
		t.Fatalf("install without Claudia: %v\n%s", err, out)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("old broker definition was removed before preflight: %v", err)
	}
}

func TestSupervisorInstallRefusesOldBrokerStillRunning(t *testing.T) {
	home, conf, fakeBin := t.TempDir(), t.TempDir(), t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	for name, body := range map[string]string{
		"supervisorctl": "#!/bin/sh\nprintf '%s\\n' \"$*\" >>\"$CALLS\"\nexit 0\n",
		"claudia":      "#!/bin/sh\n[ \"$*\" = 'broker status' ]\n",
		"pgrep":        "#!/bin/sh\nexit 0\n",
		"launchctl":    "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(fakeBin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("/bin/sh", filepath.Join(supervisorDir(t), "install.sh"))
	cmd.Env = append(os.Environ(), "HOME="+home, "SUPERVISOR_CONF_DIR="+conf,
		"SUPERVISOR_RETIRE_VANILLA_ONLY=0", "SUPERVISOR_SKIP_CTL=0", "SUPERVISOR_NO_TAKEOVER=0",
		"PATH="+fakeBin+":/usr/bin:/bin", "CALLS="+calls)
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "jevons-broker is still running") {
		t.Fatalf("install with old broker still running: %v\n%s", err, out)
	}
	out, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "restart jevonsd") {
		t.Fatalf("jevonsd restarted against the old broker:\n%s", out)
	}
}
