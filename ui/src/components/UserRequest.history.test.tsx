// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { act, cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useDrafts } from '../store/drafts';
import { UserRequest } from './UserRequest';

// T88/T562: mounted composer contract, not provider or complete-widget evidence.
const history = [{ id: 'u1', text: 'same request' }, { id: 'u2', text: 'same request' }];
beforeEach(() => useDrafts.setState({ drafts: { jevons: 'unfinished draft' } }));
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe.each(['comfortable', 'compact'] as const)('%s history recall', (density) => {
  it('navigates by turn identity and restores the draft when moving past the newest request', () => {
    const onRecall = vi.fn();
    const onSend = vi.fn();
    const { getByRole } = render(<UserRequest name="jevons" density={density} history={history} onRecall={onRecall} onSend={onSend} />);
    const box = getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.keyDown(box, { key: 'ArrowUp', altKey: true });
    expect(box.value).toBe('same request');
    expect(onRecall).toHaveBeenLastCalledWith(history[1]);
    fireEvent.keyDown(box, { key: 'ArrowUp', altKey: true });
    expect(onRecall).toHaveBeenLastCalledWith(history[0]);
    fireEvent.keyDown(box, { key: 'ArrowDown', altKey: true });
    expect(onRecall).toHaveBeenLastCalledWith(history[1]);
    fireEvent.keyDown(box, { key: 'ArrowDown', altKey: true });
    expect(box.value).toBe('unfinished draft');
    expect(onRecall).toHaveBeenLastCalledWith(null);
    expect(onSend).not.toHaveBeenCalled();
  });

  it('keeps plain arrows for caret movement and Escape cancels recall', () => {
    const { getByRole } = render(<UserRequest name="jevons" density={density} history={history} onSend={vi.fn()} />);
    const box = getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.keyDown(box, { key: 'ArrowUp' });
    expect(box.value).toBe('unfinished draft');
    fireEvent.keyDown(box, { key: 'ArrowUp', altKey: true });
    fireEvent.change(box, { target: { value: 'edited earlier request' } });
    fireEvent.keyDown(box, { key: 'Escape' });
    expect(box.value).toBe('unfinished draft');
  });

  it('primary submit requests rewind and never silently appends after failure', async () => {
    const onSend = vi.fn();
    const onRewind = vi.fn().mockRejectedValue(new Error('The conversation changed; select the turn again.'));
    const { getByRole } = render(<UserRequest name="jevons" density={density} history={history} onSend={onSend} onRewind={onRewind} />);
    const box = getByRole('textbox') as HTMLTextAreaElement;
    fireEvent.keyDown(box, { key: 'ArrowUp', altKey: true });
    fireEvent.change(box, { target: { value: 'corrected request' } });
    fireEvent.keyDown(box, { key: 'Enter' });
    await waitFor(() => expect(getByRole('alert').textContent).toContain('conversation changed'));
    expect(onRewind).toHaveBeenCalledWith(history[1], 'corrected request');
    expect(onSend).not.toHaveBeenCalled();
    expect(box.value).toBe('corrected request');
    expect(getByRole('button', { name: 'Rewind and resend' })).toBeTruthy();
  });

  it('offers an explicit append escape without rewinding', () => {
    const onSend = vi.fn();
    const onRewind = vi.fn();
    const { getByRole } = render(<UserRequest name="jevons" density={density} history={history} onSend={onSend} onRewind={onRewind} />);
    fireEvent.keyDown(getByRole('textbox'), { key: 'ArrowUp', altKey: true });
    fireEvent.click(getByRole('button', { name: 'Send as new message' }));
    expect(onSend).toHaveBeenCalledWith('same request');
    expect(onRewind).not.toHaveBeenCalled();
    expect(getByRole('button', { name: 'Send' })).toBeTruthy();
  });

  it('does not append when no rewind implementation is available', () => {
    const onSend = vi.fn();
    const { getByRole } = render(<UserRequest name="jevons" density={density} history={history} onSend={onSend} />);
    fireEvent.keyDown(getByRole('textbox'), { key: 'ArrowUp', altKey: true });
    fireEvent.keyDown(getByRole('textbox'), { key: 'Enter' });
    expect(getByRole('alert').textContent).toContain('Rewind is unavailable');
    expect(onSend).not.toHaveBeenCalled();
  });

  it('does not handle a shortcut while the input method is composing', () => {
    const onRecall = vi.fn();
    const { getByRole } = render(<UserRequest name="jevons" density={density} history={history} onRecall={onRecall} onSend={vi.fn()} />);
    fireEvent.keyDown(getByRole('textbox'), { key: 'ArrowUp', altKey: true, isComposing: true });
    expect((getByRole('textbox') as HTMLTextAreaElement).value).toBe('unfinished draft');
    expect(onRecall).not.toHaveBeenCalled();
  });

  it('keeps the unsent draft attachments out of a recalled request and restores them on cancel', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ id: 'abcdef', url: '/image', thumb_url: '/thumb' }) }));
    const onRewind = vi.fn().mockRejectedValue(new Error('Not delivered'));
    const { getByRole, queryByRole } = render(<UserRequest name="jevons" density={density} history={history} onSend={vi.fn()} onRewind={onRewind} />);
    fireEvent.paste(getByRole('textbox'), { clipboardData: { files: [new File(['image'], 'draft.png', { type: 'image/png' })] } });
    await waitFor(() => expect(getByRole('img', { name: 'attachment abcdef' })).toBeTruthy());
    fireEvent.keyDown(getByRole('textbox'), { key: 'ArrowUp', altKey: true });
    expect(queryByRole('img', { name: 'attachment abcdef' })).toBeNull();
    fireEvent.keyDown(getByRole('textbox'), { key: 'Enter' });
    await waitFor(() => expect(getByRole('alert').textContent).toBe('Not delivered'));
    expect(onRewind).toHaveBeenCalledWith(history[1], 'same request');
    fireEvent.keyDown(getByRole('textbox'), { key: 'Escape' });
    expect((getByRole('textbox') as HTMLTextAreaElement).value).toBe('unfinished draft');
    expect(getByRole('img', { name: 'attachment abcdef' })).toBeTruthy();
  });

  it.each(['resolve', 'reject'] as const)('ignores a late rewind %s after switching agents', async (outcome) => {
    let resolve!: () => void;
    let reject!: (error: Error) => void;
    const onRewind = vi.fn(() => new Promise<void>((ok, fail) => { resolve = ok; reject = fail; }));
    const onSend = vi.fn();
    const { getByRole, queryByRole, rerender } = render(<UserRequest name="jevons" density={density} history={history} onSend={onSend} onRewind={onRewind} />);
    fireEvent.keyDown(getByRole('textbox'), { key: 'ArrowUp', altKey: true });
    fireEvent.change(getByRole('textbox'), { target: { value: 'corrected old request' } });
    fireEvent.keyDown(getByRole('textbox'), { key: 'Enter' });
    expect((getByRole('textbox') as HTMLTextAreaElement).disabled).toBe(true);
    rerender(<UserRequest name="worker" density={density} history={history} onSend={onSend} onRewind={onRewind} />);
    fireEvent.change(getByRole('textbox'), { target: { value: 'new agent draft' } });
    await act(async () => {
      if (outcome === 'resolve') resolve();
      else reject(new Error('Old agent rewind failed'));
    });
    expect((getByRole('textbox') as HTMLTextAreaElement).value).toBe('new agent draft');
    expect((getByRole('textbox') as HTMLTextAreaElement).disabled).toBe(false);
    expect(queryByRole('alert')).toBeNull();
    expect(queryByRole('group', { name: 'Editing an earlier request' })).toBeNull();
    expect(useDrafts.getState().drafts.jevons).toBe('unfinished draft');
    rerender(<UserRequest name="jevons" density={density} history={history} onSend={onSend} onRewind={onRewind} />);
    expect((getByRole('textbox') as HTMLTextAreaElement).value).toBe('unfinished draft');
    expect(queryByRole('group', { name: 'Editing an earlier request' })).toBeNull();
    expect(onSend).not.toHaveBeenCalled();
  });

  it('discards an image upload that finishes after its recalled edit was canceled', async () => {
    let resolve!: (value: unknown) => void;
    vi.stubGlobal('fetch', vi.fn(() => new Promise((ok) => { resolve = ok; })));
    const { getByRole, queryByRole } = render(<UserRequest name="jevons" density={density} history={history} onSend={vi.fn()} />);
    fireEvent.keyDown(getByRole('textbox'), { key: 'ArrowUp', altKey: true });
    fireEvent.paste(getByRole('textbox'), { clipboardData: { files: [new File(['image'], 'edit.png', { type: 'image/png' })] } });
    fireEvent.keyDown(getByRole('textbox'), { key: 'Escape' });
    await act(async () => { resolve({ ok: true, json: async () => ({ id: 'abcdef' }) }); });
    expect((getByRole('textbox') as HTMLTextAreaElement).value).toBe('unfinished draft');
    expect(queryByRole('img')).toBeNull();
  });

  it('does not carry an attachment from one recalled request into another', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({ id: 'abcdef' }) }));
    const onSend = vi.fn();
    const { getByRole, queryByRole } = render(<UserRequest name="jevons" density={density} history={history} onSend={onSend} />);
    fireEvent.keyDown(getByRole('textbox'), { key: 'ArrowUp', altKey: true });
    fireEvent.paste(getByRole('textbox'), { clipboardData: { files: [new File(['image'], 'edit.png', { type: 'image/png' })] } });
    await waitFor(() => expect(getByRole('img')).toBeTruthy());
    fireEvent.keyDown(getByRole('textbox'), { key: 'ArrowUp', altKey: true });
    expect(queryByRole('img')).toBeNull();
    fireEvent.click(getByRole('button', { name: 'Send as new message' }));
    expect(onSend).toHaveBeenCalledWith('same request');
  });

  it('rejects a selected ID whose current text has changed', () => {
    const onRewind = vi.fn();
    const onSend = vi.fn();
    const { getByRole, rerender } = render(<UserRequest name="jevons" density={density} history={history} onSend={onSend} onRewind={onRewind} />);
    fireEvent.keyDown(getByRole('textbox'), { key: 'ArrowUp', altKey: true });
    rerender(<UserRequest name="jevons" density={density} history={[{ id: 'u2', text: 'different request' }]} onSend={onSend} onRewind={onRewind} />);
    fireEvent.keyDown(getByRole('textbox'), { key: 'Enter' });
    expect(getByRole('alert').textContent).toContain('request changed');
    expect(onRewind).not.toHaveBeenCalled();
    expect(onSend).not.toHaveBeenCalled();
    expect((getByRole('textbox') as HTMLTextAreaElement).value).toBe('same request');
  });

  it('restores the ordinary draft after a successful rewind callback', async () => {
    const onRewind = vi.fn().mockResolvedValue(undefined);
    const { getByRole } = render(<UserRequest name="jevons" density={density} history={history} onSend={vi.fn()} onRewind={onRewind} />);
    fireEvent.keyDown(getByRole('textbox'), { key: 'ArrowUp', altKey: true });
    fireEvent.keyDown(getByRole('textbox'), { key: 'Enter' });
    await waitFor(() => expect(getByRole('button', { name: 'Send' })).toBeTruthy());
    expect((getByRole('textbox') as HTMLTextAreaElement).value).toBe('unfinished draft');
  });
});
