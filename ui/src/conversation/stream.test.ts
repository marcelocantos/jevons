// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { applyTranscriptFrame, emptyStream, reduceTranscriptBodies } from './stream';
import { displayRows } from './display';

function assistant(text: string, sid?: string, stop?: string) {
  return {
    type: 'assistant',
    ...(sid ? { stream_id: sid } : {}),
    message: {
      content: text ? [{ type: 'text', text }] : [],
      ...(stop ? { stop_reason: stop } : {}),
    },
  };
}

describe('applyTranscriptFrame', () => {
  it('joins unlabeled token chunks into one frame', () => {
    const { frames } = reduceTranscriptBodies([
      assistant('Hello'),
      assistant(' world'),
      assistant('', undefined, 'end_turn'),
    ]);
    expect(frames).toHaveLength(1);
    expect(displayRows(frames).map((r) => r.text)).toEqual(['Hello world']);
  });

  it('segment edge after tool_result uses a blank line, not glue', () => {
    let frames: unknown[] = [];
    let stream = emptyStream();
    ({ frames, stream } = applyTranscriptFrame(frames, stream, assistant('Before', 's')));
    ({ frames, stream } = applyTranscriptFrame(frames, stream, { type: 'tool_result' }));
    ({ frames, stream } = applyTranscriptFrame(frames, stream, assistant('After', 's')));
    const text = displayRows(frames).filter((r) => r.kind === 'assistant')[0]?.text;
    expect(text).toBe('Before\n\nAfter');
  });
});
