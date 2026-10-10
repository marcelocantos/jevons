// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import "strings"

// ClassUnsupportedModel is a seat-local, persistent refusal of a pinned model
// on this account. It is not an account-wide spend/auth wall and no amount of
// retrying this same session changes the provider's model entitlement.
const ClassUnsupportedModel Class = "unsupported_model"

// IsUnsupportedModel deliberately matches the native Codex refusal, not any
// arbitrary 400 or the word "unsupported" in assistant-authored prose.
func IsUnsupportedModel(raw string) bool {
	low := strings.ToLower(raw)
	return strings.Contains(low, "model is not supported when using codex with a chatgpt account") &&
		strings.Contains(low, "the '")
}
