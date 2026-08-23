// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { applyTranscriptPageKey, pageScrollDelta, transcriptPane, TRANSCRIPT_SCROLL_SEL } from './pageScroll';

describe('pageScrollDelta', () => {
  it('PageUp goes up 0.8 viewport, PageDown down', () => {
    expect(pageScrollDelta('PageUp', 1000)).toBe(-800);
    expect(pageScrollDelta('PageDown', 1000)).toBe(800);
    expect(pageScrollDelta('Home', 1000)).toBe(0);
  });

  it('transcript pane selector is #messages, not a missing .agent-transcript (🎯T537.2.8)', () => {
    expect(TRANSCRIPT_SCROLL_SEL).toContain('#messages');
    expect(TRANSCRIPT_SCROLL_SEL).not.toContain('agent-transcript');
  });

  it('PageUp scrolls #messages and leaves follow, even from a focused composer', () => {
    document.body.innerHTML =
      '<div id="chat-pane"><div id="messages"></div><textarea data-composer="main"></textarea></div>';
    const msgs = document.getElementById('messages') as HTMLElement;
    Object.defineProperty(msgs, 'clientHeight', { value: 1000, configurable: true });
    const moved: number[] = [];
    const left: string[] = [];
    const older: string[] = [];
    msgs.scrollBy = ((x: number, y: number) => {
      moved.push(y);
    }) as typeof msgs.scrollBy;
    msgs.addEventListener('jevons-leave-track', () => left.push('left'));
    msgs.addEventListener('jevons-page-older', () => older.push('page'));
    const input = document.querySelector('textarea') as HTMLTextAreaElement;
    expect(transcriptPane(input)?.id).toBe('messages');
    expect(applyTranscriptPageKey('PageUp', input)).toBe(true);
    expect(moved).toEqual([-800]);
    expect(left).toEqual(['left']);
    expect(older).toEqual(['page']);
    expect(applyTranscriptPageKey('PageDown', input)).toBe(true);
    expect(moved).toEqual([-800, 800]);
  });
});
