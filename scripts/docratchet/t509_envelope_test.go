// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// TestT509DoctrinePointsAtEnvelopePackage ratchets 🎯T509: instruction
// files name the fence, the sigil, and internal/envelope as the schema
// source of truth rather than restating the enums.
func TestT509DoctrinePointsAtEnvelopePackage(t *testing.T) {
	need := []string{
		"🎯T509",
		"internal/envelope",
		"jevons: kind",
		"finish-report",
	}
	docs := []string{
		"internal/config/persona.md",
		"AGENTS.md",
		"agents-guide.md",
	}
	for _, doc := range docs {
		body := readRepo(t, doc)
		for _, m := range need {
			if !strings.Contains(body, m) {
				t.Errorf("%s missing T509 marker %q", doc, m)
			}
		}
	}
}

// TestT509DocsDoNotInventKinds: if instruction text writes "jevons: kind X",
// X must be a Kind in internal/envelope.AllKinds().
func TestT509DocsDoNotInventKinds(t *testing.T) {
	known := map[string]bool{}
	for _, k := range envelope.AllKinds() {
		known[string(k)] = true
	}
	re := regexp.MustCompile(`jevons: kind ([a-z0-9-]+)`)
	docs := []string{
		"internal/config/persona.md",
		"AGENTS.md",
		"agents-guide.md",
		"docs/architecture-current.md",
	}
	for _, doc := range docs {
		body := readRepo(t, doc)
		for _, m := range re.FindAllStringSubmatch(body, -1) {
			if !known[m[1]] {
				t.Errorf("%s mentions unknown kind %q (not in envelope.AllKinds)", doc, m[1])
			}
		}
	}
}

func TestT509EnumsLiveInOnePackage(t *testing.T) {
	body := readRepo(t, "internal/envelope/kind.go")
	for _, want := range []string{
		"GREEN",
		"SUSPECT",
		"in-progress",
		"live",
		"class-3",
		"residual",
		"finish-report",
		"status-ping",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("internal/envelope/kind.go missing vocabulary %q", want)
		}
	}
}
