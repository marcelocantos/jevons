// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useDrafts } from '../store/drafts';
import { usePendingImages } from '../store/pendingImages';
import { UPLOAD_TIMEOUT_MS } from '../composer/images';
import { UserRequest } from './UserRequest';

// 🎯T1025: the owner pasted an image, the tab went to the background and the
// upload stalled for minutes with nothing on screen; on refocus it resolved and
// attached to whatever message was being composed by then. The contract now:
// a chip from the instant of the paste, a stalled upload fails in view, and a
// late upload lands on the message it was pasted into or is reported — never
// on a later one.

const META = { id: 'abc123', url: '/api/images/abc123', thumb_url: '/api/images/abc123/thumb', marker: '[image: abc123]', width: 4, height: 4 };

type Call = { signal?: AbortSignal; resolve: () => void };

/**
 * A fetch the test settles by hand. `honourAbort` is how a real browser fetch
 * behaves; `false` models a response that was already on its way when the
 * composition closed, which is the late-resolution the bug is about.
 */
function controlledFetch(honourAbort = true) {
  const calls: Call[] = [];
  const fn = vi.fn((_url: string, init?: RequestInit) => new Promise<Response>((resolve, reject) => {
    const signal = init?.signal ?? undefined;
    const call: Call = { signal, resolve: () => resolve({ ok: true, json: async () => META } as Response) };
    if (honourAbort && signal) signal.addEventListener('abort', () => reject(signal.reason ?? new DOMException('aborted', 'AbortError')));
    calls.push(call);
  }));
  return { fn, calls };
}

function paste(box: HTMLElement, name = 'shot.png') {
  const file = new File([new Uint8Array([1, 2, 3])], name, { type: 'image/png' });
  fireEvent.paste(box, { clipboardData: { items: [{ type: 'image/png', getAsFile: () => file }], files: [file] } });
}

const uploadingChips = (root: HTMLElement) => root.querySelectorAll('.img-chip[data-upload="uploading"]');
const failedChips = (root: HTMLElement) => root.querySelectorAll('.img-chip[data-upload="failed"]');
const attachedChips = (root: HTMLElement) => root.querySelectorAll('.img-chip:not([data-upload])');

beforeEach(() => {
  localStorage.clear();
  useDrafts.setState({ drafts: {} });
  usePendingImages.setState({ images: {} });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.useRealTimers(); });

describe.each(['comfortable', 'compact'] as const)('%s paste upload (🎯T1025)', (density) => {
  it('the chip is on screen the instant the paste fires, before any network response', () => {
    const { fn } = controlledFetch();
    vi.stubGlobal('fetch', fn);
    const { getByRole, container } = render(<UserRequest name="jevons" density={density} onSend={vi.fn()} />);
    paste(getByRole('textbox'));
    // Synchronous: no await between the paste and these reads.
    expect(uploadingChips(container)).toHaveLength(1);
    expect(attachedChips(container)).toHaveLength(0);
    const chip = uploadingChips(container)[0];
    expect(chip.getAttribute('role')).toBe('status');
    expect(chip.getAttribute('aria-label')).toContain('Uploading shot.png');
    expect(chip.textContent).toContain('Uploading');
    expect(fn).toHaveBeenCalledTimes(1);
    const send = container.querySelector('button[id$="send"]') as HTMLButtonElement;
    expect(send.textContent).toBe('Uploading…');
    expect(send.disabled).toBe(true);
  });

  it('a late upload attaches to the message it was pasted into, after more typing, and the send waits for it', async () => {
    const { fn, calls } = controlledFetch();
    vi.stubGlobal('fetch', fn);
    const onSend = vi.fn();
    const { getByRole, container, queryByRole } = render(<UserRequest name="jevons" density={density} onSend={onSend} />);
    const box = getByRole('textbox') as HTMLTextAreaElement;
    paste(box);
    // The owner keeps composing the same message while the upload is out.
    fireEvent.change(box, { target: { value: 'what is this dialog?' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onSend).not.toHaveBeenCalled();
    expect(getByRole('alert').textContent).toContain('Wait for the image upload to finish');
    expect(uploadingChips(container)).toHaveLength(1);

    // Minutes later (a backgrounded tab), the upload resolves.
    await act(async () => { calls[0].resolve(); });
    await waitFor(() => expect(attachedChips(container)).toHaveLength(1));
    expect(uploadingChips(container)).toHaveLength(0);
    expect(queryByRole('alert')).toBeNull();
    expect(usePendingImages.getState().images.jevons?.[0]?.id).toBe('abc123');
    expect(box.value).toBe('what is this dialog?');

    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onSend).toHaveBeenCalledWith('[image: abc123]\nwhat is this dialog?');
    expect(attachedChips(container)).toHaveLength(0);
  });

  it('a stalled upload fails in view instead of hanging, and the message can still go without it', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    const { fn, calls } = controlledFetch();
    vi.stubGlobal('fetch', fn);
    const onSend = vi.fn();
    const { getByRole, container } = render(<UserRequest name="jevons" density={density} onSend={onSend} />);
    const box = getByRole('textbox') as HTMLTextAreaElement;
    paste(box);
    expect(uploadingChips(container)).toHaveLength(1);
    await act(async () => { vi.advanceTimersByTime(UPLOAD_TIMEOUT_MS + 1); });
    expect(calls[0].signal?.aborted).toBe(true);
    expect(uploadingChips(container)).toHaveLength(0);
    expect(failedChips(container)).toHaveLength(1);
    expect(failedChips(container)[0].textContent).toContain('Failed');
    expect(getByRole('alert').textContent).toMatch(/Image upload failed: upload timed out after 60s/);
    expect(attachedChips(container)).toHaveLength(0);
    // The send is no longer held: the owner sees the failure and decides.
    fireEvent.change(box, { target: { value: 'never mind the picture' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onSend).toHaveBeenCalledWith('never mind the picture');
    expect(failedChips(container)).toHaveLength(0);
  });

  it('removing an uploading chip aborts its request and leaves nothing behind', () => {
    const { fn, calls } = controlledFetch();
    vi.stubGlobal('fetch', fn);
    const { getByRole, container, queryByRole } = render(<UserRequest name="jevons" density={density} onSend={vi.fn()} />);
    paste(getByRole('textbox'));
    fireEvent.click(uploadingChips(container)[0].querySelector('button')!);
    expect(calls[0].signal?.aborted).toBe(true);
    expect(container.querySelectorAll('.img-chip')).toHaveLength(0);
    expect(queryByRole('alert')).toBeNull();
    const send = container.querySelector('button[id$="send"]') as HTMLButtonElement;
    expect(send.textContent).not.toBe('Uploading…');
  });

  it('an upload that resolves after its composition closed is reported, never attached to the later message', async () => {
    // A response already on its way ignores the abort: the late-resolution shape.
    const { fn, calls } = controlledFetch(false);
    vi.stubGlobal('fetch', fn);
    const onSend = vi.fn();
    const history = [{ id: 'u1', text: 'the earlier request' }];
    useDrafts.setState({ drafts: { jevons: 'draft in progress' } });
    const { getByRole, container } = render(<UserRequest name="jevons" density={density} history={history} onSend={onSend} />);
    const box = getByRole('textbox') as HTMLTextAreaElement;
    // Paste into an earlier request being edited, then abandon that edit.
    fireEvent.keyDown(box, { key: 'ArrowUp', altKey: true });
    expect(box.value).toBe('the earlier request');
    paste(box);
    expect(uploadingChips(container)).toHaveLength(1);
    fireEvent.keyDown(box, { key: 'Escape' });
    expect(box.value).toBe('draft in progress');
    expect(container.querySelectorAll('.img-chip')).toHaveLength(0);
    expect(calls[0].signal?.aborted).toBe(true);
    // The owner composes a different message; the stale upload lands now.
    fireEvent.change(box, { target: { value: 'a different later message' } });
    await act(async () => { calls[0].resolve(); });
    await waitFor(() => expect(getByRole('alert').textContent).toContain('shot.png finished uploading after the message it was pasted into was closed'));
    expect(attachedChips(container)).toHaveLength(0);
    expect(usePendingImages.getState().images.jevons ?? []).toEqual([]);
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onSend).toHaveBeenCalledWith('a different later message');
  });

  it('an upload still running when the composer is unmounted is aborted', () => {
    const { fn, calls } = controlledFetch();
    vi.stubGlobal('fetch', fn);
    const { getByRole, unmount } = render(<UserRequest name="jevons" density={density} onSend={vi.fn()} />);
    paste(getByRole('textbox'));
    unmount();
    expect(calls[0].signal?.aborted).toBe(true);
  });
});
