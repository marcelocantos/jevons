// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
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

describeOracle(family('clip'), () => {
  itOracle(['T55', 'T77'], 'tall content clips; short does not', () => {
    expect(shouldClip(400)).toBe(true);
    expect(shouldClip(100)).toBe(false);
    expect(shouldClip(224)).toBe(false);
    expect(shouldClip(226)).toBe(true);
  });

  itOracle('T106', 'clipped class and pocket-tab chevrons match vanilla glyphs', () => {
    expect(clipClassName('bubble bubble-user', 400)).toContain('msg-clipped');
    expect(clipClassName('bubble bubble-user', 80)).toBe('bubble bubble-user');
    expect(expandTabChevron(false)).toBe('\u25BE');
    expect(expandTabChevron(true)).toBe('\u25B4');
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
