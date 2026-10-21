// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

// No test here reaches the owner's launchd; launchctl answers as an empty
// domain unless a test says otherwise. internal/launchdfake ratchets this.
import _ "github.com/marcelocantos/jevons/internal/launchdfake"
