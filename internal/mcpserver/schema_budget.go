// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// 🎯T862.18 — tool schema token budget.
//
// A tool schema is JSON: a name, a type, a set of property names/types, and
// prose (the tool description plus every property's "description" field).
// The structural part (names, "type": "string", "required": [...]) is what a
// model actually needs to call the tool correctly; the prose is instructional
// doctrine — often the bulk of it, as this daemon's own tool set shows (some
// single tool descriptions here run past a thousand words). Left unbounded,
// prose crowds out everything else in the context a seat pays for on every
// turn.
//
// maxToolDescBytes / maxPropDescBytes bound the prose this daemon will still
// SERVE over MCP tools/list, regardless of profile (🎯T425 core/ambient is an
// orthogonal cut by tool identity; this is a cut by description length,
// applied to every tool that survives that first cut). A description at or
// under the bound is untouched; a longer one is hard-truncated with an
// ellipsis marker so the seat can tell the schema was budgeted rather than
// silently short.
const (
	maxToolDescBytes = 600
	maxPropDescBytes = 300
)

const budgetEllipsis = " …[schema truncated 🎯T862.18]"

// budgetDescription truncates a description to at most n bytes, appending a
// visible marker so truncation is never silent. n<=0 disables the bound.
func budgetDescription(desc string, n int) string {
	if n <= 0 || len(desc) <= n {
		return desc
	}
	cut := n - len(budgetEllipsis)
	if cut < 0 {
		cut = 0
	}
	if cut > len(desc) {
		cut = len(desc)
	}
	return desc[:cut] + budgetEllipsis
}

// budgetToolSchema returns a copy of t with its own description and every
// property description bounded. Structural fields (name, type, required,
// enum, items) are left exactly as built — only prose is touched.
func budgetToolSchema(t mcp.Tool) mcp.Tool {
	t.Description = budgetDescription(t.Description, maxToolDescBytes)
	if len(t.InputSchema.Properties) == 0 {
		return t
	}
	props := make(map[string]any, len(t.InputSchema.Properties))
	for name, raw := range t.InputSchema.Properties {
		m, ok := raw.(map[string]any)
		if !ok {
			props[name] = raw
			continue
		}
		if d, ok := m["description"].(string); ok {
			budgeted := make(map[string]any, len(m))
			for k, v := range m {
				budgeted[k] = v
			}
			budgeted["description"] = budgetDescription(d, maxPropDescBytes)
			props[name] = budgeted
			continue
		}
		props[name] = m
	}
	t.InputSchema.Properties = props
	return t
}

// budgetToolSchemas applies budgetToolSchema to every tool in the slice.
func budgetToolSchemas(tools []mcp.Tool) []mcp.Tool {
	out := make([]mcp.Tool, len(tools))
	for i, t := range tools {
		out[i] = budgetToolSchema(t)
	}
	return out
}

// filterToolsByProfile is the server's tools/list filter (🎯T425 profile
// cut); this wraps it so every returned surface — profiled or not — also
// gets the 🎯T862.18 prose budget. The profile cut and the prose budget are
// independent: a tool can survive the identity cut and still have its
// description trimmed.
func filterAndBudgetTools(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	return budgetToolSchemas(filterToolsByProfile(ctx, tools))
}

// schemaProseBytes measures the prose (tool description + property
// descriptions) and structural (everything else in the marshalled schema)
// byte counts for a tool set, for use in measurement/oracle code. It does
// not depend on the tools having been budgeted.
func schemaProseBytes(tools []mcp.Tool) (prose, structural int) {
	for _, t := range tools {
		prose += len(t.Description)
		structural += len(t.Name)
		for name, raw := range t.InputSchema.Properties {
			structural += len(name)
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			for k, v := range m {
				if k == "description" {
					if d, ok := v.(string); ok {
						prose += len(d)
					}
					continue
				}
				if s, ok := v.(string); ok {
					structural += len(s)
				} else {
					structural += len(k)
				}
			}
		}
		for _, r := range t.InputSchema.Required {
			structural += len(r)
		}
	}
	return prose, structural
}
