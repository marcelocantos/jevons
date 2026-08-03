// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package provider is the 🎯T27 provider-contract kernel: manifest/describe,
// feed events, ViewNode UI surfaces, MCP endpoint declaration, and an
// in-process registry that mirrors the hub surface jevonsd owns after
// 🎯T27.2–T27.6.
//
// 🎯T27.2 adds structured config loading (via internal/config Providers),
// an additive SQLite persistence home (Store under StateDir/providers/),
// and ConfigManager.Reload for mid-process desired-set updates without a
// full daemon restart. Real network transports / supervisor: 🎯T27.3+.
// The T27.1 conformance suite asserts the protocol through this package.
package provider

import (
	"fmt"
	"time"
)

// ContractMajor is the provider-contract major version this tree speaks.
// Providers must echo this in Manifest.Contract; unknown majors are refused.
const ContractMajor = "1"

// Transport modes for how jevonsd attaches to a provider process.
const (
	TransportLaunch  = "launch"
	TransportConnect = "connect"
)

// MCP transports declared on capabilities.mcp.
const (
	MCPTransportHTTP  = "http"
	MCPTransportStdio = "stdio"
)

// Manifest is the provider's answer to describe.
type Manifest struct {
	ID           string       `json:"id"`
	Version      string       `json:"version"`
	Contract     string       `json:"contract"`
	Capabilities Capabilities `json:"capabilities"`
	Egress       bool         `json:"egress"`
}

// Capabilities lists optional contribution paths. Empty/nil means not offered.
type Capabilities struct {
	Feeds []FeedCap    `json:"feeds,omitempty"`
	UI    []UISurface  `json:"ui,omitempty"`
	MCP   *MCPEndpoint `json:"mcp,omitempty"`
}

// FeedCap describes one append-only event stream.
type FeedCap struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
	Replay bool   `json:"replay"`
}

// UISurface is one declarative panel in the ViewNode vocabulary.
type UISurface struct {
	Surface string    `json:"surface"`
	Title   string    `json:"title"`
	Feeds   []string  `json:"feeds,omitempty"`
	Root    *ViewNode `json:"root,omitempty"`
}

// MCPEndpoint is where the hub MCP client dials (🎯T27.4).
type MCPEndpoint struct {
	Transport string `json:"transport"` // http | stdio
	Endpoint  string `json:"endpoint,omitempty"`
}

// FeedEvent is one append-only feed record.
type FeedEvent struct {
	Feed   string         `json:"feed"`
	Seq    int64          `json:"seq"`
	TS     time.Time      `json:"ts"`
	Origin string         `json:"origin"`
	Kind   string         `json:"kind"`
	Data   map[string]any `json:"data,omitempty"`
}

// ViewNode is the native server-driven UI node (matches ios ViewNode.swift).
type ViewNode struct {
	Type     string         `json:"type"`
	ID       string         `json:"id,omitempty"`
	Props    map[string]any `json:"props,omitempty"`
	Children []ViewNode     `json:"children,omitempty"`
}

// Tool is one MCP tool re-exported by the hub under a provider namespace.
type Tool struct {
	Provider string `json:"provider"`
	Name     string `json:"name"` // bare tool name (pre-namespace)
	// Qualified is "{provider}__{name}" as exposed on jevonsd /mcp.
	Qualified string `json:"qualified"`
}

// ValidateManifest checks describe payload invariants.
func ValidateManifest(m Manifest) error {
	if m.ID == "" {
		return fmt.Errorf("manifest.id is required")
	}
	if m.Version == "" {
		return fmt.Errorf("manifest.version is required")
	}
	if m.Contract != ContractMajor {
		return fmt.Errorf("manifest.contract %q unsupported (want %q)", m.Contract, ContractMajor)
	}
	for _, f := range m.Capabilities.Feeds {
		if f.Name == "" || f.Schema == "" {
			return fmt.Errorf("feed capability needs name and schema")
		}
	}
	for _, u := range m.Capabilities.UI {
		if u.Surface == "" {
			return fmt.Errorf("ui surface needs surface id")
		}
	}
	if mcp := m.Capabilities.MCP; mcp != nil {
		switch mcp.Transport {
		case MCPTransportHTTP:
			if mcp.Endpoint == "" {
				return fmt.Errorf("mcp http transport needs endpoint")
			}
		case MCPTransportStdio:
			// endpoint optional; hub attaches to launched pipes
		case "":
			return fmt.Errorf("mcp.transport is required")
		default:
			return fmt.Errorf("mcp.transport %q unsupported", mcp.Transport)
		}
	}
	return nil
}
