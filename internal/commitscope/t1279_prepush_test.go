package commitscope

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The pre-push `git lfs install` writes (git-lfs 3.x).
const stockLFSPrePush = `#!/bin/sh
command -v git-lfs >/dev/null 2>&1 || { printf >&2 "\n%s\n\n" "This repository is configured for Git LFS but 'git-lfs' was not found on your path. If you no longer wish to use Git LFS, remove this hook by deleting the 'pre-push' file in the hooks directory (set by 'core.hookspath'; usually '.git/hooks')."; exit 2; }
git lfs pre-push "$@"
`

func TestT1279StockLFSPrePushIsRecognised(t *testing.T) {
	if !IsStockLFSPrePush([]byte(stockLFSPrePush)) {
		t.Fatal("stock git-lfs pre-push not recognised")
	}
	custom := stockLFSPrePush + "make lint\n"
	if IsStockLFSPrePush([]byte(custom)) {
		t.Fatal("a pre-push with extra commands was treated as stock; replacing it would lose them")
	}
	if IsStockLFSPrePush([]byte("#!/bin/sh\nexit 0\n")) {
		t.Fatal("a hook without git lfs pre-push was treated as stock")
	}
}

func TestT1279InstallReplacesStockLFSPrePushOnly(t *testing.T) {
	source := filepath.Join(repoRoot(t), "scripts", "hooks", "pre-push")
	ours, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}

	hooks := t.TempDir()
	dest := filepath.Join(hooks, "pre-push")
	if err := os.WriteFile(dest, []byte(stockLFSPrePush), 0o755); err != nil {
		t.Fatal(err)
	}
	outcome, err := InstallHook(hooks, source, "pre-push")
	if err != nil || outcome != HookUpdated {
		t.Fatalf("stock LFS hook: outcome %v err %v, want updated", outcome, err)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(ours) {
		t.Fatal("stock LFS hook was not replaced by ours")
	}

	custom := stockLFSPrePush + "make lint\n"
	if err := os.WriteFile(dest, []byte(custom), 0o755); err != nil {
		t.Fatal(err)
	}
	outcome, err = InstallHook(hooks, source, "pre-push")
	if err != nil || outcome != HookForeign {
		t.Fatalf("custom hook: outcome %v err %v, want foreign", outcome, err)
	}
	if got, _ := os.ReadFile(dest); string(got) != custom {
		t.Fatal("a custom pre-push was overwritten")
	}
}

// TestT1279PrePushHook drives scripts/hooks/pre-push in a scratch repository:
// it accepts a push whose tip tree the attestation names, refuses one it does
// not, refuses without an attestation, exempts refs/backup/*, and hands git's
// ref list to git lfs pre-push.
func TestT1279PrePushHook(t *testing.T) {
	hook := filepath.Join(repoRoot(t), "scripts", "hooks", "pre-push")
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "one")
	first := git("rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644)
	git("add", "f")
	git("commit", "-q", "-m", "two")
	second := git("rev-parse", "HEAD")
	gitDir := filepath.Join(dir, ".git")

	attestAs := func(sha, profile string) {
		tree := git("rev-parse", sha+"^{tree}")
		body := "tree=" + tree + "\ncommit=" + sha + "\ndirty=0\nat=2026-10-29T00:00:00Z\nprofile=" + profile + "\ngate=test\n"
		if err := os.WriteFile(filepath.Join(gitDir, "gate-attestation"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	attest := func(sha string) { attestAs(sha, "jevons-full-v1") }
	zero := strings.Repeat("0", 40)
	push := func(stdin string, env ...string) (string, error) {
		cmd := exec.Command(hook, "origin", "git@example.invalid:x.git")
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdin)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	master := func(sha string) string {
		return "refs/heads/master " + sha + " refs/heads/master " + zero + "\n"
	}

	if out, err := push(master(second)); err == nil || !strings.Contains(out, "no gate attestation") {
		t.Fatalf("no attestation: err %v out %q, want refusal", err, out)
	}
	attest(first)
	if out, err := push(master(second)); err == nil || !strings.Contains(out, "run make gate on this commit") {
		t.Fatalf("stale attestation: err %v out %q, want refusal", err, out)
	}
	if out, err := push("refs/heads/wip " + second + " refs/backup/host/wip " + zero + "\n"); err != nil {
		t.Fatalf("backup ref: err %v out %q, want accept", err, out)
	}
	attest(second)
	if out, err := push(master(second)); err != nil || !strings.Contains(out, "gate attestation covers") {
		t.Fatalf("attested tip: err %v out %q, want accept", err, out)
	}
	// 🎯T1299: make gate attests the fast profile, make gate-full the full one.
	attestAs(second, "jevons-fast-v1")
	if out, err := push(master(second)); err != nil || !strings.Contains(out, "gate attestation covers") {
		t.Fatalf("fast-profile attestation: err %v out %q, want accept", err, out)
	}
	attestAs(second, "base")
	if out, err := push(master(second)); err == nil || !strings.Contains(out, "not a jevons gate profile") {
		t.Fatalf("foreign profile: err %v out %q, want refusal", err, out)
	}
	attest(second)

	// LFS chain: a fake git-lfs records the ref list it receives.
	bin := t.TempDir()
	got := filepath.Join(bin, "stdin")
	fake := "#!/bin/sh\ncat > " + got + "\necho \"$@\" > " + got + ".args\n"
	if err := os.WriteFile(filepath.Join(bin, "git-lfs"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.png filter=lfs diff=lfs merge=lfs -text\n"), 0o644)
	if out, err := push(master(second), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		t.Fatalf("lfs chain: err %v out %q", err, out)
	}
	received, _ := os.ReadFile(got)
	if string(received) != master(second) {
		t.Fatalf("git lfs pre-push got stdin %q, want %q", received, master(second))
	}
	args, _ := os.ReadFile(got + ".args")
	if strings.TrimSpace(string(args)) != "pre-push origin git@example.invalid:x.git" {
		t.Fatalf("git lfs got args %q", args)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
