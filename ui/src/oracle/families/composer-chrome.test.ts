// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import '../../composer/ensureLocalStorage';
import { createElement } from 'react';
import { fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, expect, vi } from 'vitest';
import { composeSendText, filesFromTransfer, parsePrefixAfterImages } from '../../composer/images';
import { deserialize, load, save, serialize } from '../../composer/sendQueue';
import { tidyDictationInsert } from '../../composer/wispr';
import { UserRequest } from '../../components/UserRequest';
import { useDrafts } from '../../store/drafts';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

afterEach(() => {
  useDrafts.setState({ drafts: {} });
});

function pasteImageOn(el: Element, file: File): void {
  fireEvent.paste(el, {
    clipboardData: {
      items: [{ type: file.type, getAsFile: () => file }],
      files: [file],
    },
  });
}

describeOracle(family('composer-chrome'), () => {
  itOracle('T76', 'pasted images reach the agent turn', async () => {
    const png = new File([new Uint8Array([137, 80, 78, 71])], 'paste.png', { type: 'image/png' });
    expect(filesFromTransfer({ items: [{ type: 'image/png', getAsFile: () => png }] }).length).toBe(1);
    expect(composeSendText('see this', [{ id: 'deadbeef', marker: '[image: deadbeef]' }])).toBe(
      '[image: deadbeef]\nsee this',
    );

    const sent: string[] = [];
    const fetchImpl = vi.fn(async (url: string) => {
      expect(String(url)).toContain('/api/images');
      return {
        ok: true,
        json: async () => ({ id: 'deadbeef', marker: '[image: deadbeef]' }),
      };
    });
    const prev = globalThis.fetch;
    globalThis.fetch = fetchImpl as unknown as typeof fetch;
    try {
      const { container } = render(createElement(UserRequest, { name: 'jevons', onSend: (t) => sent.push(t) }));
      const box = container.querySelector('#input');
      expect(box).toBeTruthy();
      pasteImageOn(box!, png);
      await waitFor(() => {
        expect(container.querySelector('#composer-images img')).toBeTruthy();
      });
      fireEvent.click(container.querySelector('#send')!);
      expect(sent[0]).toMatch(/\[image:\s*deadbeef\]/);
    } finally {
      globalThis.fetch = prev;
    }
  });

  itOracle('T80', 'Wispr/dictation inserts are lightly tidied', () => {
    expect(tidyDictationInsert('  hello there  ')).toBe('Hello there.');
    expect(tidyDictationInsert('Already done.')).toBe('Already done.');
  });

  itOracle('T368', 'prefix commands still open when the message starts with image markers', () => {
    const p = parsePrefixAfterImages('[image: abcdef] target: file this');
    expect(p.command).toBe('target');
    expect(p.body).toMatch(/file this/);
  });

  itOracle('T183', 'composer draft is visible after reload without an edit keystroke', () => {
    useDrafts.getState().setDraft('jevons', 'restored draft');
    const { container } = render(createElement(UserRequest, { name: 'jevons', onSend: () => {} }));
    const box = container.querySelector('#input') as HTMLTextAreaElement;
    expect(box.value).toBe('restored draft');
  });

  itOracle('T154', 'send queue survives reload and reconnect', () => {
    const mem = new Map<string, string>();
    const storage = {
      getItem: (k: string) => mem.get(k) ?? null,
      setItem: (k: string, v: string) => {
        mem.set(k, v);
      },
    };
    save(storage, { items: [{ id: 'q1', text: 'follow up' }], nextId: 2 });
    const again = load(storage);
    expect(again.items.map((it) => it.text)).toEqual(['follow up']);
    expect(deserialize(serialize(again)).items[0]?.text).toBe('follow up');
  });

  itOracle.skip('T70', 'composer growth keeps the latest assistant reply readable', 'named residual: pixel-identical chrome');
  itOracle.skip('T70.1', 'composer growth does not cover the latest assistant response', 'named residual: pixel-identical chrome');
  itOracle.skip('T123', 'empty composer height matches the send button', 'journey is the arbiter (J24)');
  itOracle.skip('T478', 'after send, the empty composer stays one control tall', 'journey is the arbiter (J24)');
});
