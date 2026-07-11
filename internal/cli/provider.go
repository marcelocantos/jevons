// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import "github.com/marcelocantos/claudia"

// Provider is the only agent harness jevons uses: Grok Build via claudia.
const Provider = claudia.ProviderGrok

// DefaultModel is empty so the Grok CLI picks its own default.
const DefaultModel = ""
