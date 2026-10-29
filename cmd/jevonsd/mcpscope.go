// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpattach"
	"github.com/marcelocantos/jevons/internal/mcpscope"
	"github.com/marcelocantos/jevons/internal/mcpup"
	"github.com/marcelocantos/jevons/internal/server"
)

// registerMCPEndpoints stamps the live jevonsmcp URL for Claudia mints
// (🎯T379: served port). Neither daily nor isolate boot writes provider
// configs — seats get AgentDef.MCPServers at create. Isolates point
// LoadMCP at state_dir/mcp (usually missing) so they do not inherit HOME.
func registerMCPEndpoints(cfg config.Config, host string, port int) mcpattach.Args {
	return registerMCPEndpointsAt(cfg, host, port, fleetMCPAttach(cfg, host, port))
}

func fleetMCPAttach(cfg config.Config, host string, port int) mcpattach.Args {
	a := mcpattach.Args{
		Name: fleetMCPName(cfg),
		URL:  mcpattach.HTTPURL(host, port),
	}
	if config.IsDailyStateDir(cfg.StateDir) {
		return a
	}
	dir := filepath.Join(cfg.StateDir, "mcp")
	a.ClaudeJSON = filepath.Join(dir, "claude.json")
	a.GrokTOML = filepath.Join(dir, "grok.toml")
	a.CodexTOML = filepath.Join(dir, "codex.toml")
	a.CursorJSON = filepath.Join(dir, "cursor.json")
	a.Isolate = true
	return a
}

func registerMCPEndpointsAt(cfg config.Config, host string, port int, a mcpattach.Args) mcpattach.Args {
	if a.Name == "" {
		a = fleetMCPAttach(cfg, host, port)
	}
	if a.Isolate {
		slog.Info("isolate jevonsmcp is session-scoped; no provider config write",
			"name", a.Name, "url", a.URL)
		return a
	}
	if err := mcpattach.Scrub(a); err != nil {
		slog.Warn("could not scrub jevonsmcp from user-scope provider configs",
			"name", a.Name, "err", err)
	} else {
		slog.Info("jevonsmcp is session-scoped; user-scope provider configs scrubbed",
			"name", a.Name, "url", a.URL)
	}
	return a
}

func fleetMCPName(cfg config.Config) string {
	if cfg.MCPServerName == "" {
		return mcpscope.ServerName
	}
	return cfg.MCPServerName
}

// overseerMCPServerSpec is the name+URL advertised after bind (🎯T58/T379).
func overseerMCPServerSpec(cfg config.Config, host string, port int) (name, url string) {
	return fleetMCPName(cfg), mcpattach.HTTPURL(host, port)
}

// mountHTTPUpstreamProxy puts owner-map HTTP MCP behind jevonsd loopback
// and reseeds durable OAuth tokens for silent refresh (🎯T520).
func mountHTTPUpstreamProxy(mux *http.ServeMux, cfg config.Config, watcher *config.Watcher, srv *server.Server, host string, port int, attach mcpattach.Args, onToolsCall func(name string, args map[string]any), applyAttach func(mcpattach.Args)) *mcpup.Host {
	load := &claudia.LoadMCPArgs{WorkDir: cfg.WorkDir}
	if attach.ClaudeJSON != "" || attach.GrokTOML != "" || attach.CodexTOML != "" || attach.CursorJSON != "" {
		load = &claudia.LoadMCPArgs{
			ClaudeJSON: attach.ClaudeJSON,
			GrokTOML:   attach.GrokTOML,
			CodexTOML:  attach.CodexTOML,
			CursorJSON: attach.CursorJSON,
			WorkDir:    cfg.WorkDir,
		}
	}
	inv, err := claudia.LoadMCP(load)
	if err != nil {
		slog.Warn("mcp upstream proxy: LoadMCP failed — HTTP owner-map not proxied", "err", err)
		return nil
	}
	skip := map[string]bool{fleetMCPName(cfg): true}
	store, err := mcpup.OpenStore(mcpup.DefaultPath(cfg.StateDir))
	if err != nil {
		slog.Warn("mcp upstream proxy: token store open failed", "err", err)
		store = nil
	}
	upstreams, err := mcpup.OpenUpstreamRegistry(mcpup.UpstreamRegistryPath(cfg.StateDir))
	if err != nil {
		slog.Warn("mcp upstream proxy: upstream registry open failed", "err", err)
		upstreams = nil
	}
	mountArgs := func(servers []claudia.MCPServer) *mcpup.MountArgs {
		return &mcpup.MountArgs{
			PublicBase:  mcpup.PublicBase(host, port),
			Servers:     servers,
			SkipNames:   skip,
			Store:       store,
			Upstreams:   upstreams,
			OnToolsCall: onToolsCall,
		}
	}
	var upHost *mcpup.Host
	remountHTTP := func(next *claudia.MCPInventory) {
		if next == nil {
			return
		}
		h, err := mcpup.Mount(mux, mountArgs(next.Servers))
		if err != nil {
			slog.Warn("mcp upstream proxy: hot remount failed — restart the daemon to refresh HTTP grants", "err", err)
			return
		}
		upHost = h
		a := attach
		a.Proxied = h.Advertised()
		if applyAttach != nil {
			applyAttach(a)
		}
	}
	watchMCPOwnerMap(watcher, load, inv, srv, skip, remountHTTP)
	if inv == nil || len(inv.Servers) == 0 {
		return upHost
	}
	h, err := mcpup.Mount(mux, mountArgs(inv.Servers))
	if err != nil {
		slog.Warn("mcp upstream proxy: mount failed", "err", err)
		return nil
	}
	upHost = h
	return h
}

// watchMCPOwnerMap registers every file LoadMCP read (🎯T574 mcpscope
// family). Owner-map edits are hot (🎯T1133): new seats read LoadMCP at
// mint; the HTTP upstream proxy remounts in place when its grant set moves.
func watchMCPOwnerMap(watcher *config.Watcher, load *claudia.LoadMCPArgs, boot *claudia.MCPInventory, srv *server.Server, skipNames map[string]bool, remountHTTP func(*claudia.MCPInventory)) {
	if watcher == nil || boot == nil {
		return
	}
	curFP := mcpMapFingerprint(boot, skipNames)
	curHTTP := mcpHTTPUpstreamFingerprint(boot, skipNames)
	var curInv = boot
	for _, src := range boot.Sources {
		path := src
		if _, err := config.Watch(watcher, &config.WatchArgs[string]{
			Path: path,
			Load: func(string) (string, error) {
				inv, err := claudia.LoadMCP(load)
				if err != nil {
					return "", err
				}
				return mcpMapFingerprint(inv, skipNames), nil
			},
			Fallback: curFP,
			OnChange: func(fp string) {
				if fp == curFP {
					return
				}
				inv, err := claudia.LoadMCP(load)
				if err != nil {
					slog.Warn("mcp owner map reload: LoadMCP failed", "path", path, "err", err)
					return
				}
				ch := mcpMapDiff(curInv, inv, skipNames)
				notifyMCPOwnerMapChange(srv, path, ch)
				httpFP := mcpHTTPUpstreamFingerprint(inv, skipNames)
				if httpFP != curHTTP && remountHTTP != nil {
					remountHTTP(inv)
					curHTTP = httpFP
				}
				curFP = fp
				curInv = inv
			},
		}); err != nil {
			slog.Warn("mcp owner map watch: initial load failed", "path", path, "err", err)
		}
	}
}
