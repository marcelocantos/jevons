// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package silentresponse

import "testing"

func TestIs(t *testing.T) {
	t.Parallel()
	if !Is("[silent] T222 already working") {
		t.Fatal("prefix")
	}
	if !Is("[SILENT] ok") {
		t.Fatal("case")
	}
	if !Is("  [silent] continued jv-x") {
		t.Fatal("trim")
	}
	if Is("T222 still working. No continue needed.") {
		t.Fatal("unmarked status must NOT filter")
	}
	if Is("") {
		t.Fatal("empty")
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()
	if Classify("") != Pending {
		t.Fatal("empty pending")
	}
	if Classify("[") != Pending {
		t.Fatal("partial [ pending")
	}
	if Classify("[sil") != Pending {
		t.Fatal("partial marker pending")
	}
	if Classify("[silent]") != Silent {
		t.Fatal("complete marker silent")
	}
	if Classify("[silent] continued jv-x") != Silent {
		t.Fatal("full silent body")
	}
	// Multi-fragment accumulate: prefix then continuation.
	acc := "[silent]"
	if Classify(acc) != Silent {
		t.Fatal("first frag")
	}
	acc += " continued"
	if Classify(acc) != Silent {
		t.Fatal("continuation still silent")
	}
	if Classify("Hello owner") != Visible {
		t.Fatal("normal prose visible")
	}
	if Classify(" continued") != Visible {
		t.Fatal("orphan continued (no marker in acc) is visible on its own")
	}
}
