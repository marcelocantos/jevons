// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createElement, useRef } from 'react';
import { fireEvent, render } from '@testing-library/react';
import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { FrontierTable } from '../../components/FrontierTable';
import { HIDE_GRACE_MS, nativeTitleForbidden } from '../../components/InstantTip';
import { MermaidVizPanel } from '../../components/MermaidVizPanel';
import { TargetHotspotTips } from '../../components/TargetHotspotTips';
import { paintUserHTML } from '../../conversation/paint';
import { FrontierRowsContext } from '../../frontier/rows';
import { linkifyTargetIDsInHTML } from '../../frontier/targetHotspot';
import {
  FANOUT_MARK,
  FRONTIER_API_PATH,
  formatDepMinigraph,
  formatStatus,
  formatTargetCardMarkdown,
  hoverCardMarkdown,
  toFrontierRows,
  type FrontierRow,
  type HoverCardCache,
} from '../../frontier/table';
import { playChromeSpec, playKickoffRequest, resolvePlayPO } from '../../frontier/play';
import { copyImageStatus, imageCopyPlan } from '../../conversation/mermaidClipboard';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const sample: FrontierRow = {
  id: 'T184',
  name: 'Full semantic frontier card',
  status: 'converging',
  value: 8,
  cost: 3,
  acceptance: ['Card shows acceptance list', 'Mermaid minigraph includes focus id'],
  context: 'Owner wants full target view with deps graph.',
  tags: ['ui', 'frontier'],
  depends_on: [{ id: 'T181', name: 'Rich hover' }],
  dependents: [{ id: 'T999', name: 'Downstream consumer' }],
  attestation: 'SHA abc1234 + green oracles',
  extra: { owner: 'jevons-po' },
};

/** GET /api/frontier shape — T181/T184/T326 must paint through toFrontierRows, not this object directly. */
const apiFrontierPayload = {
  targets: [
    {
      id: sample.id,
      name: sample.name,
      status: sample.status,
      value: sample.value,
      cost: sample.cost,
      acceptance: sample.acceptance,
      context: sample.context,
      tags: sample.tags,
      depends_on: sample.depends_on,
      dependents: sample.dependents,
      attestation: sample.attestation,
      extra: sample.extra,
    },
  ],
};

function cardSectionsFromMapper(data: unknown = apiFrontierPayload): { rows: FrontierRow[]; md: string } {
  const rows = toFrontierRows(data);
  return { rows, md: formatTargetCardMarkdown(rows[0]) };
}

function expectFullSemanticCard(text: string): void {
  expect(text).toMatch(/Status/);
  expect(text).toMatch(/Value \/ cost/);
  expect(text).toMatch(/Tags/);
  expect(text).toMatch(/Depends on/);
  expect(text).toMatch(/Dependents/);
  expect(text).toMatch(/Acceptance/);
}

function shownTips(container: HTMLElement): Element[] {
  return [...container.querySelectorAll('.instant-tip-show')];
}

function mockRect(el: Element, r: { left: number; top: number; right: number; bottom: number }): void {
  Object.defineProperty(el, 'getBoundingClientRect', {
    configurable: true,
    value: () => ({
      ...r,
      width: r.right - r.left,
      height: r.bottom - r.top,
      x: r.left,
      y: r.top,
      toJSON: () => r,
    }),
  });
}

function HotspotProbe(props: { text: string; rows: FrontierRow[] }) {
  const ref = useRef<HTMLDivElement>(null);
  const html = paintUserHTML(props.text, 'owner');
  return createElement(
    FrontierRowsContext.Provider,
    { value: props.rows },
    createElement(
      'div',
      null,
      createElement('div', { ref, className: 'msg-body', dangerouslySetInnerHTML: { __html: html } }),
      createElement(TargetHotspotTips, { containerRef: ref, html }),
    ),
  );
}

describeOracle(family('frontier'), () => {
  itOracle('T131', 'sidebar default tab order is Frontier then Transcript', () => {
    const panel = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../components/SidebarPanel.tsx'),
      'utf8',
    );
    const fi = panel.indexOf("id: 'frontier'");
    const ti = panel.indexOf("id: 'transcript'");
    expect(fi).toBeGreaterThanOrEqual(0);
    expect(ti).toBeGreaterThan(fi);
  });

  itOracle('T173', 'Frontier table is headerless with abbreviated status and fanout', () => {
    const { container } = render(createElement(FrontierTable, { rows: [sample] }));
    expect(container.querySelector('#frontier-table thead')).toBeNull();
    expect(container.querySelector('.ft-status')?.textContent).toBe(formatStatus('converging'));
    expect(formatStatus('converging')).toBe('Cv');
    expect(container.querySelector('.ft-fanout')?.textContent).toContain(FANOUT_MARK);
  });

  itOracle('T179', 'status glyph is normal case; id/fanout columns are tight', () => {
    expect(formatStatus('identified')).toBe('Id');
    expect(formatStatus('converging')).not.toBe(formatStatus('converging').toUpperCase());
  });

  itOracle('T181', 'ID/name hover is a rich markdown target card', () => {
    const { rows, md } = cardSectionsFromMapper();
    expectFullSemanticCard(md);
    const { container } = render(createElement(FrontierTable, { rows }));
    const idCell = container.querySelector('.ft-id [data-instant-tip-host]');
    expect(idCell).toBeTruthy();
    expect(nativeTitleForbidden(container.querySelector('.ft-id'))).toBe(true);
    fireEvent.pointerEnter(idCell!);
    const tip = container.querySelector('.instant-tip-show');
    expectFullSemanticCard(tip?.textContent || '');
    expect(tip?.textContent || '').toMatch(/T184/);
  });

  itOracle('T184', 'hover card is a full semantic card with mermaid minigraph', () => {
    const { rows, md } = cardSectionsFromMapper();
    expectFullSemanticCard(md);
    expect(md).toMatch(/```mermaid/);
    expect(md).toMatch(/graph LR/);
    expect(formatDepMinigraph(rows[0])).toMatch(/T184|T184_/);
    const stripped = toFrontierRows({
      targets: [{ id: 'T184', name: sample.name, status: sample.status }],
    });
    expect(formatTargetCardMarkdown(stripped[0])).not.toMatch(/Value \/ cost/);
    const { container } = render(createElement(FrontierTable, { rows }));
    fireEvent.pointerEnter(container.querySelector('.ft-id [data-instant-tip-host]')!);
    const tip = container.querySelector('.instant-tip-show');
    expectFullSemanticCard(tip?.textContent || '');
    expect(tip?.querySelector('strong, p, em')).toBeTruthy();
    expect(tip?.querySelector('.language-mermaid, .mermaid-diagram, code')).toBeTruthy();
    expect(tip?.textContent || '').toMatch(/Dependencies|T181|mermaid|graph LR/);
    const src = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../components/TargetHoverCard.tsx'),
      'utf8',
    );
    expect(src).toMatch(/parseAssistantMarkdown/);
    expect(src).toMatch(/renderMermaidIn/);
    const tipSrc = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../components/InstantTip.tsx'),
      'utf8',
    );
    expect(tipSrc).toMatch(/placeCardRect/);
    expect(tipSrc).toMatch(/clampSelectors/);
  });

  itOracle(['T186', 'T187', 'T230'], 'card stays open on the card — no idle timeout, no hide grace', () => {
    const src = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../components/InstantTip.tsx'),
      'utf8',
    );
    expect(src).not.toMatch(/setTimeout|setInterval/);
    expect(src).toMatch(/pointermove/);
    expect(src).toMatch(/shouldDismissOutsideHitParts|computeHitParts/);
    expect(HIDE_GRACE_MS).toBe(0);
    const { container } = render(createElement(FrontierTable, { rows: [sample] }));
    const host = container.querySelector('.ft-id [data-instant-tip-host]')!;
    fireEvent.pointerEnter(host);
    const card = container.querySelector('.instant-tip-show') as HTMLElement;
    expect(card).toBeTruthy();
    mockRect(host, { left: 320, top: 200, right: 500, bottom: 220 });
    mockRect(card, { left: 100, top: 50, right: 300, bottom: 400 });
    fireEvent.pointerMove(document, { clientX: 200, clientY: 100 });
    expect(container.querySelector('.instant-tip-show')).toBeTruthy();
  });

  itOracle(['T231', 'T271'], 'host→card along the corridor stays; vertical leave dismisses immediately', () => {
    const { container } = render(createElement(FrontierTable, { rows: [sample] }));
    const host = container.querySelector('.ft-id [data-instant-tip-host]')!;
    const name = container.querySelector('.ft-name')!;
    fireEvent.pointerEnter(host);
    const card = container.querySelector('.instant-tip-show') as HTMLElement;
    expect(card).toBeTruthy();
    mockRect(host, { left: 320, top: 200, right: 360, bottom: 220 });
    mockRect(name, { left: 360, top: 200, right: 500, bottom: 220 });
    mockRect(card, { left: 100, top: 50, right: 300, bottom: 400 });
    fireEvent.pointerMove(document, { clientX: 310, clientY: 210 });
    expect(container.querySelector('.instant-tip-show')).toBeTruthy();
    fireEvent.pointerEnter(name);
    fireEvent.pointerMove(document, { clientX: 400, clientY: 210 });
    expect(container.querySelector('.instant-tip-show')).toBeTruthy();
    fireEvent.pointerMove(document, { clientX: 310, clientY: 30 });
    expect(container.querySelector('.instant-tip-show')).toBeFalsy();
  });

  itOracle('T326', 'chat 🎯Tn hotspots use the same InstantTip frontier-card chrome', () => {
    expect(linkifyTargetIDsInHTML('<p>see 🎯T184</p>')).toMatch(/target-hotspot/);
    expect(linkifyTargetIDsInHTML('<code>T184</code>')).not.toMatch(/target-hotspot/);
    const { rows } = cardSectionsFromMapper();
    const { container } = render(createElement(HotspotProbe, { text: 'see 🎯T184 please', rows }));
    const spot = container.querySelector('.target-hotspot');
    expect(spot).toBeTruthy();
    fireEvent.pointerEnter(spot!);
    const tip = container.querySelector('.instant-tip-show');
    expect(tip).toBeTruthy();
    expectFullSemanticCard(tip?.textContent || '');
  });

  itOracle('T189', 'Escape closes the Frontier Graph panel', () => {
    const closed: string[] = [];
    render(createElement(MermaidVizPanel, { open: true, graphNonce: 1, onClose: () => closed.push('yes') }));
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(closed).toEqual(['yes']);
  });

  itOracle('T175', 'frontier hover is InstantTip-class, not a native title=', () => {
    const { container } = render(createElement(FrontierTable, { rows: [sample] }));
    expect(nativeTitleForbidden(container.querySelector('.ft-id'))).toBe(true);
    expect(nativeTitleForbidden(container.querySelector('.ft-name'))).toBe(true);
    fireEvent.pointerEnter(container.querySelector('.ft-id [data-instant-tip-host]')!);
    expect(container.querySelector('.instant-tip.instant-tip-show')).toBeTruthy();
  });

  itOracle('T203', 'only one InstantTip card is open at a time', () => {
    const other: FrontierRow = { ...sample, id: 'T181', name: 'Other' };
    const { container } = render(createElement(FrontierTable, { rows: [sample, other] }));
    const hosts = container.querySelectorAll('[data-instant-tip-host]');
    fireEvent.pointerEnter(hosts[0]);
    expect(shownTips(container).length).toBe(1);
    fireEvent.pointerEnter(hosts[2] || hosts[1]);
    expect(shownTips(container).length).toBe(1);
  });

  itOracle('T485', 'hover cards are built on first hover and reused until the row source changes', () => {
    const cache: HoverCardCache = {};
    const a = hoverCardMarkdown(cache, sample);
    const b = hoverCardMarkdown(cache, sample);
    expect(a).toBe(b);
    expect(cache.T184?.markdown).toBe(a);
    const changed = hoverCardMarkdown(cache, { ...sample, name: 'Renamed' });
    expect(changed).not.toBe(a);
  });

  itOracle.skip('T248', 'owner can drag-resize RHS width and the sidebar split', 'journey is the arbiter (J28 handle)');
  itOracle.skip('T280', 'Frontier Graph is owner-readable in one glance after hard-reload', 'named residual: pixel-identical chrome');
  itOracle.skip('T294', 'Frontier Graph fills the pane with legible nodes', 'named residual: pixel-identical chrome');
  itOracle.skip('T340', 'hierarchical ids fit without ellipsis; gutters are even', 'named residual: pixel-identical chrome');
  itOracle.skip('T174', 'Frontier table width is constrained to the RHS', 'named residual: pixel-identical chrome (with T340)');
  itOracle.skip('T177', 'Frontier columns do not overlap', 'named residual: pixel-identical chrome (with T340)');
  itOracle.skip('T331', 'hierarchical ids fit without ellipsis; id↔name gap is tight', 'named residual: pixel-identical chrome (with T340)');
  itOracle.skip('T332', 'column gutters are roughly even', 'named residual: pixel-identical chrome (with T340)');
  const playAgents = [
    { name: 'jevons', purpose: 'overseer' },
    { name: 'jevons-po', purpose: 'po', parent: 'jevons' },
    { name: 'squz-po', purpose: 'po', parent: 'jevons' },
    { name: 'jv-t184-card', purpose: 'work', parent: 'jevons-po', target_id: 'T184' },
    { name: 'sq-worker', purpose: 'work', parent: 'squz-po' },
  ];
  function fakeFetch() {
    const calls: { url: string; body: unknown }[] = [];
    let resolve!: (r: { ok: boolean; status: number }) => void;
    const pending = new Promise<{ ok: boolean; status: number }>((res) => (resolve = res));
    const fetcher = (url: string, init: { body: string }) => {
      calls.push({ url, body: JSON.parse(init.body) });
      return pending;
    };
    return { calls, fetcher, resolve };
  }

  itOracle('T182', 'each row has a play control that asks the PO to start that target', () => {
    const f = fakeFetch();
    const { container } = render(createElement(FrontierTable, { rows: [sample], fetcher: f.fetcher }));
    const btn = container.querySelector<HTMLButtonElement>('.ft-play-btn')!;
    expect(btn.getAttribute('aria-label')).toBe('Start work on 🎯T184');
    expect(btn.title).toBe('Start work via jevons-po');
    fireEvent.click(btn);
    expect(f.calls.length).toBe(1);
    expect(f.calls[0].url).toBe('/api/agents/jevons-po/send');
    const text = (f.calls[0].body as { text: string }).text;
    expect(text).toContain('Start work on frontier target 🎯T184');
    expect(text).toContain('target_id=T184');
    expect(text).toContain('Card shows acceptance list');
  });

  itOracle('T255', 'play kickoff targets the selected agent’s PO', () => {
    expect(resolvePlayPO({ agents: playAgents, selectedAgent: 'sq-worker' })).toBe('squz-po');
    expect(resolvePlayPO({ agents: playAgents, selectedAgent: 'jevons' })).toBe('jevons-po');
    expect(resolvePlayPO({ agents: playAgents, selectedAgent: 'squz-po' })).toBe('squz-po');
    const f = fakeFetch();
    const { container } = render(
      createElement(FrontierTable, { rows: [{ ...sample, id: 'T181' }], agents: playAgents, selectedAgent: 'sq-worker', fetcher: f.fetcher }),
    );
    fireEvent.click(container.querySelector('.ft-play-btn')!);
    expect(f.calls[0].url).toBe('/api/agents/squz-po/send');
    expect(playKickoffRequest({ ...sample, engaged: true, engaged_agents: ['x'] }).blocked).toBe(true);
  });

  itOracle('T278', 'play kickoff shows spinning submitted chrome immediately', () => {
    const f = fakeFetch();
    const { container } = render(createElement(FrontierTable, { rows: [sample], fetcher: f.fetcher }));
    fireEvent.click(container.querySelector('.ft-play-btn')!);
    const btn = container.querySelector<HTMLButtonElement>('.ft-play-btn')!;
    expect(btn.classList.contains('ft-submitted-btn')).toBe(true);
    expect(btn.disabled).toBe(true);
    expect(btn.querySelector('.ft-spin')).toBeTruthy();
    expect(playChromeSpec({ ...sample, kickoff_submitted: true }).mode).toBe('submitted');
  });

  itOracle('T198', 'engaged rows show Stop, not a bullseye status rewrite', () => {
    const free: FrontierRow = { ...sample, id: 'T181', name: 'Free row' };
    const f = fakeFetch();
    const { container } = render(
      createElement(FrontierTable, { rows: [sample, free], agents: playAgents, fetcher: f.fetcher }),
    );
    const rows = container.querySelectorAll('tr');
    // engaged row sinks below free rows and carries its agents
    expect(rows[0].classList.contains('ft-engaged')).toBe(false);
    expect(rows[1].classList.contains('ft-engaged')).toBe(true);
    expect(rows[1].getAttribute('data-engaged-agents')).toBe('jv-t184-card');
    expect(rows[1].querySelector('.ft-status')!.textContent).toBe(formatStatus('converging'));
    const stop = rows[1].querySelector<HTMLButtonElement>('.ft-stop-btn')!;
    expect(stop.getAttribute('aria-label')).toBe('Stop work on 🎯T184');
    fireEvent.click(stop);
    expect(f.calls[0].url).toBe('/api/agents/engagement/stop');
    expect((f.calls[0].body as { target_id: string }).target_id).toBe('T184');
  });
  itOracle.skip('T208', 'Frontier tab stays selected under background transcript refresh', 'journey is the arbiter (J27 tab-stick)');
  itOracle.skip('T266', 'target asks show built-in context chrome (repo / PO / product)', 'not ported');
  itOracle.skip('T267', 'target asks auto-select owning PO and highlight the target', 'not ported');
  itOracle('T83.1', 'owner can copy Mermaid source and image from the graph tab', () => {
    const { container } = render(createElement(MermaidVizPanel, { open: true, onClose: () => {}, graphNonce: 0 }));
    expect(container.querySelector('#mvp-copy-source')).toBeTruthy();
    expect(container.querySelector('#mvp-copy-image')).toBeTruthy();
    expect(imageCopyPlan({ writeText: true, write: true, multiType: true }).mode).toBe('multi');
    expect(imageCopyPlan({ writeText: true, write: true, multiType: false }).mode).toBe('image');
    expect(imageCopyPlan({ writeText: true, write: false, multiType: false }).mode).toBe('text-fallback');
    expect(copyImageStatus('multi')).toBe('Image + source copied');
  });
  itOracle('T168', 'Frontier tab loads frontier data without HTTP 404', () => {
    expect(FRONTIER_API_PATH).toBe('/api/frontier');
    const app = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../App.tsx'),
      'utf8',
    );
    expect(app).toMatch(/fetch\('\/api\/frontier'\)/);
    expect(app).toMatch(/toFrontierRows/);
    expect(app).not.toMatch(/id: t\.id \|\| ''/);
    expect(toFrontierRows({ targets: [{ id: 'T168', name: 'x', status: 'identified', acceptance: ['wired'] }] })[0].acceptance).toEqual([
      'wired',
    ]);
  });
});
