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
