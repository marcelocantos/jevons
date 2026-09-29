// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { reduceTranscriptBodies, tailBoundary } from './stream';
import { displayRows } from './display';

function assistant(text: string, sid?: string, stop?: string) {
  return {
    type: 'assistant',
    ...(sid ? { stream_id: sid } : {}),
    message: { content: text ? [{ type: 'text', text }] : [], ...(stop ? { stop_reason: stop } : {}) },
  };
}

function owner(text: string) {
  return { type: 'user', turn_origin: 'owner', message: { role: 'user', content: [{ type: 'text', text }] } };
}

const rows = (bodies: unknown[]) => displayRows(reduceTranscriptBodies(bodies).frames).map((r) => [r.kind, r.text]);

// 🎯T907: an owner message steered into a streaming answer is painted where
// it lands, but it no longer breaks the answer's sentence: the rest of the
// line stays with the bubble the owner's cut into.
describe('steered owner bubble and a streaming answer (T907)', () => {
  it('the 2026-09-29 screenshot: the cut line is finished above the owner bubble', () => {
    const got = rows([
      assistant('4. Silk screening pushes ink through mesh creating bold', 's1'),
      owner('Reply with exactly: held-a'),
      assistant(' graphic prints on fabric and paper.\n5. Red giants swell', 's1'),
      assistant(' when aging stars exhaust hydrogen.', 's1', 'end_turn'),
    ]);
    expect(got).toEqual([
      ['assistant', '4. Silk screening pushes ink through mesh creating bold graphic prints on fabric and paper.'],
      ['user', 'Reply with exactly: held-a'],
      ['assistant', '5. Red giants swell when aging stars exhaust hydrogen.'],
    ]);
  });

  it('a tail arriving in several deltas keeps going to the cut bubble until the line ends', () => {
    const got = rows([
      assistant('The weather in', 's1'),
      owner('status?'),
      assistant(' Paris', 's1'),
      assistant(' is mild', 's1'),
      assistant('. Status: all green.', 's1', 'end_turn'),
    ]);
    expect(got).toEqual([
      ['assistant', 'The weather in Paris is mild.'],
      ['user', 'status?'],
      ['assistant', 'Status: all green.'],
    ]);
  });

  it('a bubble that already ended its sentence is not owed anything (T504 unchanged)', () => {
    const got = rows([
      assistant('First part done.', 's1'),
      owner('next?'),
      assistant('Second part.', 's1', 'end_turn'),
    ]);
    expect(got).toEqual([
      ['assistant', 'First part done.'],
      ['user', 'next?'],
      ['assistant', 'Second part.'],
    ]);
  });

  it('a tool result ends what the cut bubble is owed', () => {
    const got = rows([
      assistant('Running the', 's1'),
      owner('go on'),
      { type: 'tool_result', content: 'ok' },
      assistant('Done.', 's1', 'end_turn'),
    ]).filter(([kind]) => kind !== 'tool_result' && kind !== 'tool');
    expect(got[0]).toEqual(['assistant', 'Running the']);
    expect(got.at(-1)).toEqual(['assistant', 'Done.']);
  });

  it('a continuation with no line end within the budget stays below the owner bubble', () => {
    const long = ' ' + 'word '.repeat(60) + 'end.';
    const got = rows([
      assistant('A cut', 's1'),
      owner('hey'),
      assistant(long, 's1', 'end_turn'),
    ]);
    expect(got[0]).toEqual(['assistant', 'A cut']);
    expect(got[1]).toEqual(['user', 'hey']);
    expect(got[2][1]).toContain('end.');
  });

  it('live 2026-09-29 18:37: a cut stream interrupted before its line ended keeps the line whole', () => {
    // Frames 11–19 of the T789.1 run on a real Anthropic overseer.
    const got = rows([
      assistant('4. Coins came first.\n5. The oboist ad', 'A'),
      owner('Reply with exactly: held-a'),
      assistant("y before the orchestra's tuning note.\n6. Deep beneath the se", 'A'),
      owner('Reply with exactly: held-b'),
      assistant('afloor, methane hydrates remain a', 'A'),
      owner('Reply with exactly: cut-held-a'),
      assistant('aborted', 'B', 'end_turn'),
      assistant('', 'C', 'end_turn'),
      assistant('held-aheld-bcut-held-a', 'D', 'end_turn'),
    ]);
    expect(got).toEqual([
      ['assistant', "4. Coins came first.\n5. The oboist ady before the orchestra's tuning note."],
      ['user', 'Reply with exactly: held-a'],
      ['assistant', '6. Deep beneath the seafloor, methane hydrates remain a'],
      ['user', 'Reply with exactly: held-b'],
      ['user', 'Reply with exactly: cut-held-a'],
      ['assistant', 'aborted'],
      ['assistant', 'held-aheld-bcut-held-a'],
    ]);
  });

  it('live 2026-09-29 18:31: a fragment arriving after the abort joins the cut line, not a bubble of its own', () => {
    const got = rows([
      assistant('1. The tinsmith hammered patterns into the lantern until light', 'A'),
      owner('Reply with exactly: cut-held-a'),
      assistant('aborted', 'B', 'end_turn'),
      assistant(' sc', 'A'),
      assistant('held-a', 'D', 'end_turn'),
    ]);
    expect(got).toEqual([
      ['assistant', '1. The tinsmith hammered patterns into the lantern until light sc'],
      ['user', 'Reply with exactly: cut-held-a'],
      ['assistant', 'aborted'],
      ['assistant', 'held-a'],
    ]);
  });

  it('tailBoundary finds the first line or sentence end', () => {
    expect(tailBoundary(' prints on paper.\n5. Red')).toBe(' prints on paper.'.length);
    expect(tailBoundary('\n5. Red')).toBe(1);
    expect(tailBoundary(' mid-sentence still')).toBe(-1);
    expect(tailBoundary('3.14 is pi')).toBe(-1);
  });
});
