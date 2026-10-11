// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import '../composer/ensureLocalStorage';
import { createEvent, fireEvent, render, cleanup } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { isTouchscreenDevice, classifyEnterAction } from '../keys/composerEnter';
import { useDrafts } from '../store/drafts';
import { UserRequest } from './UserRequest';

afterEach(() => {
  cleanup();
  delete (window as Window & { JevonsImagePicker?: unknown }).JevonsImagePicker;
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  useDrafts.setState({ drafts: {} });
});

function key(box: HTMLTextAreaElement, name: string, mods: Record<string, boolean> = {}) {
  const e = createEvent.keyDown(box, { key: name, cancelable: true, ...mods });
  fireEvent(box, e);
  return e;
}

describe.each(['comfortable', 'compact'] as const)('T1057 touchscreen hardware keyboard, %s composer', (density) => {
  it('leaves plain arrows and modifier navigation native; only Alt+Up/Down recalls', () => {
    const onSend = vi.fn();
    const onRecall = vi.fn();
    const draft = 'first line\nsecond line';
    useDrafts.getState().setDraft('jevons', draft);
    const { getByRole } = render(<UserRequest name="jevons" density={density} onSend={onSend} onRecall={onRecall} history={[{ id: 'old', text: 'earlier' }]} />);
    const box = getByRole('textbox') as HTMLTextAreaElement;
    box.focus();
    box.setSelectionRange(8, 13);
    for (const name of ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown']) {
      for (const mods of [{}, { shiftKey: true }, { metaKey: true }]) {
        const e = key(box, name, mods);
        expect(e.defaultPrevented, `${name} ${JSON.stringify(mods)}`).toBe(false);
        // jsdom cannot execute the browser's default caret movement. Ensure
        // React did not change the selection or recall/send the draft.
        expect([box.selectionStart, box.selectionEnd]).toEqual([8, 13]);
      }
    }
    expect(onRecall).not.toHaveBeenCalled();
    expect(onSend).not.toHaveBeenCalled();
    expect(key(box, 'ArrowUp', { altKey: true }).defaultPrevented).toBe(true);
    expect(onRecall).toHaveBeenCalledWith({ id: 'old', text: 'earlier' });
  });

  it('leaves unmodified hardware Return to the textarea even when a trackpad reports fine pointer', () => {
    vi.stubGlobal('navigator', Object.assign(Object.create(navigator), { maxTouchPoints: 5 }));
    vi.stubGlobal('matchMedia', () => ({ matches: false })); // trackpad: fine primary pointer
    expect(isTouchscreenDevice()).toBe(true);
    expect(classifyEnterAction('Enter', {}, { touchscreen: true, composerEmpty: false })).toBe('newline');
    const onSend = vi.fn();
    const { getByRole } = render(<UserRequest name="jevons" density={density} onSend={onSend} />);
    const box = getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.change(box, { target: { value: 'first\nsecond' } });
    box.focus();
    box.setSelectionRange(5, 5);
    const plain = key(box, 'Enter');
    expect(plain.defaultPrevented).toBe(false);
    expect(onSend).not.toHaveBeenCalled();
    expect(box.title).toContain('Enter newline');
    expect(key(box, 'Enter', { shiftKey: true }).defaultPrevented).toBe(false);
    expect(key(box, 'Enter', { metaKey: true }).defaultPrevented).toBe(true);
    expect(onSend).toHaveBeenCalledWith('first\nsecond', { mode: 'steer' });
  });
});

it('T1057: ordinary fine-pointer desktop without touchscreen retains Enter-to-send', () => {
  vi.stubGlobal('matchMedia', () => ({ matches: false }));
  vi.stubGlobal('navigator', Object.assign(Object.create(navigator), { maxTouchPoints: 0 }));
  expect(isTouchscreenDevice()).toBe(false);
  const onSend = vi.fn();
  const { getByRole } = render(<UserRequest name="jevons" onSend={onSend} />);
  const box = getByRole('textbox') as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: 'desktop draft' } });
  expect(key(box, 'Enter').defaultPrevented).toBe(true);
  expect(onSend).toHaveBeenCalledWith('desktop draft');
  expect(box.title).toContain('Enter send');
  fireEvent.change(box, { target: { value: 'second draft' } });
  expect(key(box, 'Enter', { shiftKey: true }).defaultPrevented).toBe(false);
  expect(onSend).toHaveBeenCalledTimes(1);
});

it('T1057: touchscreen without hardware keyboard uses the same plain Return policy', () => {
  vi.stubGlobal('navigator', Object.assign(Object.create(navigator), { maxTouchPoints: 5 }));
  const onSend = vi.fn();
  const { getByRole } = render(<UserRequest name="jevons" onSend={onSend} />);
  const box = getByRole('textbox') as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: 'virtual keyboard draft' } });
  expect(key(box, 'Enter').defaultPrevented).toBe(false);
  expect(onSend).not.toHaveBeenCalled();
});

it('T1057: a coarse pointer or Flutter-shaped channel alone never overrides no-touch desktop Return', () => {
  vi.stubGlobal('navigator', Object.assign(Object.create(navigator), { maxTouchPoints: 0 }));
  vi.stubGlobal('matchMedia', () => ({ matches: true }));
  (window as Window & { JevonsImagePicker?: unknown }).JevonsImagePicker = { postMessage: vi.fn() };
  const onSend = vi.fn();
  const { getByRole } = render(<UserRequest name="jevons" onSend={onSend} />);
  const box = getByRole('textbox') as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: 'not a touchscreen' } });
  expect(key(box, 'Enter').defaultPrevented).toBe(true);
  expect(onSend).toHaveBeenCalledWith('not a touchscreen');
  delete (window as Window & { JevonsImagePicker?: unknown }).JevonsImagePicker;
});
