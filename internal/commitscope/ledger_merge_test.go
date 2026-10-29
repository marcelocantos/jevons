package commitscope

import (
	"strings"
	"testing"
)

func TestUnionLedgerYAMLKeepsBothSides(t *testing.T) {
	base := `schema_version: 1
targets:
  T1: {name: a}
`
	worker := `schema_version: 1
targets:
  T2: {name: b}
`
	out, err := UnionLedgerYAML([]byte(base), []byte(worker))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "T1:") || !strings.Contains(string(out), "T2:") {
		t.Fatalf("union dropped a side:\n%s", out)
	}
}
