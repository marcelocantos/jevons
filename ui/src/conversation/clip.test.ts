// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
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
} from './clip';

describe('shouldClip', () => {
  it('clips a tall user wall and leaves a short bubble', () => {
    expect(shouldClip(400)).toBe(true);
    expect(shouldClip(100)).toBe(false);
    expect(shouldClip(224)).toBe(false);
    expect(shouldClip(226)).toBe(true);
  });
});

describe('clipClassName', () => {
  it('adds msg-clipped only when tall', () => {
    expect(clipClassName('bubble bubble-user', 400)).toContain('msg-clipped');
    expect(clipClassName('bubble bubble-user', 80)).toBe('bubble bubble-user');
  });
});

describe('expandTabChevron', () => {
  it('matches the vanilla pocket-tab glyphs', () => {
    expect(expandTabChevron(false)).toBe('\u25BE');
    expect(expandTabChevron(true)).toBe('\u25B4');
  });
});

describe('T166 clearance', () => {
  it('reserves tab height plus air above the collapse tab', () => {
    expect(expandedTabClearancePx(16)).toBeCloseTo(22.4);
    expect(paintedClipHeight(400, false)).toBe(224);
    expect(paintedClipHeight(400, true)).toBe(400);
  });
});

describe('T66 latest stays expanded', () => {
  it('expands the last messageish row when tall', () => {
    expect(lastMessageRowIndex(['user', 'steps', 'assistant'])).toBe(2);
    expect(shouldStayExpandedLatest({ isLatest: true, tall: true })).toBe(true);
    expect(shouldStayExpandedLatest({ isLatest: true, tall: false })).toBe(false);
    expect(shouldStayExpandedLatest({ isLatest: false, tall: true })).toBe(false);
    expect(shouldStayExpandedLatest({ isLatest: true, tall: true, userToggled: true })).toBe(false);
  });
});

describe('T246 stay expanded while on-screen', () => {
  it('anyPartInViewport: partial overlap stays material', () => {
    expect(anyPartInViewport(100, 50, 0, 300)).toBe(true);
    expect(anyPartInViewport(299, 50, 0, 300)).toBe(true);
    expect(anyPartInViewport(-49, 50, 0, 300)).toBe(true);
    expect(anyPartInViewport(0, 100, 100, 300)).toBe(false);
    expect(isFullyAboveViewport(0, 100, 100)).toBe(true);
    expect(anyPartInViewport(400, 50, 0, 300)).toBe(false);
    expect(anyPartInViewport(100, 0, 100, 300)).toBe(false);
  });

  it('shouldAutoCollapseOffScreen: only when fully off-screen, not latest', () => {
    expect(
      shouldAutoCollapseOffScreen({
        isLatest: true,
        autoExpanded: true,
        userToggled: false,
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
        autoExpanded: false,
        userToggled: false,
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
        userToggled: false,
        top: 250,
        height: 100,
        scrollTop: 0,
        clientHeight: 300,
      }),
    ).toBe(false);
    expect(
      shouldAutoCollapseOffScreen({
        isLatest: false,
        autoExpanded: true,
        userToggled: false,
        top: 0,
        height: 100,
        scrollTop: 200,
        clientHeight: 300,
      }),
    ).toBe(true);
    expect(
      shouldAutoCollapseOffScreen({
        isLatest: false,
        autoExpanded: true,
        userToggled: false,
        top: 800,
        height: 100,
        scrollTop: 0,
        clientHeight: 300,
      }),
    ).toBe(true);
  });
});

describe('T261 near-end in-view', () => {
  it('isNearTranscriptEnd: pin-bottom and mid-scroll', () => {
    expect(isNearTranscriptEnd(700, 1000, 300)).toBe(true);
    expect(isNearTranscriptEnd(680, 1000, 300, 48)).toBe(true);
    expect(isNearTranscriptEnd(600, 1000, 300, 48)).toBe(false);
    expect(isNearTranscriptEnd(0, 1000, 300)).toBe(false);
    expect(isNearTranscriptEnd(0, 200, 300)).toBe(true);
  });

  it('shouldAutoExpandInView: only tall + near-end + in viewport', () => {
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
        tall: false,
        nearEnd: true,
        userToggled: false,
        top: 700,
        height: 100,
        scrollTop: 600,
        clientHeight: 300,
      }),
    ).toBe(false);
    expect(
      shouldAutoExpandInView({
        tall: true,
        nearEnd: false,
        userToggled: false,
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
        userToggled: true,
        top: 700,
        height: 100,
        scrollTop: 600,
        clientHeight: 300,
      }),
    ).toBe(false);
    expect(
      shouldAutoExpandInView({
        tall: true,
        nearEnd: true,
        userToggled: false,
        top: 0,
        height: 100,
        scrollTop: 600,
        clientHeight: 300,
      }),
    ).toBe(false);
    expect(
      shouldAutoExpandInView({
        tall: true,
        nearEnd: true,
        userToggled: false,
        historyReplayActive: true,
        top: 700,
        height: 100,
        scrollTop: 600,
        clientHeight: 300,
      }),
    ).toBe(false);
  });

  it('shouldRunOffScreenCollapse: suppressed during history replay', () => {
    expect(shouldRunOffScreenCollapse(true)).toBe(false);
    expect(shouldRunOffScreenCollapse(false)).toBe(true);
    expect(shouldRunOffScreenCollapse(0)).toBe(true);
  });
});

describe('nextAutoExpanded', () => {
  it('latest tall stays open; off-screen prior collapses; manual wins', () => {
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
        height: 100,
        scrollTop: 600,
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
        nearEnd: true,
        historyReplayActive: false,
        top: 0,
        height: 100,
        scrollTop: 200,
        clientHeight: 300,
      }),
    ).toBe(false);
    expect(
      nextAutoExpanded({
        tall: true,
        isLatest: false,
        userToggled: true,
        expanded: true,
        autoExpanded: false,
        nearEnd: false,
        historyReplayActive: false,
        top: 0,
        height: 100,
        scrollTop: 500,
        clientHeight: 300,
      }),
    ).toBe(true);
  });
});
