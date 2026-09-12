// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import {
  applyTranscriptFrame,
  coalesceAssistantText,
  emptyStream,
  joinAssistantTexts,
  reduceTranscriptBodies,
} from './stream';
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

  it('🎯T645 coalesceAssistantText: fence opener at segment edge gets a newline', () => {
    const out = coalesceAssistantText('finish-report.', '```jevons\nk: v\n```');
    expect(out).toBe('finish-report.\n\n```jevons\nk: v\n```');
    expect(out).not.toContain('.```');
  });

  it('🎯T645 coalesceAssistantText: sentence punct + capital gets a space', () => {
    expect(coalesceAssistantText('idle.', 'The reminted')).toBe('idle. The reminted');
    expect(coalesceAssistantText('went idle.', 'The reminted T643')).toBe(
      'went idle. The reminted T643',
    );
  });

  it('🎯T147 coalesceAssistantText: Intro. + ```cpp inserts a blank line', () => {
    expect(coalesceAssistantText('Intro.', '```cpp\ncode\n```')).toBe('Intro.\n\n```cpp\ncode\n```');
  });

  it('🎯T147 joinAssistantTexts: multi-part fence edge', () => {
    const out = joinAssistantTexts([
      'Checking conventions briefly, then a small clean snippet.',
      '```cpp\nint main() {}\n```',
    ]);
    expect(out).toBe(
      'Checking conventions briefly, then a small clean snippet.\n\n```cpp\nint main() {}\n```',
    );
  });

  it('🎯T147 coalesceAssistantText: intra-token streams stay bare-concat', () => {
    expect(coalesceAssistantText('Hello', '.')).toBe('Hello.');
    expect(coalesceAssistantText('Hel', 'lo')).toBe('Hello');
    expect(coalesceAssistantText('Yes.', ' I have')).toBe('Yes. I have');
  });

  it('same-stream consecutive chunks use join-time coalesce, not glue', () => {
    const { frames } = reduceTranscriptBodies([
      assistant('finish-report.', 's'),
      assistant('```jevons\nk: v\n```', 's'),
      assistant('idle.', 't'),
      assistant('The reminted', 't'),
    ]);
    const texts = displayRows(frames).map((r) => r.text);
    expect(texts).toContain('finish-report.\n\n```jevons\nk: v\n```');
    expect(texts).toContain('idle. The reminted');
    expect(texts.join('\n')).not.toContain('finish-report.```');
    expect(texts.join('\n')).not.toContain('idle.The reminted');
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
