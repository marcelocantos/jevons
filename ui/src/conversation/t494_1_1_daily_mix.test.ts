// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { displayRows } from './display';
import { agentNote, assistantProse, assistantTool, userTurn } from '../oracle/fixtures';

/** 🎯T494.1.1: fold coverage for the daily replay class mix — not host
 * historyReplayActive (see J19 / journey), but the fold the React pane uses. */
describe('T494.1.1 daily replay mix', () => {
  it('coalesces progress/status/notes/tools between owner turns', () => {
    const frames: unknown[] = [];
    for (let i = 0; i < 2; i++) {
      frames.push(userTurn(`ROOThist-${i}`));
      frames.push(assistantProse(`ack ${i}`));
      for (let p = 0; p < 8; p++) {
        frames.push({ type: 'progress', phase: 'tool', step: 'Read' });
      }
      frames.push(assistantTool('Read'));
      frames.push(agentNote(`note ${i}`));
      frames.push({ type: 'status', text: 'working' });
    }
    const rows = displayRows(frames);
    expect(rows.length).toBeLessThan(frames.length - 2);
    const steps = rows.filter((r) => r.kind === 'steps');
    expect(steps.length).toBeGreaterThan(0);
    const items = steps.flatMap((s) => s.items || []);
    expect(items.some((it) => it.cls === 'tool-use')).toBe(true);
    expect(items.some((it) => it.cls === 'agent-note')).toBe(true);
  });

  it('text-only pairs stay the failed-oracle shape (control)', () => {
    const frames = [userTurn('a'), assistantProse('b'), userTurn('c'), assistantProse('d')];
    const rows = displayRows(frames);
    expect(rows.map((r) => r.kind)).toEqual(['user', 'assistant', 'user', 'assistant']);
    expect(rows.length).toBe(frames.length);
  });
});
