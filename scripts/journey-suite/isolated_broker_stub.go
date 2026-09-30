// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !claudia_omp

// 🎯T950: stands in for isolated_broker.go on the default build, which
// cannot import github.com/marcelocantos/claudia/omp — that package exists
// only on local claudia master, past the published v0.42.0 pin. Build with
// -tags claudia_omp against a go.work'd local claudia checkout (🎯T448) to
// run the brokered journeys (subscription sidecar seats, provider
// migration) for real; otherwise they fail fast, naming the tag.

package main

import (
	"fmt"

	"github.com/marcelocantos/claudia"
)

type isolatedBroker struct{ socket string }

func journeyNeedsBroker(provider claudia.Provider) bool {
	switch provider {
	case claudia.ProviderGrok, claudia.ProviderCursor:
		return true
	default:
		return false
	}
}

func (s *suite) withIsolatedBroker(run func(*suite) error) error {
	return errBrokerUnavailable
}

func (s *suite) startIsolatedBroker() (*isolatedBroker, error) {
	return nil, errBrokerUnavailable
}

func (b *isolatedBroker) close() error { return nil }

var errBrokerUnavailable = fmt.Errorf(
	"isolated Claudia broker needs github.com/marcelocantos/claudia/omp, unpublished past the v0.42.0 pin; " +
		"rebuild with -tags claudia_omp against a local claudia checkout (🎯T950, 🎯T448)")
