// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useCallback, useEffect, useState } from 'react';

/** Full-resolution path for a pasted chat image (server ImageFullURL). */
export function imageFullSrc(id: string): string {
  const n = String(id || '').trim().toLowerCase();
  return n ? '/api/images/' + n : '';
}

type Open = { ids: string[]; index: number };

/**
 * 🎯T258 / 🎯T976: clicking a chat image opens it at full resolution in an
 * in-page viewer, with Prev / Next across the other images in the same
 * message. The vanilla cockpit had this; the React port kept its CSS but not
 * its behaviour, so a click on a thumb did nothing. Clicks are delegated from
 * the document: transcript bodies are HTML strings, not React elements.
 */
export function ImageLightbox() {
  const [open, setOpen] = useState<Open | null>(null);

  useEffect(() => {
    const onClick = (e: MouseEvent) => {
      const target = e.target;
      if (!(target instanceof HTMLImageElement) || !target.classList.contains('chat-img')) return;
      const id = target.getAttribute('data-image-id') || '';
      if (!id) return;
      const scope = target.closest('.msg') || target.parentElement;
      const ids = [...(scope?.querySelectorAll<HTMLImageElement>('img.chat-img[data-image-id]') ?? [])]
        .map((img) => img.getAttribute('data-image-id') || '')
        .filter(Boolean);
      const list = ids.includes(id) ? ids : [id];
      e.preventDefault();
      setOpen({ ids: list, index: list.indexOf(id) });
    };
    document.addEventListener('click', onClick);
    return () => document.removeEventListener('click', onClick);
  }, []);

  const step = useCallback((d: number) => {
    setOpen((o) => (o ? { ...o, index: Math.min(o.ids.length - 1, Math.max(0, o.index + d)) } : o));
  }, []);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(null);
      else if (e.key === 'ArrowLeft') step(-1);
      else if (e.key === 'ArrowRight') step(1);
      else return;
      e.preventDefault();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, step]);

  if (!open) return null;
  const many = open.ids.length > 1;
  return (
    <div
      id="img-lightbox"
      role="dialog"
      aria-modal="true"
      aria-label="Image viewer"
      onClick={(e) => {
        if (e.target === e.currentTarget) setOpen(null);
      }}
    >
      <div className="ilb-stage">
        <img
          className="ilb-img"
          id="ilb-img"
          alt="Full resolution image"
          draggable={false}
          src={imageFullSrc(open.ids[open.index])}
        />
        <div className="ilb-chrome">
          {many ? (
            <button type="button" id="ilb-prev" title="Previous image (←)" aria-label="Previous image" disabled={open.index === 0} onClick={() => step(-1)}>
              ‹ Prev
            </button>
          ) : null}
          {many ? (
            <span className="ilb-counter" id="ilb-counter" aria-live="polite">
              {open.index + 1} / {open.ids.length}
            </span>
          ) : null}
          {many ? (
            <button type="button" id="ilb-next" title="Next image (→)" aria-label="Next image" disabled={open.index === open.ids.length - 1} onClick={() => step(1)}>
              Next ›
            </button>
          ) : null}
          <button type="button" id="ilb-close" title="Close (Esc)" aria-label="Close image viewer" onClick={() => setOpen(null)}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}
