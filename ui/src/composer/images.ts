// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** React lift of vanilla T76/T224 paste → POST /api/images → [image: id] on send. */

export type ClipboardLike = {
  items?: ArrayLike<{ type?: string; kind?: string; getAsFile?: () => File | null }>;
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

/** True if a File looks like an image (MIME and/or filename). */
export function isImageFile(f: File | null | undefined): boolean {
  if (!f) return false;
  const t = String(f.type || '').toLowerCase();
  if (t.indexOf('image/') === 0) return true;
  // macOS / some browsers leave type empty or application/octet-stream on paste.
  if (t && t !== 'application/octet-stream') return false;
  return /\.(png|jpe?g|gif|webp|bmp|tiff?|heic|heif)$/i.test(String(f.name || ''));
}

/** Clipboard item looks like an image payload (including empty type + file kind). */
function isImageClipboardItem(it: { type?: string; kind?: string } | null | undefined): boolean {
  if (!it) return false;
  const t = String(it.type || '').toLowerCase();
  if (t.indexOf('image/') === 0) return true;
  // Some WebKit pastes report kind=file with empty type for screenshots.
  if (it.kind === 'file' && (!t || t === 'application/octet-stream')) return true;
  return false;
}

/** Clipboard or drop: image files only. Text-only paste yields []. */
export function filesFromTransfer(data: ClipboardLike | null | undefined): File[] {
  const out: File[] = [];
  const seen = new Set<File>();
  const push = (f: File | null | undefined) => {
    if (!f || !isImageFile(f) || seen.has(f)) return;
    seen.add(f);
    out.push(f);
  };
  const items = data?.items;
  if (items) {
    for (let i = 0; i < items.length; i++) {
      const it = items[i] as { type?: string; kind?: string; getAsFile?: () => File | null };
      if (!isImageClipboardItem(it)) continue;
      const f = typeof it.getAsFile === 'function' ? it.getAsFile() : null;
      push(f);
    }
  }
  const files = data?.files;
  if (files) {
    for (let i = 0; i < files.length; i++) push(files[i]);
  }
  return out;
}

/**
 * 🎯T1025: a stalled upload fails visibly instead of hanging. A backgrounded
 * tab throttles network work, so a bare fetch could sit for minutes and then
 * resolve into whatever message the owner was composing by then. The longest
 * legitimate upload is a phone photo over the pigeon relay, well under this.
 */
export const UPLOAD_TIMEOUT_MS = 60_000;

/** Why an upload did not produce an image; `kind` is what the UI reports. */
export class UploadError extends Error {
  readonly kind: 'timeout' | 'cancelled' | 'rejected' | 'network';
  constructor(kind: UploadError['kind'], message: string) {
    super(message);
    this.name = 'UploadError';
    this.kind = kind;
  }
}

export type UploadOptions = {
  /** Caller-side cancel (chip removed, composition closed). */
  signal?: AbortSignal;
  timeoutMs?: number;
};

function abortReason(signal: AbortSignal | undefined): unknown {
  return signal && 'reason' in signal ? signal.reason : undefined;
}

export async function uploadPastedImage(
  file: File,
  fetchImpl: typeof fetch = fetch,
  opts: UploadOptions = {},
): Promise<UploadedImage> {
  const timeoutMs = opts.timeoutMs ?? UPLOAD_TIMEOUT_MS;
  const controller = new AbortController();
  const timeoutError = new UploadError('timeout', 'upload timed out after ' + Math.round(timeoutMs / 1000) + 's');
  const cancelError = new UploadError('cancelled', 'upload cancelled');
  const timer = setTimeout(() => controller.abort(timeoutError), timeoutMs);
  const onCancel = () => controller.abort(cancelError);
  if (opts.signal) {
    if (opts.signal.aborted) onCancel();
    else opts.signal.addEventListener('abort', onCancel, { once: true });
  }
  const fd = new FormData();
  fd.append('file', file, file.name || 'paste.png');
  try {
    // Cancelled before the first byte: nothing to send.
    if (controller.signal.aborted) throw cancelError;
    let res: Response;
    try {
      res = await fetchImpl('/api/images', { method: 'POST', body: fd, signal: controller.signal });
    } catch (err) {
      const reason = abortReason(controller.signal);
      if (reason instanceof UploadError) throw reason;
      if (controller.signal.aborted) throw cancelError;
      throw new UploadError('network', err instanceof Error && err.message ? err.message : 'upload failed');
    }
    if (!res.ok) throw new UploadError('rejected', 'upload ' + res.status);
    const meta = (await res.json()) as Record<string, unknown>;
    const id = String(meta.id || '');
    if (!id) throw new UploadError('rejected', 'upload missing id');
    return {
      id,
      url: String(meta.url || imageFullSrc(id)),
      thumbUrl: String(meta.thumb_url || imageThumbSrc(id)),
      marker: String(meta.marker || imageMarker(id)),
      width: Number(meta.width) || undefined,
      height: Number(meta.height) || undefined,
    };
  } finally {
    clearTimeout(timer);
    opts.signal?.removeEventListener('abort', onCancel);
  }
}

export function objectUrlFor(file: File): string | undefined {
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

/**
 * 🎯T562.3: a queued send carries its images as the same `[image: id]` markers
 * composeSendText prepends, so they survive reload and delivery with the text.
 * This is the inverse: the ids and the text left over.
 */
export function splitImageMarkers(text: string): { ids: string[]; text: string } {
  const src = String(text ?? '');
  const ids: string[] = [];
  const re = new RegExp(IMAGE_MARKER_RE.source, 'gi');
  let m: RegExpExecArray | null;
  while ((m = re.exec(src)) !== null) ids.push(m[1].toLowerCase());
  const rest = src.replace(new RegExp(IMAGE_MARKER_RE.source, 'gi'), '').replace(/^\s+|\s+$/g, '');
  return { ids, text: rest };
}

/** Rebuild a pending-image record from a durable id (thumb and full URLs are id-derived). */
export function imageFromId(id: string): UploadedImage {
  const n = String(id).trim().toLowerCase();
  return { id: n, url: imageFullSrc(n), thumbUrl: imageThumbSrc(n), marker: imageMarker(n) };
}
