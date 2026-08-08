// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import "strings"

// Company keys shared with web/scripts/model_prefix.js (🎯T287 / 🎯T323).
// Provider wins for the badge mark; a model id that sniffs as a *different*
// company is foreign residue (e.g. sticky "fable" after claude→grok migrate)
// and must not reach /api/agents or the family letter.

const (
	companyAnthropic = "anthropic"
	companyXAI       = "xai"
	companyOpenAI    = "openai"
)

// providerCompany maps a registry provider id to a badge company.
// Empty when the provider is unknown (caller keeps the model rather than
// inventing a mismatch).
func providerCompany(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude", "anthropic", "bedrock":
		return companyAnthropic
	case "grok", "xai":
		return companyXAI
	case "codex", "openai":
		return companyOpenAI
	default:
		return ""
	}
}

// modelCompany sniffs a model id for the company that ships it. Empty when
// the string names nothing we recognise — unknown pins stay through.
// Mirrors companyFromModel in web/scripts/model_prefix.js.
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
	default:
		return ""
	}
}

// modelFitsProvider reports whether model may be reported for an agent on
// provider (🎯T323). Empty model always fits (mark-alone is correct). A
// model that clearly belongs to another company does not — fail closed so
// migrate residue never paints Grok+F or leaks Anthropic ids under grok.
func modelFitsProvider(provider, model string) bool {
	if strings.TrimSpace(model) == "" {
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
