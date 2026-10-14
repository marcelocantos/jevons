// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useDrafts } from '../store/drafts';
import { usePendingImages } from '../store/pendingImages';
import { UserRequest } from './UserRequest';

beforeEach(() => {
  localStorage.clear();
  useDrafts.setState({ drafts: {} });
  usePendingImages.setState({ images: {} });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); delete (window as typeof window & { JevonsImagePicker?: unknown }).JevonsImagePicker; });

it('routes a native picked image through the existing upload and send path, only for its composer', async () => {
  const postMessage = vi.fn();
  (window as typeof window & { JevonsImagePicker?: unknown }).JevonsImagePicker = { postMessage };
  const upload = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ id: 'img123', url: '/image', thumb_url: '/thumb', marker: '[image: img123]' }) });
  vi.stubGlobal('fetch', upload);
  const onSend = vi.fn();
  const { getByRole, container } = render(<UserRequest name="jevons" onSend={onSend} />);
  fireEvent.click(getByRole('button', { name: 'Attach an image' }));
  expect(JSON.parse(postMessage.mock.calls[0][0])).toEqual({ composerId: 'jevons:normal' });
  window.dispatchEvent(new CustomEvent('jevons-picked-image', { detail: { composerId: 'other:normal', name: 'wrong.png', mime: 'image/png', base64: 'AQID' } }));
  expect(upload).not.toHaveBeenCalled();
  window.dispatchEvent(new CustomEvent('jevons-picked-image', { detail: { composerId: 'jevons:normal', name: 'photo.png', mime: 'image/png', base64: 'AQID' } }));
  await waitFor(() => expect(container.querySelector('.img-chip:not([data-upload])')).toBeTruthy());
  expect(upload).toHaveBeenCalledTimes(1);
  fireEvent.change(getByRole('textbox'), { target: { value: 'Look at this' } });
  fireEvent.click(getByRole('button', { name: /send/i }));
  expect(onSend.mock.calls[0][0]).toContain('[image: img123]');
});
