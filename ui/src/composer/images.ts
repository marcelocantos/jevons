// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** React lift of vanilla T76/T224 paste → POST /api/images → [image: id] on send. */

export type ClipboardLike = {
  items?: ArrayLike<{ type?: string; getAsFile?: () => File | null }>;
  files?: ArrayLike<File>;
};

export type UploadedImage = {
  id: string;
  url: string;
  thumbUrl: string;
  marker: string;
  width?: number;
  height?: number;
};

export type PendingImage = UploadedImage & { objectUrl?: string };

const PREFIX_RE = /^\s*(aside|capture|park|main|pursue|target|idea)\s*:\s*/i;
const LEADING_IMAGE_MARKERS_RE = /^\s*(?:\[image:\s*[^\]]*\]\s*)+/i;
const IMAGE_MARKER_RE = /\[image:\s*([a-f0-9]+)(?:\s+(\d+)x(\d+))?\]/gi;

export function imageThumbSrc(id: string): string {
  const n = String(id || '').trim().toLowerCase();
  return n ? '/api/images/' + n + '/thumb' : '';
}

export function imageFullSrc(id: string): string {
  const n = String(id || '').trim().toLowerCase();
  return n ? '/api/images/' + n : '';
}

export function imageMarker(id: string): string {
  const n = String(id || '').trim().toLowerCase();
  return n ? '[image: ' + n + ']' : '';
}

/** Clipboard or drop: image/* files only. Text-only paste yields []. */
export function filesFromTransfer(data: ClipboardLike | null | undefined): File[] {
  const out: File[] = [];
  const items = data?.items;
  if (items) {
    for (let i = 0; i < items.length; i++) {
      const it = items[i];
      if (it && it.type && it.type.indexOf('image/') === 0) {
        const f = typeof it.getAsFile === 'function' ? it.getAsFile() : null;
        if (f) out.push(f);
      }
    }
  }
  if (out.length) return out;
  const files = data?.files;
  if (files) {
    for (let i = 0; i < files.length; i++) {
      const f = files[i];
      if (f && f.type && f.type.indexOf('image/') === 0) out.push(f);
    }
  }
  return out;
}

export async function uploadPastedImage(
  file: File,
  fetchImpl: typeof fetch = fetch,
): Promise<UploadedImage> {
  const fd = new FormData();
  fd.append('file', file, file.name || 'paste.png');
  const res = await fetchImpl('/api/images', { method: 'POST', body: fd });
  if (!res.ok) throw new Error('upload ' + res.status);
  const meta = (await res.json()) as Record<string, unknown>;
  const id = String(meta.id || '');
  if (!id) throw new Error('upload missing id');
  return {
    id,
    url: String(meta.url || imageFullSrc(id)),
    thumbUrl: String(meta.thumb_url || imageThumbSrc(id)),
    marker: String(meta.marker || imageMarker(id)),
    width: Number(meta.width) || undefined,
    height: Number(meta.height) || undefined,
  };
}

function objectUrlFor(file: File): string | undefined {
  if (typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') return undefined;
  try {
    return URL.createObjectURL(file);
  } catch {
    return undefined;
  }
}

export function revokeObjectUrl(url: string | undefined): void {
  if (!url || typeof URL === 'undefined' || typeof URL.revokeObjectURL !== 'function') return;
  try {
    URL.revokeObjectURL(url);
  } catch {
    /* ignore */
  }
}

/** Upload each pasted image immediately (vanilla T76). Failed files are skipped. */
export async function ingestPastedFiles(
  files: File[],
  fetchImpl: typeof fetch = fetch,
): Promise<PendingImage[]> {
  const out: PendingImage[] = [];
  for (const file of files) {
    const objectUrl = objectUrlFor(file);
    try {
      const meta = await uploadPastedImage(file, fetchImpl);
      out.push({ ...meta, objectUrl });
    } catch {
      revokeObjectUrl(objectUrl);
    }
  }
  return out;
}

/** Vanilla send: markers prepended so the overseer gets durable refs (T76). */
export function composeSendText(draft: string, images: { id: string; marker?: string }[]): string {
  const t = String(draft || '').trim();
  if (!images.length) return t;
  const markers = images.map((img) => img.marker || imageMarker(img.id)).filter(Boolean).join(' ');
  if (!markers) return t;
  return t ? markers + '\n' + t : markers;
}

/**
 * T368: leading [image: id] must not hide target:/aside:/capture:/idea:.
 * Same skip as web/scripts/attention_threads.js parsePrefix.
 */
export function parsePrefixAfterImages(draft: string): {
  command: string | null;
  body: string;
  images: string;
} {
  const raw = String(draft || '');
  let rest = raw;
  let images = '';
  let m = raw.match(PREFIX_RE);
  if (!m) {
    const im = raw.match(LEADING_IMAGE_MARKERS_RE);
    if (im) {
      const after = raw.slice(im[0].length);
      const m2 = after.match(PREFIX_RE);
      if (m2) {
        rest = after;
        images = im[0].trim().replace(/\s+/g, ' ');
        m = m2;
      }
    }
  }
  if (!m) return { command: null, body: raw.replace(/^\s+/, ''), images: '' };
  return {
    command: m[1].toLowerCase(),
    body: rest.slice(m[0].length),
    images,
  };
}

export function hasImageMarker(text: string): boolean {
  return /\[image:\s*[a-f0-9]+/i.test(String(text || ''));
}

function escapeText(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function escapeAttr(s: string): string {
  return escapeText(s).replace(/"/g, '&quot;');
}

/** Owner bubble: [image: id] → T224 thumb, not blob: and not leftover marker text. */
export function renderUserTextWithImages(s: string): string {
  const re = new RegExp(IMAGE_MARKER_RE.source, 'gi');
  let html = '';
  let last = 0;
  let m: RegExpExecArray | null;
  const src = String(s ?? '');
  while ((m = re.exec(src)) !== null) {
    html += escapeText(src.slice(last, m.index));
    const id = m[1].toLowerCase();
    html +=
      '<img class="chat-img" src="' +
      escapeAttr(imageThumbSrc(id)) +
      '" data-image-id="' +
      escapeAttr(id) +
      '" alt="pasted image ' +
      escapeAttr(id) +
      '" loading="lazy">';
    last = m.index + m[0].length;
  }
  html += escapeText(src.slice(last));
  return html;
}
