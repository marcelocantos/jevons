// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import '../../composer/ensureLocalStorage';
import { createElement } from 'react';
import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { render } from '@testing-library/react';
import { composeSendText, filesFromTransfer, parsePrefixAfterImages } from '../../composer/images';
import {
  COMPOSER_MAX_HEIGHT,
  emptyComposerUsedHeight,
  growthWithoutCoverHolds,
  lastMessageFullyVisible,
  scrollTopAfterComposerGrow,
} from '../../composer/layout';
import { deserialize, enqueue, emptyState, load, save, serialize, shiftNext, STORAGE_KEY } from '../../composer/sendQueue';
import { EMPTY_SEED, needsSeedOnlyClass, tidyDictationInsert } from '../../composer/wispr';
import { useDrafts } from '../../store/drafts';
import { UserRequest } from '../../components/UserRequest';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const uiSrc = join(dirname(fileURLToPath(import.meta.url)), '../..');
const css = readFileSync(join(uiSrc, 'cockpit.css'), 'utf8');
const draftsSrc = readFileSync(join(uiSrc, 'store/drafts.ts'), 'utf8');

function memStorage(seed?: Record<string, string>) {
  const map: Record<string, string> = { ...(seed || {}) };
  return {
    getItem: (k: string) => (Object.prototype.hasOwnProperty.call(map, k) ? map[k] : null),
    setItem: (k: string, v: string) => {
      map[k] = String(v);
    },
    _map: map,
  };
}

describeOracle(family('composer-chrome'), () => {
  itOracle('T70', 'composer growth keeps the latest assistant reply readable', () => {
    const chat = css.match(/#chat-pane\s*\{[^}]+\}/);
    const msg = css.match(/#messages\s*\{[^}]+\}/);
    expect(chat?.[0]).toMatch(/flex-direction:\s*column/);
    expect(msg?.[0]).toMatch(/flex:\s*1/);
    expect(msg?.[0]).toMatch(/min-height:\s*0/);
    expect(growthWithoutCoverHolds(280, 400, 80)).toBe(true);
  });

  itOracle('T70.1', 'composer growth does not cover the latest assistant response', () => {
    expect(growthWithoutCoverHolds(280, 400, 80)).toBe(true);
    expect(growthWithoutCoverHolds(320, 500, 120)).toBe(true);
    expect(growthWithoutCoverHolds(200, 400, 0)).toBe(true);
    const lastHeight = 280;
    const clientHeight = 400;
    const growPx = 80;
    const filler = Math.max(clientHeight, 200);
    const scrollHeight = filler + lastHeight;
    const lastTop = filler;
    const scrollTop = Math.max(0, scrollHeight - clientHeight);
    const clientAfter = clientHeight - growPx;
    expect(lastMessageFullyVisible(lastTop, lastHeight, scrollTop, clientAfter)).toBe(false);
    const fixed = scrollTopAfterComposerGrow(scrollTop, clientAfter, scrollHeight, growPx);
    expect(lastMessageFullyVisible(lastTop, lastHeight, fixed, clientAfter)).toBe(true);
    const inputRule = css.match(/#input\s*\{[^}]+\}/);
    expect(inputRule?.[0]).toContain('max-height: ' + COMPOSER_MAX_HEIGHT);
  });

  itOracle('T76', 'pasted images reach the agent turn', () => {
    const png = new File([new Uint8Array([1, 2, 3])], 'paste.png', { type: 'image/png' });
    const files = filesFromTransfer({
      items: [{ type: 'image/png', getAsFile: () => png }],
    });
    expect(files).toHaveLength(1);
    expect(files[0].type).toBe('image/png');
    expect(filesFromTransfer({ items: [{ type: 'text/plain', getAsFile: () => null }] })).toEqual([]);
    expect(composeSendText('hello', [{ id: 'abc', marker: '[image: abc]' }])).toBe('[image: abc]\nhello');
    expect(composeSendText('', [{ id: 'deadbeef' }])).toBe('[image: deadbeef]');
  });

  itOracle('T80', 'Wispr/dictation inserts are lightly tidied', () => {
    expect(tidyDictationInsert('  hello world  ')).toBe('Hello world.');
    expect(tidyDictationInsert('Already fine.')).toBe('Already fine.');
    expect(tidyDictationInsert('What now?')).toBe('What now?');
    expect(tidyDictationInsert('mid edit leave')).toBe('Mid edit leave.');
    expect(tidyDictationInsert('   ')).toBe('');
  });

  itOracle('T123', 'empty composer height matches the send button', () => {
    expect(css).toMatch(/#input-bar\s*\{[^}]*--control-h:\s*calc\(1\.45\s*\*\s*14px\s*\+\s*24px\)/);
    const inputRule = css.match(/#input\s*\{[^}]+\}/);
    const sendRule = css.match(/#send\s*\{[^}]+\}/);
    expect(inputRule?.[0]).toMatch(/min-height:\s*var\(--control-h\)/);
    expect(sendRule?.[0]).toMatch(/min-height:\s*var\(--control-h\)/);
    expect(inputRule?.[0]).not.toMatch(/2\.9\s*\*\s*1\.45/);
  });

  itOracle('T154', 'send queue survives reload and reconnect', () => {
    const storage = memStorage();
    let s = enqueue(enqueue(enqueue(emptyState(), 'alpha'), 'beta'), 'gamma');
    save(storage, s);
    const reloaded = load(storage);
    expect(reloaded.items.map((it) => it.text)).toEqual(['alpha', 'beta', 'gamma']);
    expect(storage.getItem(STORAGE_KEY)).toBeTruthy();
    const raw = serialize(s);
    const restored = deserialize(raw);
    expect(restored.items[0].id).toBe(s.items[0].id);
    let drain = shiftNext(reloaded);
    expect(drain.item?.text).toBe('alpha');
    drain = shiftNext(drain.state);
    expect(drain.item?.text).toBe('beta');
    expect(deserialize('not-json{').items).toEqual([]);
    expect(load(null).items).toEqual([]);
  });

  itOracle('T183', 'composer draft is visible after reload without an edit keystroke', () => {
    const restored = 'Hello after reload — still here';
    expect(needsSeedOnlyClass(restored)).toBe(false);
    expect(needsSeedOnlyClass(EMPTY_SEED)).toBe(true);
    expect(needsSeedOnlyClass(EMPTY_SEED + restored)).toBe(false);
    expect(needsSeedOnlyClass('')).toBe(false);
    expect(css).not.toMatch(/\.composer-seed-only\s*\{[^}]*color:\s*transparent/);
    expect(draftsSrc).toContain("name: 'jevons-drafts'");
    useDrafts.getState().setDraft('jevons', restored);
    expect(localStorage.getItem('jevons-drafts')).toContain(restored);
    const { container, unmount } = render(createElement(UserRequest, { name: 'jevons', onSend: () => {} }));
    const ta = container.querySelector('#input') as HTMLTextAreaElement;
    expect(ta.value).toBe(restored);
    expect(ta.classList.contains('composer-seed-only')).toBe(false);
    unmount();
    useDrafts.setState({ drafts: {} });
    localStorage.removeItem('jevons-drafts');
  });

  itOracle('T368', 'prefix commands still open when the message starts with image markers', () => {
    const filed = parsePrefixAfterImages('[image: d592b0380b1a9e9b]\ntarget: virtualise history');
    expect(filed.command).toBe('target');
    expect(filed.body).toBe('virtualise history');
    expect(filed.images).toBe('[image: d592b0380b1a9e9b]');
    const multi = parsePrefixAfterImages('[image: aaa111] [image: bbb222]  ASIDE : two shots');
    expect(multi.command).toBe('aside');
    expect(multi.body).toBe('two shots');
    expect(multi.images).toBe('[image: aaa111] [image: bbb222]');
    expect(parsePrefixAfterImages('capture: side thought').command).toBe('capture');
    const plain = parsePrefixAfterImages('[image: aaa111]\nplain body');
    expect(plain.command).toBeNull();
    expect(plain.body).toBe('[image: aaa111]\nplain body');
    expect(parsePrefixAfterImages('[image: abcdef] capture: spark').command).toBe('capture');
    expect(parsePrefixAfterImages('[image: abcdef] idea: note').command).toBe('idea');
  });

  itOracle('T478', 'after send, the empty composer stays one control tall', () => {
    expect(emptyComposerUsedHeight(96, 44, true)).toBe(44);
    expect(emptyComposerUsedHeight(72, 44, false)).toBe(72);
    expect(emptyComposerUsedHeight(120, 44, true)).toBe(44);
  });
});
