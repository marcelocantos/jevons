// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useDrafts } from '../store/drafts';
import { usePendingImages } from '../store/pendingImages';
import { composeSendText, imageFromId, splitImageMarkers } from '../composer/images';
import { enqueue, emptyState } from '../composer/sendQueue';
import { SendQueueStrip } from './SendQueueStrip';
import { UserRequest } from './UserRequest';

// 🎯T562.3: pending images and queued attachments are durable references.
const UPLOADED = { id: 'abc123', url: '/api/images/abc123', thumbUrl: '/api/images/abc123/thumb', marker: '[image: abc123]', width: 4, height: 4 };

beforeEach(() => {
  localStorage.clear();
  useDrafts.setState({ drafts: {} });
  usePendingImages.setState({ images: {} });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function paste(box: HTMLElement) {
  const file = new File([new Uint8Array([1, 2, 3])], 'p.png', { type: 'image/png' });
  fireEvent.paste(box, { clipboardData: { items: [{ type: 'image/png', getAsFile: () => file }], files: [file] } });
}

describe.each(['comfortable', 'compact'] as const)('%s pending images', (density) => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ id: 'abc123', url: UPLOADED.url, thumb_url: UPLOADED.thumbUrl, marker: UPLOADED.marker, width: 4, height: 4 }),
    }));
  });

  it('keeps a pasted image across an agent switch', async () => {
    const { getByRole, container, rerender } = render(<UserRequest name="alpha" density={density} onSend={vi.fn()} />);
    paste(getByRole('textbox'));
    await waitFor(() => expect(container.querySelectorAll('.img-chip:not([data-upload])').length).toBe(1));
    rerender(<UserRequest name="beta" density={density} onSend={vi.fn()} />);
    expect(container.querySelectorAll('.img-chip:not([data-upload])').length).toBe(0);
    rerender(<UserRequest name="alpha" density={density} onSend={vi.fn()} />);
    expect(container.querySelectorAll('.img-chip:not([data-upload])').length).toBe(1);
  });

  it('restores the chip after a reload (fresh mount over the persisted store)', async () => {
    const first = render(<UserRequest name="alpha" density={density} onSend={vi.fn()} />);
    paste(first.getByRole('textbox'));
    await waitFor(() => expect(first.container.querySelectorAll('.img-chip:not([data-upload])').length).toBe(1));
    first.unmount();
    // A reload rebuilds the store from localStorage alone.
    const persisted = JSON.parse(localStorage.getItem('jevons-pending-images') || '{}');
    expect(persisted.state.images.alpha[0].id).toBe('abc123');
    expect(persisted.state.images.alpha[0].objectUrl).toBeUndefined();
    const stored = localStorage.getItem('jevons-pending-images') as string;
    usePendingImages.setState({ images: {} });
    localStorage.setItem('jevons-pending-images', stored);
    await usePendingImages.persist.rehydrate();
    const second = render(<UserRequest name="alpha" density={density} onSend={vi.fn()} />);
    const img = second.container.querySelector('.img-chip:not([data-upload]) img') as HTMLImageElement;
    expect(img.getAttribute('src')).toBe(UPLOADED.thumbUrl);
  });

  it('sends the marker and clears the persisted images', () => {
    usePendingImages.setState({ images: { alpha: [UPLOADED] } });
    const onSend = vi.fn();
    const { getByRole } = render(<UserRequest name="alpha" density={density} onSend={onSend} />);
    fireEvent.change(getByRole('textbox'), { target: { value: 'look' } });
    fireEvent.keyDown(getByRole('textbox'), { key: 'Enter' });
    expect(onSend).toHaveBeenCalledWith('[image: abc123]\nlook');
    expect(usePendingImages.getState().images.alpha).toEqual([]);
  });
});

describe('queued items keep their attachments', () => {
  it('the queued text carries the image marker until delivery', () => {
    const text = composeSendText('later', [UPLOADED]);
    const q = enqueue(emptyState(), text);
    expect(q.items[0].text).toBe('[image: abc123]\nlater');
    expect(splitImageMarkers(q.items[0].text)).toEqual({ ids: ['abc123'], text: 'later' });
    expect(imageFromId('ABC123')).toMatchObject({ id: 'abc123', thumbUrl: UPLOADED.thumbUrl, marker: UPLOADED.marker });
  });

  it('the strip paints a thumbnail, not marker text', () => {
    const { container } = render(
      <SendQueueStrip items={[{ id: 'q1', text: '[image: abc123]\nlater' }]} onSteer={vi.fn()} onInterrupt={vi.fn()} onRemove={vi.fn()} onEdit={vi.fn()} />,
    );
    expect((container.querySelector('.sq-thumb') as HTMLImageElement).getAttribute('src')).toBe(UPLOADED.thumbUrl);
    expect(container.querySelector('.sq-text')?.textContent).toBe('later');
  });
});
