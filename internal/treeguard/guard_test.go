package treeguard

// Unit coverage for the pure policy (🎯T376). The end-to-end acceptance oracle
// lives in clobber_e2e_test.go; these pin the decision table so a future
// refactor cannot quietly turn a denial into a silent allow.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	baseHTML  = "<html>\n<div id=\"top\">base</div>\n</html>\n"
	otherEdit = "<script src=\"scripts/fleet_cycle.js\"></script>"
	myEdit    = "<div class=\"sidebar-composer-title\">Composer</div>"
)

// diskWithOther is what another worker left on disk after its edit landed.
var diskWithOther = "<html>\n<div id=\"top\">base</div>\n" + otherEdit + "\n</html>\n"

func observed(content string) *Observation {
	return &Observation{Path: "web/index.html", Hash: HashBytes([]byte(content))}
}

func TestDecideAllowsWriteFromCurrentContent(t *testing.T) {
	got := Decide(&DecideArgs{
		Tool:            "Write",
		RelPath:         "web/index.html",
		OnDisk:          []byte(diskWithOther),
		Observed:        observed(diskWithOther),
		ObservedContent: []byte(diskWithOther),
		Proposed:        []byte(diskWithOther + myEdit + "\n"),
	})
	if got.Verdict != Allow || got.Reason != "fresh" {
		t.Fatalf("Decide = %+v, want Allow/fresh", got)
	}
}

func TestDecideDeniesStaleBaseAndNamesTheLostLine(t *testing.T) {
	// The exact 🎯T370 loss: our proposal is derived from the pre-other-worker
	// content, so it silently drops their script tag.
	got := Decide(&DecideArgs{
		Tool:            "Write",
		RelPath:         "web/index.html",
		OnDisk:          []byte(diskWithOther),
		Observed:        observed(baseHTML),
		ObservedContent: []byte(baseHTML),
		Proposed:        []byte(strings.Replace(baseHTML, "</html>", myEdit+"\n</html>", 1)),
	})
	if got.Verdict != Deny || got.Reason != "stale-base" {
		t.Fatalf("Decide = %+v, want Deny/stale-base", got)
	}
	if len(got.AtRisk) != 1 || got.AtRisk[0] != otherEdit {
		t.Errorf("AtRisk = %q, want exactly the other worker's line", got.AtRisk)
	}
	for _, want := range []string{"web/index.html", "fleet_cycle.js", "T376"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message lacks %q:\n%s", want, got.Message)
		}
	}
}

func TestDecideDeniesWriteBySessionThatNeverRead(t *testing.T) {
	got := Decide(&DecideArgs{
		Tool:     "Write",
		RelPath:  "web/index.html",
		OnDisk:   []byte(diskWithOther),
		Proposed: []byte(baseHTML),
	})
	if got.Verdict != Deny || got.Reason != "never-observed" {
		t.Fatalf("Decide = %+v, want Deny/never-observed", got)
	}
}

func TestDecideAllowsStaleBaseWhenProposalKeepsTheirLines(t *testing.T) {
	// A worker that re-read and re-applied on top has a stale *hash* only
	// because it wrote in between; nothing of theirs would be lost, so
	// refusing here would be false-positive noise.
	got := Decide(&DecideArgs{
		Tool:            "Write",
		RelPath:         "web/index.html",
		OnDisk:          []byte(diskWithOther),
		Observed:        observed(baseHTML),
		ObservedContent: []byte(baseHTML),
		Proposed:        []byte(diskWithOther + myEdit + "\n"),
	})
	if got.Verdict != Allow || got.Reason != "stale-base-no-loss" {
		t.Fatalf("Decide = %+v, want Allow/stale-base-no-loss", got)
	}
}

func TestDecideDeniesStaleEditWithUnknownResult(t *testing.T) {
	// Edit supplies no whole-file proposal, so the guard cannot prove the
	// other worker's line survives and must refuse.
	got := Decide(&DecideArgs{
		Tool:            "Edit",
		RelPath:         "web/index.html",
		OnDisk:          []byte(diskWithOther),
		Observed:        observed(baseHTML),
		ObservedContent: []byte(baseHTML),
	})
	if got.Verdict != Deny || got.Reason != "stale-base" {
		t.Fatalf("Decide = %+v, want Deny/stale-base", got)
	}
}

func TestDecideAllowsWhenNothingToProtect(t *testing.T) {
	for _, tc := range []struct {
		name string
		args DecideArgs
		want string
	}{{
		name: "non-mutating tool",
		args: DecideArgs{Tool: "Read", RelPath: "web/index.html", OnDisk: []byte(baseHTML)},
		want: "tool-not-mutating",
	}, {
		name: "unguarded path",
		args: DecideArgs{Tool: "Write", RelPath: "web/scripts/fleet_cycle.js", OnDisk: []byte(baseHTML)},
		want: "path-not-guarded",
	}, {
		name: "new file",
		args: DecideArgs{Tool: "Write", RelPath: "web/index.html"},
		want: "new-file",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(&tc.args)
			if got.Verdict != Allow || got.Reason != tc.want {
				t.Fatalf("Decide = %+v, want Allow/%s", got, tc.want)
			}
		})
	}
}

func TestDetectLostAdditionsIgnoresWhitespaceOnlyChurn(t *testing.T) {
	base := "alpha\nbeta\n"
	disk := "  alpha  \n\n\tbeta\n"
	if got := DetectLostAdditions([]byte(base), []byte(disk), nil); len(got) != 0 {
		t.Fatalf("reindent read as loss: %q", got)
	}
}

func TestDetectLostAdditionsCapsOutput(t *testing.T) {
	var disk strings.Builder
	for i := range MaxAtRisk + 5 {
		disk.WriteString("added line ")
		disk.WriteByte(byte('a' + i))
		disk.WriteByte('\n')
	}
	got := DetectLostAdditions(nil, []byte(disk.String()), nil)
	if len(got) != MaxAtRisk {
		t.Fatalf("len(AtRisk) = %d, want %d", len(got), MaxAtRisk)
	}
}

func TestDecideDeniesBullseyeYAMLEvenWhenFresh(t *testing.T) {
	onDisk := []byte("schema_version: 5\ntargets: {}\n")
	got := Decide(&DecideArgs{
		Tool:            "StrReplace",
		RelPath:         "bullseye.yaml",
		OnDisk:          onDisk,
		Observed:        &Observation{Path: "bullseye.yaml", Hash: HashBytes(onDisk)},
		ObservedContent: onDisk,
		Proposed:        []byte("schema_version: 5\ntargets: {T1: {}}\n"),
	})
	if got.Verdict != Deny || got.Reason != "ledger-tool-only" {
		t.Fatalf("Decide = %+v, want Deny/ledger-tool-only", got)
	}
	if !strings.Contains(got.Message, "bullseye") || !strings.Contains(got.Message, "T546") {
		t.Fatalf("refusal must name the bullseye tool: %q", got.Message)
	}
	write := Decide(&DecideArgs{
		Tool:     "Write",
		RelPath:  "docs/nested/bullseye.yaml",
		OnDisk:   onDisk,
		Observed: &Observation{Path: "docs/nested/bullseye.yaml", Hash: HashBytes(onDisk)},
	})
	if write.Verdict != Deny || write.Reason != "ledger-tool-only" {
		t.Fatalf("nested ledger Write = %+v, want Deny/ledger-tool-only", write)
	}
}

func TestIsLedgerPath(t *testing.T) {
	if !IsLedgerPath("bullseye.yaml") || !IsLedgerPath("foo/Bullseye.yaml") {
		t.Fatal("ledger path not detected")
	}
	if IsLedgerPath("web/index.html") || IsLedgerPath("bullseye.md") {
		t.Fatal("non-ledger path flagged")
	}
}

func TestDecideDeniesBashRedirectToLedger(t *testing.T) {
	onDisk := []byte("schema_version: 5\ntargets: {}\n")
	got := Decide(&DecideArgs{
		Tool:     ToolBash,
		Form:     FormRedirect,
		RelPath:  "bullseye.yaml",
		OnDisk:   onDisk,
		Observed: &Observation{Path: "bullseye.yaml", Hash: HashBytes(onDisk)},
	})
	if got.Verdict != Deny || got.Reason != "ledger-tool-only" {
		t.Fatalf("Decide = %+v, want Deny/ledger-tool-only", got)
	}
}

func TestPreDeniesStrReplaceOfLedger(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bullseye.yaml")
	onDisk := []byte("schema_version: 5\ntargets: {}\n")
	if err := os.WriteFile(path, onDisk, 0o644); err != nil {
		t.Fatal(err)
	}
	env := &Env{
		Store:    &Store{Root: t.TempDir()},
		RepoRoot: root,
		Guarded:  DefaultGuardedPaths,
		Now:      time.Now,
	}
	p := &Payload{SessionID: "t546", CWD: root, ToolName: "StrReplace"}
	p.ToolInput.FilePath = path
	got, err := env.Pre(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != Deny || got.Reason != "ledger-tool-only" {
		t.Fatalf("Pre = %+v, want Deny/ledger-tool-only", got)
	}
	if !strings.Contains(got.Message, "jevons_target_file") || !strings.Contains(got.Message, "T546") {
		t.Fatalf("hook refuse must name the bullseye tool: %q", got.Message)
	}
}

func TestIsGuardedMatchesHotFilesOnly(t *testing.T) {
	for _, tc := range []struct {
		rel  string
		want bool
	}{
		{"web/index.html", true},
		{"web/chat.html", true},
		{"Makefile", true},
		{"bullseye.yaml", true},
		{"web/scripts/chat_events.js", false},
		{"internal/server/devserver.go", false},
	} {
		if got := IsGuarded(tc.rel, DefaultGuardedPaths); got != tc.want {
			t.Errorf("IsGuarded(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}

func TestStoreKeepsSessionsIndependent(t *testing.T) {
	// Per-session directories are the point: the guard must not itself have a
	// file that N concurrent workers write.
	store := &Store{Root: t.TempDir()}
	path := filepath.Join(t.TempDir(), "index.html")
	now := time.Now()

	if err := store.Record("worker-a", path, []byte(baseHTML), now); err != nil {
		t.Fatal(err)
	}
	if err := store.Record("worker-b", path, []byte(diskWithOther), now); err != nil {
		t.Fatal(err)
	}

	obsA, contentA, err := store.Lookup("worker-a", path)
	if err != nil {
		t.Fatal(err)
	}
	if obsA.Hash != HashBytes([]byte(baseHTML)) || string(contentA) != baseHTML {
		t.Error("worker-b's record overwrote worker-a's observation")
	}
	obsB, _, err := store.Lookup("worker-b", path)
	if err != nil {
		t.Fatal(err)
	}
	if obsB.Hash != HashBytes([]byte(diskWithOther)) {
		t.Error("worker-b's own observation is wrong")
	}

	missing, _, err := store.Lookup("worker-c", path)
	if err != nil || missing != nil {
		t.Errorf("Lookup for an unseen session = %+v, %v; want nil, nil", missing, err)
	}
}

func TestStorePruneDropsStaleSessionsOnly(t *testing.T) {
	store := &Store{Root: t.TempDir()}
	path := filepath.Join(t.TempDir(), "index.html")
	now := time.Now()
	if err := store.Record("fresh", path, []byte(baseHTML), now); err != nil {
		t.Fatal(err)
	}
	if err := store.Prune(time.Hour, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if obs, _, _ := store.Lookup("fresh", path); obs == nil {
		t.Fatal("Prune dropped a session it should have kept")
	}
	if err := store.Prune(time.Hour, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if obs, _, _ := store.Lookup("fresh", path); obs != nil {
		t.Fatal("Prune kept a session older than maxAge")
	}
}
