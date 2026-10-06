// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/cli"
)

func (s *Server) handleAgentProviderPolicy(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name := strings.TrimSpace(str(args["name"]))
	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}
	if s.registry == nil {
		return mcp.NewToolResultError("agent registry not available"), nil
	}
	def := s.registry.Def(name)
	if def == nil {
		return mcp.NewToolResultError(fmt.Sprintf("agent %q is not registered", name)), nil
	}
	_, setPrefer := args["prefer_provider"]
	_, setAllowed := args["allowed_providers"]
	_, setExcluded := args["exclude_providers"]
	_, setInterrupt := args["allow_interrupt"]
	_, setPark := args["allow_park"]
	allowAny := boolArg(args["allow_any"])
	st := s.seatPlans.Get(name)
	if !setPrefer && !setAllowed && !setExcluded && !setInterrupt && !setPark && !allowAny {
		return mcp.NewToolResultText(formatAgentProviderPolicy(*def, st)), nil
	}
	actor := strings.TrimSpace(str(args["actor"]))
	if actor != s.overseerName() {
		return mcp.NewToolResultError("only the overseer may change a seat's provider policy; pass actor=" + s.overseerName()), nil
	}
	if setAllowed && allowAny {
		return mcp.NewToolResultError("allowed_providers and allow_any cannot be combined"), nil
	}
	if s.seatPlans == nil {
		return mcp.NewToolResultError("seat plan store is not configured"), nil
	}
	prefer := st.PreferProvider
	if setPrefer {
		value, ok := args["prefer_provider"].(string)
		if !ok {
			return mcp.NewToolResultError("prefer_provider must be a string"), nil
		}
		prefer = cli.PlanProvider(claudia.Provider(strings.TrimSpace(value)))
	}
	allowed := st.AllowedProviders
	allowNone := st.AllowNone
	if allowAny {
		allowed = nil
		allowNone = false
	} else if setAllowed {
		parsed, err := providerPolicyArray(args["allowed_providers"])
		if err != nil {
			return mcp.NewToolResultError("allowed_providers: " + err.Error()), nil
		}
		for i := range parsed {
			parsed[i] = cli.PlanProvider(parsed[i])
		}
		allowed = parsed
		allowNone = len(parsed) == 0
		if allowNone {
			allowed = nil
		}
	}
	excluded := st.ExcludeProviders
	if setExcluded {
		parsed, err := providerPolicyArray(args["exclude_providers"])
		if err != nil {
			return mcp.NewToolResultError("exclude_providers: " + err.Error()), nil
		}
		for i := range parsed {
			parsed[i] = cli.PlanProvider(parsed[i])
		}
		excluded = parsed
	}
	mayInterrupt := st.HostMayInterrupt
	if setInterrupt {
		value, ok := args["allow_interrupt"].(bool)
		if !ok {
			return mcp.NewToolResultError("allow_interrupt must be a boolean"), nil
		}
		mayInterrupt = value
	}
	neverPark := st.HostNeverPark
	if setPark {
		value, ok := args["allow_park"].(bool)
		if !ok {
			return mcp.NewToolResultError("allow_park must be a boolean"), nil
		}
		neverPark = !value
	}
	next := st
	next.PreferProvider = prefer
	next.AllowedProviders = allowed
	next.AllowNone = allowNone
	next.ExcludeProviders = excluded
	next.HostMayInterrupt = mayInterrupt
	next.HostNeverPark = neverPark
	if err := s.seatPlans.Put(name, next); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	s.logLifecycle(compAgentLifecycle, "provider_policy", "ok", map[string]any{
		"name": name, "actor": actor, "prefer_provider": prefer,
		"allowed_providers": allowed, "exclude_providers": excluded,
		"allow_interrupt": mayInterrupt, "allow_park": !neverPark,
	})
	return mcp.NewToolResultText(formatAgentProviderPolicy(*def, s.seatPlans.Get(name))), nil
}

func providerPolicyArray(raw any) ([]claudia.Provider, error) {
	var entries []any
	switch value := raw.(type) {
	case []any:
		entries = value
	case []string:
		for _, item := range value {
			entries = append(entries, item)
		}
	default:
		return nil, fmt.Errorf("must be an array of provider names")
	}
	providers := make([]claudia.Provider, 0, len(entries))
	for _, entry := range entries {
		value, ok := entry.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("each provider must be a non-empty string")
		}
		providers = append(providers, claudia.Provider(strings.TrimSpace(value)))
	}
	return providers, nil
}

func formatAgentProviderPolicy(def claudia.AgentDef, st claudia.SeatPolicy) string {
	allowed := "any"
	if provs, restricted := st.Allowed(); restricted {
		allowed = fmt.Sprint(provs)
		if len(provs) == 0 {
			allowed = "none"
		}
	}
	prefer := string(st.PreferProvider)
	if prefer == "" {
		prefer = "none"
	}
	return fmt.Sprintf("Agent %q provider policy (Claudia): current=%s, prefer=%s, allowed=%s, excluded=%v; Jevons host policy: allow_interrupt=%t, allow_park=%t. Changes affect future placement; no move was triggered.",
		def.Name, def.Provider, prefer, allowed, st.ExcludeProviders, st.HostMayInterrupt, !st.HostNeverPark)
}
