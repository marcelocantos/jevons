// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createElement } from 'react';
import { fireEvent, render } from '@testing-library/react';
import { expect } from 'vitest';
import { ClippedBubble } from '../../components/AgentTranscript';
import {
  clipClassName,
  DEFAULT_COLLAPSED_PX,
  expandTabChevron,
  expandedTabClearancePx,
  nextAutoExpanded,
  shouldClip,
  shouldStayExpandedLatest,
} from '../../conversation/clip';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

function withTallMsgBody<T>(fn: () => T): T {
  const proto = HTMLElement.prototype as unknown as { scrollHeight: number };
  const desc = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollHeight');
  Object.defineProperty(HTMLElement.prototype, 'scrollHeight', {
    configurable: true,
    get() {
      if ((this as HTMLElement).classList?.contains('msg-body')) return 400;
      return desc && desc.get ? desc.get.call(this) : 0;
    },
  });
  try {
    return fn();
  } finally {
    if (desc) Object.defineProperty(HTMLElement.prototype, 'scrollHeight', desc);
    else delete proto.scrollHeight;
  }
}

describeOracle(family('clip'), () => {
  itOracle(['T55', 'T77'], 'tall content clips; short does not', () => {
    expect(shouldClip(400)).toBe(true);
    expect(shouldClip(100)).toBe(false);
    expect(shouldClip(224)).toBe(false);
    expect(shouldClip(226)).toBe(true);
  });

  itOracle(['T55', 'T556'], 'pocket tab click expands a clipped bubble and click again collapses', () => {
    withTallMsgBody(() => {
      const { container } = render(
        createElement(ClippedBubble, {
          index: 0,
          kind: 'assistant',
          text: 'tall line\n'.repeat(40),
          start: 0,
          sealed: true,
          isLatest: false,
          nearEnd: false,
          historyReplayActive: false,
          scrollTop: 0,
          clientHeight: 200,
        }),
      );
      const msg = container.querySelector('.msg')!;
      const btn = container.querySelector<HTMLButtonElement>('.msg-expand-tab')!;
      expect(msg).toBeTruthy();
      expect(btn).toBeTruthy();
      expect(msg.classList.contains('msg-clipped')).toBe(true);
      expect(btn.getAttribute('aria-expanded')).toBe('false');
      fireEvent.click(btn);
      expect(msg.classList.contains('msg-clipped')).toBe(false);
      expect(btn.getAttribute('aria-expanded')).toBe('true');
      expect(btn.getAttribute('aria-label')).toBe('Collapse');
      fireEvent.click(btn);
      expect(msg.classList.contains('msg-clipped')).toBe(true);
      expect(btn.getAttribute('aria-expanded')).toBe('false');
      expect(btn.getAttribute('aria-label')).toBe('Expand');
    });
  });

  itOracle(['T77', 'T556'], 'manual expand survives a rerender that would auto-collapse', () => {
    withTallMsgBody(() => {
      const props = {
        index: 0,
        kind: 'assistant' as const,
        text: 'tall line\n'.repeat(40),
        start: 0,
        sealed: true,
        isLatest: false,
        nearEnd: false,
        historyReplayActive: false,
        scrollTop: 0,
        clientHeight: 200,
      };
      const { container, rerender } = render(createElement(ClippedBubble, props));
      const btn = container.querySelector<HTMLButtonElement>('.msg-expand-tab')!;
      fireEvent.click(btn);
      expect(container.querySelector('.msg')!.classList.contains('msg-clipped')).toBe(false);
      rerender(createElement(ClippedBubble, { ...props, scrollTop: 8000, clientHeight: 200 }));
      expect(container.querySelector('.msg')!.classList.contains('msg-clipped')).toBe(false);
      expect(btn.getAttribute('aria-expanded')).toBe('true');
    });
  });

  itOracle('T106', 'clipped class and pocket-tab chevrons match vanilla glyphs', () => {
    expect(clipClassName('bubble bubble-user', 400)).toContain('msg-clipped');
    expect(clipClassName('bubble bubble-user', 80)).toBe('bubble bubble-user');
    expect(expandTabChevron(false)).toBe('\u25BE');
    expect(expandTabChevron(true)).toBe('\u25B4');
  });

  itOracle(['T106', 'T559'], 'clipped pocket is an inset cavity + body mask, not a type scrim', () => {
    const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../cockpit.css'), 'utf8');
    const clipped = css.match(/\.msg\.msg-clipped\s*\{[^}]+\}/);
    const body = css.match(/\.msg\.msg-clipped\s*>\s*\.msg-body\s*\{[^}]+\}/);
    expect(clipped?.[0]).toMatch(/box-shadow:\s*inset/);
    expect(body?.[0]).toMatch(/mask-image:\s*linear-gradient\(to bottom/);
    expect(body?.[0]).toMatch(/transparent/);
    expect(css).not.toMatch(/\.msg\.msg-clipped\s*>\s*\.msg-body\s*\{[^}]*rgba\(\s*0\s*,\s*0\s*,\s*0/);
  });

  itOracle('T66', 'latest assistant starts expanded (same rule as latest user)', () => {
    expect(shouldStayExpandedLatest({ isLatest: true, tall: true })).toBe(true);
    expect(shouldStayExpandedLatest({ isLatest: false, tall: true })).toBe(false);
    expect(
      nextAutoExpanded({
        tall: true,
        isLatest: true,
        userToggled: false,
        expanded: false,
        autoExpanded: false,
        nearEnd: false,
        historyReplayActive: false,
        top: 0,
        height: 400,
        scrollTop: 0,
        clientHeight: 200,
      }),
    ).toBe(true);
  });

  itOracle('T166', 'expanded tall bubble clears last line above the collapse tab', () => {
    expect(expandedTabClearancePx(16)).toBeGreaterThan(DEFAULT_COLLAPSED_PX * 0);
    expect(expandedTabClearancePx(16)).toBeCloseTo((1.05 + 0.35) * 16);
  });

  itOracle('T246', 'new messages stay expanded until scrolled out of view', () => {
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
        height: 400,
        scrollTop: 0,
        clientHeight: 200,
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
        height: 400,
        scrollTop: 500,
        clientHeight: 200,
      }),
    ).toBe(false);
  });

  itOracle('T261', 'in-view messages near the end never collapse', () => {
    expect(
      nextAutoExpanded({
        tall: true,
        isLatest: false,
        userToggled: false,
        expanded: false,
        autoExpanded: false,
        nearEnd: true,
        historyReplayActive: false,
        top: 100,
        height: 400,
        scrollTop: 80,
        clientHeight: 200,
      }),
    ).toBe(true);
  });

  itOracle('T480', 'main and sidebar Transcript use the same size-clip', () => {
    expect(clipClassName('bubble', 400)).toBe(clipClassName('bubble', 400));
    expect(DEFAULT_COLLAPSED_PX).toBe(224);
  });
});
