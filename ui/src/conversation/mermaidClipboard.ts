// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** 🎯T83.1: copy Mermaid source / image — port of web/index.html
 * copyMermaidSource / copyMermaidImage. Capability plan is pure so a
 * hermetic oracle can decide mode without a real clipboard. */

export type ClipboardCaps = { writeText: boolean; write: boolean; multiType: boolean };
export type CopyMode = 'multi' | 'image' | 'text' | 'text-legacy' | 'text-fallback';

export function mermaidClipboardCaps(nav: Partial<Navigator> | undefined = typeof navigator !== 'undefined' ? navigator : undefined): ClipboardCaps {
  const cb = nav && nav.clipboard;
  const hasItem = typeof globalThis.ClipboardItem !== 'undefined';
  return {
    writeText: !!(cb && typeof cb.writeText === 'function'),
    write: !!(cb && typeof cb.write === 'function' && hasItem),
    multiType: !!(cb && typeof cb.write === 'function' && hasItem),
  };
}

/** Which clipboard shape an image copy will use for the given capabilities. */
export function imageCopyPlan(caps: ClipboardCaps): { mode: CopyMode; reason?: string } {
  if (caps.multiType) return { mode: 'multi' };
  if (caps.write) return { mode: 'image' };
  return { mode: 'text-fallback', reason: 'image clipboard unavailable' };
}

export function svgMarkupFrom(container: ParentNode | null): string {
  const svg = container ? container.querySelector('svg') : null;
  return svg ? svg.outerHTML : '';
}

export async function copyMermaidSource(src: string): Promise<{ ok: true; mode: CopyMode }> {
  const text = String(src || '');
  const caps = mermaidClipboardCaps();
  if (caps.writeText) {
    await navigator.clipboard.writeText(text);
    return { ok: true, mode: 'text' };
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  ta.style.position = 'fixed';
  ta.style.left = '-9999px';
  document.body.appendChild(ta);
  ta.select();
  const ok = typeof document.execCommand === 'function' && document.execCommand('copy');
  document.body.removeChild(ta);
  if (!ok) throw new Error('clipboard text write failed');
  return { ok: true, mode: 'text-legacy' };
}

export function svgToPngBlob(svgMarkup: string): Promise<Blob> {
  return new Promise((resolve, reject) => {
    const svgBlob = new Blob([svgMarkup], { type: 'image/svg+xml;charset=utf-8' });
    const url = URL.createObjectURL(svgBlob);
    const img = new Image();
    img.onload = () => {
      try {
        const canvas = document.createElement('canvas');
        canvas.width = img.naturalWidth || 800;
        canvas.height = img.naturalHeight || 600;
        const ctx = canvas.getContext('2d');
        if (!ctx) throw new Error('no 2d context');
        ctx.drawImage(img, 0, 0);
        canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('toBlob failed'))), 'image/png');
      } catch (err) {
        reject(err);
      } finally {
        URL.revokeObjectURL(url);
      }
    };
    img.onerror = () => {
      URL.revokeObjectURL(url);
      reject(new Error('svg image load failed'));
    };
    img.src = url;
  });
}

export async function copyMermaidImage(src: string, svgMarkup: string): Promise<{ ok: true; mode: CopyMode; reason?: string }> {
  const caps = mermaidClipboardCaps();
  const plan = imageCopyPlan(caps);
  if (plan.mode === 'text-fallback' || !svgMarkup) {
    await copyMermaidSource(src);
    return { ok: true, mode: 'text-fallback', reason: plan.reason || 'no rendered SVG' };
  }
  const pngBlob = await svgToPngBlob(svgMarkup);
  const parts: Record<string, Blob> = { 'image/png': pngBlob };
  if (plan.mode === 'multi') parts['text/plain'] = new Blob([String(src || '')], { type: 'text/plain' });
  await navigator.clipboard.write([new ClipboardItem(parts)]);
  return { ok: true, mode: plan.mode };
}

export function copyImageStatus(mode: CopyMode): string {
  if (mode === 'multi') return 'Image + source copied';
  if (mode === 'image') return 'Image copied';
  return 'Source copied (image clipboard unavailable)';
}
