// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"github.com/marcelocantos/jevons/internal/sendq"
	"strings"
)

func parseSendDirective(args map[string]any) (*sendq.Directive, error) {
	family := strings.TrimSpace(str(args["directive_family"]))
	kind := strings.TrimSpace(str(args["directive_kind"]))
	if family == "" && kind == "" {
		return nil, nil
	}
	d := &sendq.Directive{Family: family, Kind: kind}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if raw := str(args["directive_family"]); raw != family {
		return nil, fmt.Errorf("directive_family must not carry surrounding whitespace")
	}
	return d, nil
}
