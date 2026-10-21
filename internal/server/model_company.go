// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// Company keys shared with web/scripts/model_prefix.js (🎯T287 / 🎯T323).
// Provider wins for the badge mark; a model id that sniffs as a *different*
// company is foreign residue (e.g. sticky "fable" after claude→grok migrate)
// and must not reach /api/agents or the family letter.

const (
	companyAnthropic = "anthropic"
	companyXAI       = "xai"
	companyOpenAI    = "openai"
	companyCursor    = "cursor"
)

// providerCompany maps a registry provider id, in either vocabulary
// (🎯T1079: xai-oauth and openai-codex rows used to read as unknown), to a
// badge company. Empty when the provider is unknown (caller keeps the model
// rather than inventing a mismatch).
func providerCompany(provider string) string {
	switch seatprovider.Resolve(claudia.Provider(provider)).Plan {
	case seatprovider.Claude, seatprovider.Bedrock:
		return companyAnthropic
	case seatprovider.Grok:
		return companyXAI
	case seatprovider.Codex:
		return companyOpenAI
	case seatprovider.Cursor:
		return companyCursor
	default:
		return ""
	}
}

// modelCompany returns the company that ships model. Empty when the id
// names nothing this table recognises — an unrecognised pin stays through.
// Mirrors companyFromModel in the fleet badge.
func modelCompany(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return ""
	}
	switch {
	case strings.Contains(m, "claude"),
		strings.Contains(m, "opus"),
		strings.Contains(m, "sonnet"),
		strings.Contains(m, "haiku"),
		strings.Contains(m, "fable"):
		return companyAnthropic
	case strings.Contains(m, "grok"):
		return companyXAI
	case strings.Contains(m, "gpt"),
		strings.Contains(m, "codex"),
		len(m) >= 2 && m[0] == 'o' && m[1] >= '0' && m[1] <= '9':
		return companyOpenAI
	case strings.Contains(m, "composer"):
		return companyCursor
	default:
		return ""
	}
}

// modelFitsProvider reports whether model may be reported for an agent on
// provider (🎯T323). Empty model always fits (mark-alone is correct). A
// model that clearly belongs to another company does not — fail closed so
// migrate residue never paints Grok+F or leaks Anthropic ids under grok.
//
// Cursor is a harness, not a model company. The fleet mark is the Cursor
// logo and the subscript is the model that seat is running, which is
// routinely Claude, GPT, Grok, or Composer. Dropping those ids leaves
// every Cursor seat as a bare mark.
func modelFitsProvider(provider, model string) bool {
	if strings.TrimSpace(model) == "" {
		return true
	}
	if seatprovider.Resolve(claudia.Provider(provider)).Plan == seatprovider.Cursor {
		return true
	}
	mc := modelCompany(model)
	if mc == "" {
		return true // unrecognised id: keep (pin may be free-form)
	}
	pc := providerCompany(provider)
	if pc == "" {
		return true // unknown provider: keep; UI may still sniff the model
	}
	return mc == pc
}
