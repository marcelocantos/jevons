// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect } from 'vitest';
import {
  anyPartInViewport,
  clipClassName,
  expandTabChevron,
  expandedTabClearancePx,
  isFullyAboveViewport,
  isNearTranscriptEnd,
  lastMessageRowIndex,
  nextAutoExpanded,
  paintedClipHeight,
  shouldAutoCollapseOffScreen,
  shouldAutoExpandInView,
  shouldClip,
  shouldRunOffScreenCollapse,
  shouldStayExpandedLatest,
} from '../../conversation/clip';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const uiSrc = join(dirname(fileURLToPath(import.meta.url)), '../..');

describeOracle(family('clip'), () => {
  itOracle(['T55'], 'tall content clips; short does not', () => {
    expect(shouldClip(400)).toBe(true);
    expect(shouldClip(100)).toBe(false);
    expect(shouldClip(224)).toBe(false);
    expect(shouldClip(226)).toBe(true);
    expect(shouldClip(0)).toBe(false);
    expect(shouldClip(400, 0)).toBe(false);
  });

  itOracle('T66', 'latest assistant starts expanded (same rule as latest user)', () => {
    expect(lastMessageRowIndex(['user', 'steps', 'assistant'])).toBe(2);
    expect(lastMessageRowIndex(['user', 'assistant', 'user'])).toBe(2);
    expect(shouldStayExpandedLatest({ isLatest: true, tall: true })).toBe(true);
    expect(shouldStayExpandedLatest({ isLatest: true, tall: true, userToggled: true })).toBe(false);
    expect(
      nextAutoExpanded({
        tall: true,
        isLatest: true,
        userToggled: false,
        expanded: false,
        autoExpanded: false,
        nearEnd: true,
        historyReplayActive: false,
        top: 700,
        height: 400,
        scrollTop: 600,
        clientHeight: 300,
      }),
    ).toBe(true);
    const paint = readFileSync(join(uiSrc, 'components/AgentTranscript.tsx'), 'utf8');
    expect(paint).toMatch(/nextAutoExpanded/);
    expect(paint).toMatch(/shouldStayExpandedLatest|isLatest/);
  });

  itOracle('T77', 'ceasing to be latest does not itself collapse (T246)', () => {
    expect(
      shouldAutoCollapseOffScreen({
        isLatest: false,
        autoExpanded: true,
        userToggled: false,
        top: 250,
        height: 100,
        scrollTop: 0,
        clientHeight: 300,
      }),
    ).toBe(false);
    expect(
      nextAutoExpanded({
        tall: true,
        isLatest: false,
        userToggled: false,
        expanded: true,
        autoExpanded: true,
        nearEnd: true,
        historyReplayActive: false,
        top: 250,
        height: 100,
        scrollTop: 0,
        clientHeight: 300,
      }),
    ).toBe(true);
  });

  itOracle('T106', 'clipped class and pocket-tab chevrons match vanilla glyphs', () => {
    expect(clipClassName('msg user', 400)).toContain('msg-clipped');
    expect(clipClassName('msg user', 80)).toBe('msg user');
    expect(expandTabChevron(false)).toBe('\u25BE');
    expect(expandTabChevron(true)).toBe('\u25B4');
    const css = readFileSync(join(uiSrc, 'cockpit.css'), 'utf8');
    expect(css).toMatch(/\.msg\.msg-clipped\s*>\s*\.msg-body/);
    expect(css).toMatch(/--collapsed-max-height:\s*14rem/);
    expect(css).toMatch(/\.msg-clip-fade/);
  });

  itOracle('T166', 'expanded tall bubble clears last line above the collapse tab', () => {
    expect(expandedTabClearancePx(16)).toBeCloseTo(22.4);
    expect(paintedClipHeight(400, true)).toBe(400);
    expect(paintedClipHeight(400, false)).toBe(224);
    const css = readFileSync(join(uiSrc, 'cockpit.css'), 'utf8');
    expect(css).toMatch(/--expand-tab-clearance:\s*0\.35rem/);
    expect(css).toMatch(/--expand-tab-height:\s*1\.05rem/);
    expect(css).toMatch(/\.msg:has\(>\s*\.msg-expand-tab\):not\(\.msg-clipped\)/);
    expect(css).toMatch(
      /padding-bottom:\s*calc\(var\(--expand-tab-height\)\s*\+\s*var\(--expand-tab-clearance\)\)/,
    );
  });

  itOracle('T246', 'new messages stay expanded until scrolled out of view', () => {
    expect(anyPartInViewport(100, 50, 0, 300)).toBe(true);
    expect(anyPartInViewport(299, 50, 0, 300)).toBe(true);
    expect(anyPartInViewport(-49, 50, 0, 300)).toBe(true);
    expect(anyPartInViewport(0, 100, 100, 300)).toBe(false);
    expect(isFullyAboveViewport(0, 100, 100)).toBe(true);
    expect(anyPartInViewport(400, 50, 0, 300)).toBe(false);
    expect(
      shouldAutoCollapseOffScreen({
        isLatest: true,
        autoExpanded: true,
        top: 0,
        height: 100,
        scrollTop: 500,
        clientHeight: 300,
      }),
    ).toBe(false);
    expect(
      shouldAutoCollapseOffScreen({
        isLatest: false,
        autoExpanded: true,
        userToggled: true,
        top: 0,
        height: 100,
        scrollTop: 500,
        clientHeight: 300,
      }),
    ).toBe(false);
    expect(
      shouldAutoCollapseOffScreen({
        isLatest: false,
        autoExpanded: true,
        top: 0,
        height: 100,
        scrollTop: 200,
        clientHeight: 300,
      }),
    ).toBe(true);
    expect(
      nextAutoExpanded({
        tall: true,
        isLatest: false,
        userToggled: false,
        expanded: true,
        autoExpanded: true,
        nearEnd: false,
        historyReplayActive: false,
        top: 0,
        height: 100,
        scrollTop: 200,
        clientHeight: 300,
      }),
    ).toBe(false);
  });

  itOracle('T261', 'in-view messages near the end never collapse', () => {
    expect(isNearTranscriptEnd(700, 1000, 300)).toBe(true);
    expect(isNearTranscriptEnd(680, 1000, 300, 48)).toBe(true);
    expect(isNearTranscriptEnd(600, 1000, 300, 48)).toBe(false);
    expect(isNearTranscriptEnd(0, 200, 300)).toBe(true);
    expect(shouldRunOffScreenCollapse(true)).toBe(false);
    expect(shouldRunOffScreenCollapse(false)).toBe(true);
    expect(
      shouldAutoExpandInView({
        tall: true,
        nearEnd: true,
        userToggled: false,
        historyReplayActive: false,
        top: 700,
        height: 100,
        scrollTop: 600,
        clientHeight: 300,
      }),
    ).toBe(true);
    expect(
      shouldAutoExpandInView({
        tall: true,
        nearEnd: false,
        top: 100,
        height: 100,
        scrollTop: 80,
        clientHeight: 300,
      }),
    ).toBe(false);
    expect(
      shouldAutoExpandInView({
        tall: true,
        nearEnd: true,
        historyReplayActive: true,
        top: 700,
        height: 100,
        scrollTop: 600,
        clientHeight: 300,
      }),
    ).toBe(false);
    expect(
      nextAutoExpanded({
        tall: true,
        isLatest: false,
        userToggled: false,
        expanded: false,
        autoExpanded: false,
        nearEnd: true,
        historyReplayActive: false,
        top: 700,
        height: 100,
        scrollTop: 600,
        clientHeight: 300,
      }),
    ).toBe(true);
  });

  itOracle('T480', 'main and sidebar Transcript use the same size-clip', () => {
    expect(shouldClip(400)).toBe(true);
    expect(shouldClip(40)).toBe(false);
    const clip = readFileSync(join(uiSrc, 'conversation/clip.ts'), 'utf8');
    expect(clip).not.toMatch(/user_info|git_status|classifyInspect|system-reminder|injectKind|standing-brief/);
    expect(clip).not.toMatch(/if\s*\(\s*role\s*===/);
    const paint = readFileSync(join(uiSrc, 'components/AgentTranscript.tsx'), 'utf8');
    expect(paint).toMatch(/shouldClip\(fullH\)/);
    expect(paint).toMatch(/clipClassName/);
    expect(paint).toMatch(/nextAutoExpanded/);
    expect(paint).toMatch(/kind !== ['"]steps['"]/);
    const app = readFileSync(join(uiSrc, 'App.tsx'), 'utf8');
    expect(app.split('<AgentInteraction').length - 1).toBe(2);
    expect(app).toContain('density="comfortable"');
    expect(app).toContain('density="compact"');
    const interaction = readFileSync(join(uiSrc, 'components/AgentInteraction.tsx'), 'utf8');
    expect(interaction).toMatch(/<AgentTranscript/);
    expect(interaction).not.toMatch(/SidebarTranscript|InspectTranscript|layoutSizeClip/);
    const css = readFileSync(join(uiSrc, 'cockpit.css'), 'utf8');
    expect(css).toMatch(/#agent-inspect-body\s*>\s*\.msg\.msg-clipped\s*>\s*\.msg-body/);
  });
});
