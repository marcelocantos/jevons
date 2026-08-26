// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Replace marked `language-mermaid` fences with SVG (vanilla T59). */

// Load the same UMD browser bundle vanilla pulls from the mermaid 11 CDN.
// Vite ESM interop of mermaid.min.js crashes (`this.mermaid` is undefined);
// a script tag matches the daily cockpit and Playwright Chromium smoke.
import mermaidUrl from 'mermaid/dist/mermaid.min.js?url';

type MermaidAPI = {
  initialize: (opts: Record<string, unknown>) => void;
  render: (id: string, src: string) => Promise<{ svg: string }>;
};

let seq = 0;
let started = false;
let loading: Promise<MermaidAPI> | null = null;

function rememberErr(err: unknown): void {
  const w = window as Window & { __jevonsMermaidErrs?: string[] };
  if (!w.__jevonsMermaidErrs) w.__jevonsMermaidErrs = [];
  w.__jevonsMermaidErrs.push(String(err instanceof Error ? err.stack || err.message : err));
}

function loadMermaid(): Promise<MermaidAPI> {
  const existing = (window as Window & { mermaid?: MermaidAPI }).mermaid;
  if (existing) return Promise.resolve(existing);
  if (loading) return loading;
  loading = new Promise<MermaidAPI>((resolve, reject) => {
    const s = document.createElement('script');
    s.src = mermaidUrl;
    s.onload = () => {
      const api = (window as Window & { mermaid?: MermaidAPI }).mermaid;
      if (!api) {
        reject(new Error('mermaid script loaded without window.mermaid'));
        return;
      }
      resolve(api);
    };
    s.onerror = () => reject(new Error('mermaid.min.js failed to load'));
    document.head.appendChild(s);
  });
  return loading;
}

/** Kick the UMD load so the first transcript paint does not wait on the script. */
export function preloadMermaid(): void {
  void loadMermaid().catch(rememberErr);
}

async function ensureMermaid(): Promise<MermaidAPI> {
  const api = await loadMermaid();
  if (!started) {
    const dark = document.documentElement.getAttribute('data-theme') !== 'light';
    api.initialize({
      startOnLoad: false,
      securityLevel: 'strict',
      suppressErrorRendering: true,
      theme: dark ? 'dark' : 'default',
      look: 'classic',
    });
    started = true;
  }
  return api;
}

export async function renderMermaidIn(container: ParentNode | null): Promise<void> {
  if (!container) return;
  // Await the script BEFORE querying. Hydrate remounts bubbles during the
  // first load; a NodeList captured earlier points at detached fences.
  let api: MermaidAPI;
  try {
    api = await ensureMermaid();
  } catch (err) {
    rememberErr(err);
    return;
  }
  if (!(container as Node).isConnected) return;
  const codes = container.querySelectorAll('code.language-mermaid');
  if (!codes.length) return;
  for (const code of codes) {
    if (!code.isConnected) continue;
    const host = code.closest('pre') || code;
    const src = code.textContent || '';
    if (!src.trim()) continue;
    const id = 'mmd-' + ++seq;
    try {
      const { svg } = await api.render(id, src);
      if (!svg || !/<(svg|SVG)\b/.test(svg)) {
        throw new Error('mermaid.render returned no SVG');
      }
      if (!host.isConnected || !host.parentNode) continue;
      const wrap = document.createElement('div');
      wrap.className = 'mermaid mermaid-diagram';
      wrap.innerHTML = svg;
      host.parentNode.replaceChild(wrap, host);
    } catch (err) {
      rememberErr(err);
      [id, 'd' + id].forEach((x) => document.getElementById(x)?.remove());
    }
  }
}

if (typeof window !== 'undefined') {
  preloadMermaid();
}
