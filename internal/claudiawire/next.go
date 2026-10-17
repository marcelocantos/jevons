// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

package claudiawire

import (
	"context"

	"github.com/marcelocantos/claudia"
)

// Aliases of the Claudia module selected by the build. The default build
// uses the local copies in the pin files instead.

type UsageFact = claudia.UsageFact
type UsageCounters = claudia.UsageCounters
type UsageRequestRef = claudia.UsageRequestRef
type UsagePage = claudia.UsagePage
type UsageQuery = claudia.UsageQuery

const UsagePageLimit = claudia.UsagePageLimit

func ValidateUsageTransition(previous, incoming *UsageFact) (bool, error) {
	return claudia.ValidateUsageTransition(previous, incoming)
}

type RequestReceipt = claudia.RequestReceipt
type ReceiptState = claudia.ReceiptState
type ReceiptCause = claudia.ReceiptCause
type ReceiptQuery = claudia.ReceiptQuery

const (
	ReceiptAccepted  = claudia.ReceiptAccepted
	ReceiptStarted   = claudia.ReceiptStarted
	ReceiptCompleted = claudia.ReceiptCompleted
	ReceiptCancelled = claudia.ReceiptCancelled
	ReceiptFailed    = claudia.ReceiptFailed
	ReceiptUnknown   = claudia.ReceiptUnknown

	ReceiptCauseNone         = claudia.ReceiptCauseNone
	ReceiptCauseAuth         = claudia.ReceiptCauseAuth
	ReceiptCauseCapacity     = claudia.ReceiptCauseCapacity
	ReceiptCauseCancellation = claudia.ReceiptCauseCancellation
	ReceiptCauseFailure      = claudia.ReceiptCauseFailure
	ReceiptCauseUnknown      = claudia.ReceiptCauseUnknown
)

func QueryReceipt(ctx context.Context, q ReceiptQuery) (*RequestReceipt, error) {
	return claudia.QueryReceipt(ctx, q)
}

type GenerationIdentity = claudia.GenerationIdentity
type GenerationIssue = claudia.GenerationIssue
type GenerationPage = claudia.GenerationPage
type GenerationDiscoveryError = claudia.GenerationDiscoveryError
type GenerationQuery = claudia.GenerationQuery

const (
	GenerationPageLimit    = claudia.GenerationPageLimit
	DiscoveryCursorExpired = claudia.DiscoveryCursorExpired
	DiscoveryBusy          = claudia.DiscoveryBusy
)
