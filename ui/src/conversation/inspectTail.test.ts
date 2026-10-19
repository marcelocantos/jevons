// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { existsSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { displayRows } from './display';
import { assistantProse, assistantTool, userTurn } from '../oracle/fixtures';
import { INSPECT_HISTORY_TURNS, inspectDisplayRows, tailInspectFrames } from './inspectTail';

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(here, '../../..');

const FIXTURE_TURNS = 200;
const SLOT_TURN = 185; // inside the last 30 of 200
const EARLY_SLOT_TURN = 5; // outside the window

function longInspectTape(): unknown[] {
  const frames: unknown[] = [];
  for (let i = 1; i <= FIXTURE_TURNS; i++) {
    frames.push(userTurn(`turn-${i}`));
    if (i === EARLY_SLOT_TURN) frames.push(assistantTool('T609EarlyRead', { path: 'early.go' }));
    if (i === SLOT_TURN) frames.push(assistantTool('T609SlotRead', { path: 'slot.go' }));
    frames.push(assistantProse(`ack-${i}`));
  }
  return frames;
}

function stepHas(rows: ReturnType<typeof inspectDisplayRows>, needle: string): boolean {
  return rows.some((r) => r.kind === 'steps' && (r.items || []).some((it) => it.text.includes(needle)));
}

describe('🎯T609 inspect tail', () => {
  it('the bound is a named constant, not a magic number, and is smaller than the 200-turn fixture', () => {
    expect(INSPECT_HISTORY_TURNS).toBe(30);
    expect(INSPECT_HISTORY_TURNS).toBeLessThan(FIXTURE_TURNS);
    const src = readFileSync(join(here, 'inspectTail.ts'), 'utf8');
    expect(src).toMatch(/export const INSPECT_HISTORY_TURNS = 30/);
    expect(src).not.toMatch(/slice\(\s*-?\d+\s*\)/);
  });

  it('a 200-turn fixture paints the bounded window and still slots a turn-slot inside it', () => {
    const tape = longInspectTape();
    const uncappedUsers = displayRows(tape, { inspect: true }).filter((r) => r.kind === 'user');
    expect(uncappedUsers, 'fixture must be long enough that only the cap explains the bound').toHaveLength(FIXTURE_TURNS);

    const rows = inspectDisplayRows(tape);
    const users = rows.filter((r) => r.kind === 'user');
    // Removing the cap (inspectDisplayRows → displayRows) makes this 200.
    expect(users.length).toBeLessThan(FIXTURE_TURNS);
    expect(users.length).toBe(INSPECT_HISTORY_TURNS);
    expect(users[0].text).toBe(`turn-${FIXTURE_TURNS - INSPECT_HISTORY_TURNS + 1}`);
    expect(users.some((r) => r.text === 'turn-1')).toBe(false);
    expect(users.some((r) => r.text === `turn-${FIXTURE_TURNS}`)).toBe(true);

    expect(stepHas(rows, 'T609SlotRead'), 'turn-slot inside the window must survive').toBe(true);
    expect(stepHas(rows, 'T609EarlyRead'), 'turn-slot before the window must not leak').toBe(false);
  });

  it('tailInspectFrames cuts on user turns, keeping the trailing slot frames', () => {
    const tape = longInspectTape();
    const tailed = tailInspectFrames(tape);
    const users = tailed.filter((f) => (f as { type?: string }).type === 'user');
    expect(users).toHaveLength(INSPECT_HISTORY_TURNS);
    expect(JSON.stringify(tailed)).toContain('T609SlotRead');
    expect(JSON.stringify(tailed)).not.toContain('T609EarlyRead');
  });

  it('comfortable / main fold is not tailed — inspect is the only bound surface', () => {
    const tape = longInspectTape();
    expect(displayRows(tape).filter((r) => r.kind === 'user')).toHaveLength(FIXTURE_TURNS);
  });

  it('AgentTranscript compact density uses inspectDisplayRows (fails if the cap is unwired)', () => {
    const src = readFileSync(join(here, '../components/AgentTranscript.tsx'), 'utf8');
    expect(src).toMatch(/inspectDisplayRows\(props\.frames\)/);
    expect(src).toMatch(/density === 'compact' \? inspectDisplayRows/);
  });

  it('vanilla T533 pending cases are gone with the frozen web/ surface, not left asserting a missing API', () => {
    expect(existsSync(join(repoRoot, 'web/scripts/agent_transcript_test.js'))).toBe(false);
    expect(existsSync(join(repoRoot, 'web'))).toBe(false);
  });
});
