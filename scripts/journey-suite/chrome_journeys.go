// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

// Chrome-pack journeys (🎯T540.1). Covers lists are the retired targets
// each journey must re-prove. Referent = ledger acceptance, not today's React.

// J19-connect-tail covers (transcript surface). Live paint stays J19.
const chromeConnectCovers = "T23 T30.2 T39 T52 T58 T63 T64 T71 T87 T89 T91 T94 T116 T119 T119.1 T119.6 T119.7 T119.8 T122 T138 T143 T151 T159 T161 T202 T210 T223 T224 T233 T238 T240 T242 T245 T249 T256 T257 T258 T259 T260 T264 T272 T273 T279 T281 T289 T300 T306 T314 T327 T336 T341 T347 T350 T351 T361 T362 T363 T369 T374 T381 T382 T384 T479 T483 T491 T494 T496 T504 T513"

// J22-send-once: T52 T159 T210 T223 T228 T249 T279 T281 T382 T384 T496 T504 T513
const chromeSendCovers = "T52 T159 T210 T223 T228 T249 T279 T281 T382 T384 T496 T504 T513"

// J23-fold-md: T55 T57 T59 T66 T74 T75 T77 T106 T145 T146 T147 T150 T166 T246 T261 T480
const chromeFoldCovers = "T55 T57 T59 T66 T74 T75 T77 T106 T145 T146 T147 T150 T166 T246 T261 T480"

// J24-composer: T21 T69 T70 T70.1 T76 T80 T88 T113 T123 T126 T127 T132 T133 T149 T153 T154 T183 T192 T227 T228 T235 T239 T241 T307 T349 T366 T368 T478
const chromeComposerCovers = "T21 T69 T70 T70.1 T76 T80 T88 T113 T123 T126 T127 T132 T133 T149 T153 T154 T183 T192 T227 T228 T235 T239 T241 T307 T349 T366 T368 T478"

// J25-fleet-sidebar: T68 T72 T72.1 T73 T81 T82 T84 T111.3 T115 T118 T124 T200 T211 T285.2 T287 T293 T295 T296 T298 T299 T301 T302 T311 T312 T323 T348 T383 T412 T474 T506 T507 T508
const chromeFleetCovers = "T68 T72 T72.1 T73 T81 T82 T84 T111.3 T115 T118 T124 T200 T211 T285.2 T287 T293 T295 T296 T298 T299 T301 T302 T311 T312 T323 T348 T383 T412 T474 T506 T507 T508"

// J26-aside: T65 T93 T95 T95.1 T99 T134 T135 T136 T152 T157 T164 T167 T205 T216 T217 T221 T247 T250 T251 T252 T263 T265 T269 T270 T275 T309 T325.3 T329 T365 T367 T371 T372
const chromeAsideCovers = "T65 T93 T95 T95.1 T99 T134 T135 T136 T152 T157 T164 T167 T205 T216 T217 T221 T247 T250 T251 T252 T263 T265 T269 T270 T275 T309 T325.3 T329 T365 T367 T371 T372"

// J27-frontier: T131 T168 T173 T174 T175 T177 T179 T181 T182 T184 T185 T186 T187 T189 T190 T196 T198 T199 T203 T208 T230 T231 T255 T266 T267 T268 T271 T274 T278 T280 T294 T331 T332 T340
const chromeFrontierCovers = "T131 T168 T173 T174 T175 T177 T179 T181 T182 T184 T185 T186 T187 T189 T190 T196 T198 T199 T203 T208 T230 T231 T255 T266 T267 T268 T271 T274 T278 T280 T294 T331 T332 T340"

// J28-ticker-chrome: T83 T83.1 T103 T117 T138 T140 T204 T237 T248 T309.1 T317 T319 T326 T345 T354 T355 T374 T390 T390.1.3 T390.1.6 T390.1.6.1 T489
const chromeTickerCovers = "T83 T83.1 T103 T117 T138 T140 T204 T237 T248 T309.1 T317 T319 T326 T345 T354 T355 T374 T390 T390.1.3 T390.1.6 T390.1.6.1 T489"

const (
	chromeFleetAgent = "jv-t540-fleet-oracle"
	chromeSendToken  = "T540SEND-ONCE"
	chromeTouchTok   = "T540CHROME-LIVE"
)

func init() {
	_ = chromeConnectCovers
}

func (s *suite) jSendOnce() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	_ = chromeSendCovers
	surface, err := s.startReactSurface()
	if err != nil {
		return err
	}
	defer surface.stop()
	shot := filepath.Join(s.stateDir, "j22-send.png")
	if err := s.runReactPaint(surface.host, "send", shot); err != nil {
		return err
	}
	return s.liveOwnerTouch(chromeSendToken)
}

func (s *suite) jFoldMd() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	_ = chromeFoldCovers
	journal := filepath.Join(s.stateDir, "chatlog", overseerName+".jsonl")
	if err := seedFoldMdJournal(journal); err != nil {
		return fmt.Errorf("seed fold journal: %w", err)
	}
	surface, err := s.startReactSurface()
	if err != nil {
		return err
	}
	defer surface.stop()
	shot := filepath.Join(s.stateDir, "j23-fold.png")
	if err := s.runReactPaint(surface.host, "fold-md", shot); err != nil {
		return err
	}
	return s.liveOwnerTouch(chromeTouchTok + "-fold")
}

func (s *suite) jComposerChrome() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	_ = chromeComposerCovers
	surface, err := s.startReactSurface()
	if err != nil {
		return err
	}
	defer surface.stop()
	shot := filepath.Join(s.stateDir, "j24-composer.png")
	if err := s.runReactPaint(surface.host, "composer", shot); err != nil {
		return err
	}
	return s.liveOwnerTouch(chromeTouchTok + "-composer")
}

func (s *suite) jFleetSidebar() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	_ = chromeFleetCovers
	want := chromeFleetAgent
	if _, err := s.AgentStart(chromeFleetAgent, s.workdir, "jevons", overseerName); err != nil {
		if out := asOutage("j25 fleet mint", err); out != nil {
			return out
		}
		if strings.Contains(err.Error(), "host_saturated") {
			// T460 refused a new pane. Prove T68/T72 on the live overseer
			// that is already in the isolate graph — do not skip the tree.
			want = overseerName
		} else {
			return fmt.Errorf("mint %s: %w", chromeFleetAgent, err)
		}
	} else {
		defer func() { _, _ = s.AgentKill(chromeFleetAgent, "jevons") }()
	}
	surface, err := s.startReactSurface()
	if err != nil {
		return err
	}
	defer surface.stop()
	shot := filepath.Join(s.stateDir, "j25-fleet.png")
	if err := s.runReactPaint(surface.host, "fleet", shot, want); err != nil {
		return err
	}
	return s.liveOwnerTouch(chromeTouchTok + "-fleet")
}

func (s *suite) jAsideChrome() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	_ = chromeAsideCovers
	surface, err := s.startReactSurface()
	if err != nil {
		return err
	}
	defer surface.stop()
	shot := filepath.Join(s.stateDir, "j26-aside.png")
	if err := s.runReactPaint(surface.host, "aside", shot); err != nil {
		return err
	}
	return s.liveOwnerTouch(chromeTouchTok + "-aside")
}

func (s *suite) jFrontierChrome() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	_ = chromeFrontierCovers
	surface, err := s.startReactSurface()
	if err != nil {
		return err
	}
	defer surface.stop()
	shot := filepath.Join(s.stateDir, "j27-frontier.png")
	if err := s.runReactPaint(surface.host, "frontier", shot); err != nil {
		return err
	}
	return s.liveOwnerTouch(chromeTouchTok + "-frontier")
}

func (s *suite) jTickerChrome() error {
	if err := portguard.RefuseDaily(s.port); err != nil {
		return err
	}
	_ = chromeTickerCovers
	surface, err := s.startReactSurface()
	if err != nil {
		return err
	}
	defer surface.stop()
	shot := filepath.Join(s.stateDir, "j28-ticker.png")
	if err := s.runReactPaint(surface.host, "ticker", shot); err != nil {
		return err
	}
	return s.liveOwnerTouch(chromeTouchTok + "-ticker")
}

func seedFoldMdJournal(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	base := time.Now().UTC().Add(-3 * time.Minute)
	var b strings.Builder
	write := func(obj any) {
		raw, _ := json.Marshal(obj)
		b.Write(raw)
		b.WriteByte('\n')
	}
	write(map[string]any{
		"type": "user", "timestamp": base.Format(time.RFC3339),
		"message": map[string]any{"role": "user", "content": "draw the graph"},
	})
	write(map[string]any{
		"type": "assistant", "timestamp": base.Add(time.Second).Format(time.RFC3339),
		"message": map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "text", "text": "```mermaid\ngraph TD\nA-->B\n```"},
			},
			"stop_reason": "end_turn",
		},
	})
	write(map[string]any{
		"type": "user", "timestamp": base.Add(2 * time.Second).Format(time.RFC3339),
		"message": map[string]any{"role": "user", "content": "silent please"},
	})
	write(map[string]any{
		"type": "assistant", "timestamp": base.Add(3 * time.Second).Format(time.RFC3339),
		"message": map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "text", "text": "[silent] do not paint this as an owner bubble"},
			},
			"stop_reason": "end_turn",
		},
	})
	var tall strings.Builder
	tall.WriteString("TALL-CLIP-BODY\n")
	tall.WriteString("```mermaid\ngraph TD\nA-->B\n```\n")
	for i := 0; i < 40; i++ {
		tall.WriteString("padding line for 14rem clip contract\n")
	}
	write(map[string]any{
		"type": "user", "timestamp": base.Add(4 * time.Second).Format(time.RFC3339),
		"message": map[string]any{"role": "user", "content": "show a wall of text"},
	})
	write(map[string]any{
		"type": "assistant", "timestamp": base.Add(5 * time.Second).Format(time.RFC3339),
		"message": map[string]any{
			"role":        "assistant",
			"content":     []any{map[string]any{"type": "text", "text": tall.String()}},
			"stop_reason": "end_turn",
		},
	})
	if _, err := f.WriteString(b.String()); err != nil {
		return err
	}
	return f.Sync()
}
