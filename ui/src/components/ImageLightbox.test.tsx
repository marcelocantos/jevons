// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { ImageLightbox } from './ImageLightbox';

// 🎯T976: clicking a chat thumb opens its full-resolution image; the React
// port had kept the lightbox CSS but dropped the behaviour.
describe('ImageLightbox', () => {
  afterEach(cleanup);

  function transcript() {
    const msg = document.createElement('div');
    msg.className = 'msg user';
    msg.innerHTML =
      '<div class="msg-body"><p>look</p>' +
      '<img class="chat-img" data-image-id="aaaa000000000001" src="/api/images/aaaa000000000001/thumb">' +
      '<img class="chat-img" data-image-id="bbbb000000000002" src="/api/images/bbbb000000000002/thumb"></div>';
    document.body.appendChild(msg);
    return msg;
  }

  it('opens the clicked image at full resolution and steps within its message', () => {
    const msg = transcript();
    const { container } = render(<ImageLightbox />);
    act(() => {
      (msg.querySelectorAll('img.chat-img')[1] as HTMLElement).click();
    });
    const img = () => document.querySelector<HTMLImageElement>('#ilb-img');
    expect(img()?.getAttribute('src')).toBe('/api/images/bbbb000000000002');
    expect(document.querySelector('#ilb-counter')?.textContent).toBe('2 / 2');
    fireEvent.keyDown(document, { key: 'ArrowLeft' });
    expect(img()?.getAttribute('src')).toBe('/api/images/aaaa000000000001');
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(document.querySelector('#img-lightbox')).toBeNull();
    expect(container).toBeTruthy();
    msg.remove();
  });
});
