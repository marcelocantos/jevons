// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
import { fireEvent, render, waitFor } from '@testing-library/react';
import { createRef } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { TargetHotspotTips } from '../components/TargetHotspotTips';
import { FrontierRowsContext } from './rows';
import { clearFrontierTargetCache, fetchFrontierTarget, hoverCardMarkdown, type FrontierRow } from './table';
import { linkifyTargetIDsInHTML, linkifyTargetText, repoFromWorkdir } from './targetHotspot';

afterEach(() => { vi.unstubAllGlobals(); clearFrontierTargetCache(); });

it('qualifies foreign references atomically and skips code and links', () => {
  const html = linkifyTargetIDsInHTML('<p>claudia/T177 🎯claudia/T177 claudia/🎯T177 T177 T12.3</p><code>claudia/T177</code><a href="x">claudia/T177</a>');
  expect(html.match(/data-target-repo="claudia"/g)).toHaveLength(3);
  expect(html.match(/data-target-id="T177"/g)).toHaveLength(4);
  expect(html).toContain('data-target-id="T12.3"');
  expect(html).toContain('<code>claudia/T177</code><a href="x">claudia/T177</a>');
  expect(linkifyTargetText('src/claudia/T177')).toBe('src/claudia/T177');
  expect(linkifyTargetText('github.com/marcelocantos/claudia/T177')).toContain('data-target-repo="github.com/marcelocantos/claudia"');
  expect(linkifyTargetText('T177', 'claudia')).toContain('data-target-repo="claudia"');
  expect(repoFromWorkdir('/Users/m/work/github.com/org/claudia')).toBe('github.com/org/claudia');
  expect(repoFromWorkdir('/Users/m/work/github.com/org/.claudia-worktrees-claudia/agent')).toBe('github.com/org/claudia');
  expect(repoFromWorkdir('/Users/m/work/github.com/org/jevons')).toBeUndefined();
});

it('foreign and local ids have separate lookup and card caches, including misses', async () => {
  const fetcher = vi.fn(async (url: string) => new Response(JSON.stringify(url.includes('repo=claudia')
    ? { found: false } : { found: true, target: { id: 'T177', name: 'Local', status: 'identified' } }), { status: 200 }));
  vi.stubGlobal('fetch', fetcher);
  expect(await fetchFrontierTarget('T177', undefined, 'claudia')).toBeNull();
  expect((await fetchFrontierTarget('T177'))?.name).toBe('Local');
  expect(await fetchFrontierTarget('T177', undefined, 'claudia')).toBeNull();
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(String(fetcher.mock.calls[0][0])).toContain('repo=claudia');
  const cache = {};
  const local: FrontierRow = { id: 'T177', name: 'Local', status: 'identified' };
  const foreign: FrontierRow = { id: 'T177', name: 'Foreign', status: 'identified' };
  hoverCardMarkdown(cache, local);
  hoverCardMarkdown(cache, foreign, 'claudia');
  expect(Object.keys(cache).sort()).toEqual(['T177', 'claudia/T177']);
});

it('foreign hover does not show colliding local frontier and stale responses cannot overwrite a new target', async () => {
  let resolveOld!: (r: Response) => void;
  vi.stubGlobal('fetch', vi.fn((url: string) => String(url).includes('T177')
    ? new Promise<Response>(r => { resolveOld = r; })
    : Promise.resolve(new Response(JSON.stringify({ found: true, target: { id: 'T178', name: 'Foreign 178', status: 'identified' } })) )));
  const ref = createRef<HTMLDivElement>();
  const view = render(<FrontierRowsContext.Provider value={[{ id: 'T177', name: 'Wrong local', status: 'identified' }]}>
    <div><div ref={ref}><span className="target-hotspot" data-target-id="T177" data-target-repo="claudia">claudia/T177</span>
      <span className="target-hotspot" data-target-id="T178" data-target-repo="claudia">claudia/T178</span>
      </div><TargetHotspotTips containerRef={ref} /></div>
  </FrontierRowsContext.Provider>);
  const spots = view.container.querySelectorAll('.target-hotspot');
  fireEvent.pointerEnter(spots[0]);
  await waitFor(() => expect(resolveOld).toBeTypeOf('function'));
  expect(view.container.textContent).not.toContain('Wrong local'); // only the context row, not rendered
  fireEvent.pointerEnter(spots[1]);
  await waitFor(() => expect(view.container.querySelector('.instant-tip-show')?.textContent).toContain('Foreign 178'));
  resolveOld(new Response(JSON.stringify({ found: true, target: { id: 'T177', name: 'Stale 177' } })));
  await waitFor(() => expect(view.container.querySelector('.instant-tip-show')?.textContent).not.toContain('Stale 177'));
});

it('canonical identity deduplicates alias/canonical lookups without disclosing ledger paths', async () => {
  const fetcher = vi.fn(async () => new Response(JSON.stringify({ found: true, repo_identity: 'github.com/marcelocantos/claudia', ledger_key: '/private/path', target: { id: 'T177', name: 'Remote' } })));
  vi.stubGlobal('fetch', fetcher);
  expect((await fetchFrontierTarget('T177', undefined, 'claudia'))?.name).toBe('Remote');
  expect((await fetchFrontierTarget('T177', undefined, 'github.com/marcelocantos/claudia'))?.name).toBe('Remote');
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it('ambiguous foreign scope remains unresolved; it cannot fall back to same-id local row', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response('ambiguous', { status: 409 })));
  await expect(fetchFrontierTarget('T177', undefined, 'claudia')).rejects.toThrow('409');
  await expect(fetchFrontierTarget('T177')).rejects.toThrow(); // local is a separate request, not fallback
});

it('foreign ledger HTML is sanitized at the hovercard boundary while local rich HTML stays', async () => {
  const { TargetHoverCard } = await import('../components/TargetHoverCard');
  const payload = '<img src=x onerror="window.pwned=1"><svg onload="window.pwned=2"></svg>[click](javascript:alert(1))<strong data-t1056="rich">safe</strong>'; 
  const foreign = render(<TargetHoverCard markdown={payload} foreign />);
  expect(foreign.container.querySelector('[onerror], [onload]')).toBeNull();
  expect(foreign.container.querySelector('a[href^="javascript:"]')).toBeNull();
  expect(foreign.container.querySelector('strong[data-t1056]')?.textContent).toBe('safe');
  const local = render(<TargetHoverCard markdown={payload} />);
  expect(local.container.querySelector('[onerror]')).not.toBeNull(); // existing local-ledger rich HTML contract
});

it('sealed markdown honors fully qualified foreign targets and excludes existing anchors', async () => {
  const { parseAssistantMarkdown } = await import('../conversation/markdown');
  const html = parseAssistantMarkdown('claudia/T177 and github.com/marcelocantos/claudia/T177 and [claudia/T177](https://example.org)');
  expect(html.match(/data-target-id="T177"/g)).toHaveLength(2);
  expect(html).toContain('data-target-repo="github.com/marcelocantos/claudia"');
  expect(html).toContain('<a href="https://example.org">claudia/T177</a>');
});
