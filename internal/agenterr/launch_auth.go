// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import (
	"strings"
)

// LaunchAuthFailure reports provider authentication refusal on agent or
// provider start (🎯T607). Launch errors often arrive without a typed
// provider-failure cause — only the rendered text — so this helper pins
// the known incident strings after Classify. It is not for reply prose or
// send-path classification.
func LaunchAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	if Classify(err) == ClassAuth {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "authentication required") {
		return true
	}
	if strings.Contains(msg, "invalid api key") {
		return true
	}
	if strings.Contains(msg, "authentication_error") {
		return true
	}
	if strings.Contains(msg, "invalid_token") {
		return true
	}
	if strings.Contains(msg, "401 unauthorized") || strings.Contains(msg, "403 forbidden") {
		return true
	}
	return false
}

// LaunchAuthOwnerCopy is actionable owner-facing text for a launch-time
// auth refusal, including the fix command when the detail is mute churn.
func LaunchAuthOwnerCopy(raw string) string {
	return OwnerCopy(ClassAuth, raw)
}
