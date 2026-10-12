package mcpserver

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestT540VerdictFile(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	b, err := os.ReadFile(filepath.Join(root, "docs/evidence/t540.7.1.1/VERDICT.md"))
	if err != nil {
		t.Fatal(err)
	}
	report := "Achieved 🎯T540.7.1.1.\n" + string(b)
	if !HasVisualProseVerdict(report) {
		t.Fatal("HasVisualProseVerdict false")
	}
	if c := ClassifyVisualVerdict(report); c != VisualVerdictPresent {
		t.Fatalf("class=%s", c)
	}
}
