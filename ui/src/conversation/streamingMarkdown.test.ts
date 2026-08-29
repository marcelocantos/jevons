// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import * as smd from 'streaming-markdown';

describe('streaming-markdown last glyph (🎯T555.5)', () => {
  it('write without parser_end drops the last character — not an accepted paint', () => {
    let painted = '';
    const p = smd.parser({
      data: {},
      add_token() {},
      end_token() {},
      add_text(_data, text) {
        painted += text;
      },
      set_attr() {},
    });
    smd.parser_write(p, 'overseer is back');
    expect(painted).toBe('overseer is bac');
    expect(p.pending).toBe('k');
    smd.parser_end(p);
    expect(painted).toBe('overseer is back');
  });
});
