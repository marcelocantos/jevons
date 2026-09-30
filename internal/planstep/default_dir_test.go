package planstep

import (
	"path/filepath"
	"testing"
)

func TestDefaultDir(t *testing.T) {
	got := DefaultDir("/home/x/.jevons")
	want := filepath.Join("/home/x/.jevons", "planstep")
	if got != want {
		t.Fatalf("DefaultDir = %q, want %q", got, want)
	}
}
