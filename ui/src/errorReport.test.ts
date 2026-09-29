// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { installErrorReport, MAX_REPORTS_PER_PAGE, type ErrorReport } from './errorReport';

const ASSET = 'http://127.0.0.1:13705/assets/index-abc123.js';

function referenceError(name: string): ErrorEvent {
  const err = new ReferenceError(`${name} is not defined`);
  return new ErrorEvent('error', { error: err, message: `Uncaught ReferenceError: ${name} is not defined`, filename: ASSET, lineno: 7053, colno: 48 });
}

// A private target, not the jsdom window: an error event the sensor has
// stopped listening to would otherwise surface as an uncaught test error.
function fakeWindow(): Window {
  return new EventTarget() as unknown as Window;
}

describe('🎯T505 cockpit error sensor', () => {
  let uninstall: (() => void) | undefined;
  let win: Window;
  beforeEach(() => {
    win = fakeWindow();
  });
  afterEach(() => uninstall?.());

  it('journals an uncaught ReferenceError in the vanilla window.onerror shape, tagged with the served asset', () => {
    const posts: ErrorReport[] = [];
    uninstall = installErrorReport(win, (r) => posts.push(r), ASSET);
    win.dispatchEvent(referenceError('closeProviderMenu'));
    expect(posts).toHaveLength(1);
    expect(posts[0].level).toBe('error');
    expect(posts[0].msg).toBe('window.onerror');
    expect(posts[0].fields).toMatchObject({
      component: 'window',
      message: 'ReferenceError: closeProviderMenu is not defined',
      filename: ASSET,
      lineno: 7053,
      colno: 48,
      asset: ASSET,
    });
    expect(String(posts[0].fields.stack)).toContain('ReferenceError');
  });

  it('journals an unhandled rejection', () => {
    const posts: ErrorReport[] = [];
    uninstall = installErrorReport(win, (r) => posts.push(r), ASSET);
    const reason = new TypeError('boom');
    const ev = new Event('unhandledrejection') as PromiseRejectionEvent;
    Object.defineProperty(ev, 'reason', { value: reason });
    win.dispatchEvent(ev);
    expect(posts).toHaveLength(1);
    expect(posts[0].msg).toBe('window.unhandledrejection');
    expect(posts[0].fields).toMatchObject({ component: 'window', message: 'TypeError: boom', asset: ASSET });
  });

  it('reports a render loop rethrowing the same error once', () => {
    const posts: ErrorReport[] = [];
    uninstall = installErrorReport(win, (r) => posts.push(r), ASSET);
    for (let i = 0; i < 50; i++) win.dispatchEvent(referenceError('children'));
    expect(posts).toHaveLength(1);
  });

  it('caps distinct reports per page', () => {
    const posts: ErrorReport[] = [];
    uninstall = installErrorReport(win, (r) => posts.push(r), ASSET);
    for (let i = 0; i < MAX_REPORTS_PER_PAGE + 10; i++) win.dispatchEvent(referenceError(`v${i}`));
    expect(posts).toHaveLength(MAX_REPORTS_PER_PAGE);
  });

  it('posts to /api/log by default', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })));
    vi.stubGlobal('fetch', fetchMock);
    try {
      uninstall = installErrorReport(win, undefined, ASSET);
      win.dispatchEvent(referenceError('lastTranscriptBubbleBottom'));
      expect(fetchMock).toHaveBeenCalledTimes(1);
      const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
      expect(url).toBe('/api/log');
      expect(init.method).toBe('POST');
      expect(JSON.parse(String(init.body))).toMatchObject({ level: 'error', msg: 'window.onerror', fields: { component: 'window' } });
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('stops reporting once uninstalled', () => {
    const posts: ErrorReport[] = [];
    installErrorReport(win, (r) => posts.push(r), ASSET)();
    win.dispatchEvent(referenceError('children'));
    expect(posts).toHaveLength(0);
  });
});
