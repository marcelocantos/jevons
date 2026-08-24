// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createElement } from 'react';
import { fireEvent, render } from '@testing-library/react';
import { FrontierTable } from '../../components/FrontierTable';
import { SidebarPanel } from '../../components/SidebarPanel';
import {
  FRONTIER_API_PATH,
  cardSourceFingerprint,
  chromeColumnIsContentMin,
  cssBlock,
  expireCardCache,
  formatFanout,
  formatStatus,
  formatTargetCardMarkdown,
  hoverCardMarkdown,
  idColumnClipsHierarchical,
  idRemChasm,
  idUses7chClip,
  nameFillsRemainder,
  shouldReuseHoverCard,
  statusFanFacingCollapse,
  statusUsesSmallCaps,
  tableLayoutIsAuto,
  tdHasSharedHorizontalPad,
  type HoverCardCache,
} from '../../frontier/table';
import {
  clampFleetFraction,
  clampSidebarWidth,
  fleetFractionFromPointer,
  sidebarWidthFromPointer,
} from '../../layout/rhsLayout';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const uiSrc = join(dirname(fileURLToPath(import.meta.url)), '../..');
const css = readFileSync(join(uiSrc, 'cockpit.css'), 'utf8');
const app = readFileSync(join(uiSrc, 'App.tsx'), 'utf8');
const tableSrc = readFileSync(join(uiSrc, 'components/FrontierTable.tsx'), 'utf8');
const hoverSrc = readFileSync(join(uiSrc, 'components/TargetHoverCard.tsx'), 'utf8');
const sidebarSrc = readFileSync(join(uiSrc, 'components/SidebarPanel.tsx'), 'utf8');

const CARD_ROW = {
  id: 'T181',
  name: 'Rich target hover on Frontier',
  status: 'Converging',
  acceptance: [
    'Hovering a frontier target ID shows InstantTip with full target',
    'Hermetic tip includes acceptance text',
  ],
  context: 'Owner wants fully expanded target in rich markdown.',
  tags: ['ui', 'frontier'],
  dependents: [{ id: 'T999', name: 'Downstream' }],
  fanout: 1,
};

describeOracle(family('frontier'), () => {
  itOracle('T131', 'sidebar default tab order is Frontier then Transcript', () => {
    const fi = sidebarSrc.indexOf("id: 'frontier'");
    const ti = sidebarSrc.indexOf("id: 'transcript'");
    expect(fi).toBeGreaterThanOrEqual(0);
    expect(ti).toBeGreaterThan(fi);
  });

  itOracle('T168', 'Frontier tab loads frontier data without HTTP 404', () => {
    expect(FRONTIER_API_PATH).toBe('/api/frontier');
    expect(app).toContain("fetch('" + FRONTIER_API_PATH + "')");
    expect(app).toMatch(/if\s*\(!r\.ok\)\s*return\s*\[\]/);
    expect(app).not.toMatch(/\/Users\/.*\/bullseye\.yaml/);
    expect(app).not.toContain('.local/share/bullseye');
    expect(app).not.toMatch(/const\s+BULLSEYE_PATH\s*=\s*['"]bullseye\.yaml['"]/);
    expect(app).toContain("enabled: tab === 'frontier'");
  });

  itOracle('T173', 'Frontier table is headerless with abbreviated status and fanout', () => {
    expect(formatStatus('Converging')).toBe('Cv');
    expect(formatStatus('converging')).toBe('Cv');
    expect(formatStatus('Identified')).toBe('Id');
    expect(formatStatus('set_aside')).toBe('Sa');
    expect(formatStatus('')).toBe('—');
    const zero = formatFanout(0, 'T10.2');
    expect(zero.visible).toBe(false);
    expect(zero.text).toBe('');
    const many = formatFanout(4, 'T10.2');
    expect(many.visible).toBe(true);
    expect(many.text).toBe('4\u169B');
    expect(tableSrc).not.toMatch(/<thead/);
    expect(tableSrc).not.toMatch(/<th[\s>]/);
    const { container } = render(
      createElement(FrontierTable, {
        rows: [
          { id: 'T173', name: 'Headerless', status: 'Converging', fanout: 3 },
          { id: 'T1', name: 'Empty fan', status: 'identified' },
        ],
      }),
    );
    expect(container.querySelector('thead')).toBeNull();
    expect(container.querySelector('th')).toBeNull();
    const row = container.querySelector('tr');
    expect(row?.querySelector('.ft-status')?.textContent).toBe('Cv');
    expect(row?.querySelector('.ft-fanout')?.textContent).toBe('3\u169B');
    expect(container.querySelectorAll('.ft-fanout-empty').length).toBe(1);
  });

  itOracle('T179', 'status glyph is normal case; id/fanout columns are tight', () => {
    const statusRule = cssBlock(css, '#frontier-table .ft-status');
    const idRule = cssBlock(css, '#frontier-table .ft-id');
    const fanRule = cssBlock(css, '#frontier-table .ft-fanout');
    expect(statusRule).toBeTruthy();
    expect(statusUsesSmallCaps(statusRule)).toBe(false);
    expect(chromeColumnIsContentMin(idRule)).toBe(true);
    expect(chromeColumnIsContentMin(statusRule)).toBe(true);
    expect(chromeColumnIsContentMin(fanRule)).toBe(true);
    expect(idRemChasm(idRule)).toBe(false);
    const deps = formatFanout(4, 'T10.2', [
      { id: 'T10.3', name: 'Client requests table drives server actions' },
      { id: 'T10.4', name: 'Reconnect uses diff sync only' },
      { id: 'T10.5', name: 'Short' },
      { id: 'T10.6', name: 'Also blocked' },
    ]);
    expect(deps.title.indexOf('4 targets depend on T10.2')).toBe(0);
    expect(deps.title).toContain('• T10.3 Client requests table drives server actions');
    expect(deps.title).not.toContain('—');
  });

  itOracle('T181', 'ID/name hover is a rich markdown target card', () => {
    const md = formatTargetCardMarkdown(CARD_ROW);
    expect(md).toContain('🎯T181');
    expect(md).toContain('Rich target hover on Frontier');
    expect(md).toMatch(/\*\*Status:\*\*/);
    expect(md).toContain('**Acceptance**');
    expect(md).toContain('Hovering a frontier target ID shows InstantTip with full target');
    expect(md).toContain('**Context**');
    expect(md).toContain('**Tags:**');
    expect(md).toContain('**Dependents**');
    expect(md.split('\n').length).toBeGreaterThanOrEqual(6);
    expect(formatTargetCardMarkdown(null)).toBe('');
    expect(hoverSrc).toMatch(/instant-tip-card|target-hover-card/);
    const { container } = render(createElement(FrontierTable, { rows: [CARD_ROW] }));
    const idCell = container.querySelector('.ft-id');
    const nameCell = container.querySelector('.ft-name');
    expect(idCell?.className).toContain('has-instant-tip');
    expect(nameCell?.className).toContain('has-instant-tip');
    fireEvent.mouseEnter(container.querySelector('tr') as HTMLElement);
    const card = container.querySelector('.target-hover-card');
    expect(card).toBeTruthy();
    expect(card?.textContent).toContain('Acceptance');
    expect(card?.textContent).toContain('Hermetic tip includes acceptance text');
  });

  itOracle('T248', 'owner can drag-resize RHS width and the sidebar split', () => {
    expect(clampSidebarWidth(100, 1000)).toBe(220);
    expect(clampSidebarWidth(900, 1000)).toBe(720);
    expect(sidebarWidthFromPointer(580, 1000)).toBe(420);
    expect(fleetFractionFromPointer(200, 500)).toBeGreaterThan(0);
    expect(clampFleetFraction(0.01, 400)).toBeGreaterThan(0);
    expect(app).toContain('id="rhs-width-handle"');
    expect(app).toContain('id="rhs-split-handle"');
    expect(app).toContain("kind: 'width'");
    expect(app).toContain("kind: 'fleet'");
    expect(app).toContain('sidebarWidthFromPointer');
    expect(app).toContain('fleetFractionFromPointer');
  });

  itOracle.skip(
    'T280',
    'Frontier Graph is owner-readable in one glance after hard-reload',
    'visual exception: one-glance readability is a T493 prose look, not a hermetic helper predicate',
  );

  itOracle('T294', 'Frontier Graph fills the pane with legible nodes', () => {
    expect(sidebarSrc).toContain('id="frontier-graph"');
    const { container } = render(
      createElement(SidebarPanel, { tab: 'frontier', onTab: () => {} }, createElement('span')),
    );
    const graph = container.querySelector('#frontier-graph');
    expect(graph).toBeTruthy();
    expect(graph?.textContent).toMatch(/Graph/);
    expect(container.querySelector('#frontier-refresh')).toBeTruthy();
    expect(css).toMatch(/#mermaid-viz-panel\.mvp-large\s*\{/);
    expect(css).toMatch(/90vw/);
    expect(css).toMatch(/90vh/);
    expect(css).toMatch(/#mermaid-viz-panel\s+\.mvp-body\s+svg\s*\{[^}]*max-width:\s*100%/);
    expect(css).toMatch(/mvp-scale-fill/);
  });

  itOracle('T340', 'hierarchical ids fit without ellipsis; gutters are even', () => {
    const tableRule = cssBlock(css, '#frontier-table');
    const tdRule = cssBlock(css, '#frontier-table td');
    const idRule = cssBlock(css, '#frontier-table .ft-id');
    const nameRule = cssBlock(css, '#frontier-table .ft-name');
    const statusRule = cssBlock(css, '#frontier-table .ft-status');
    const fanRule = cssBlock(css, '#frontier-table .ft-fanout');
    expect(tableLayoutIsAuto(tableRule)).toBe(true);
    expect(tdHasSharedHorizontalPad(tdRule)).toBe(true);
    expect(idColumnClipsHierarchical(idRule)).toBe(false);
    expect(idRule).toMatch(/overflow:\s*visible/);
    expect(idRule).toMatch(/white-space:\s*nowrap/);
    expect(idUses7chClip(idRule)).toBe(false);
    expect(idRemChasm(idRule)).toBe(false);
    expect(chromeColumnIsContentMin(idRule)).toBe(true);
    expect(chromeColumnIsContentMin(statusRule)).toBe(true);
    expect(chromeColumnIsContentMin(fanRule)).toBe(true);
    expect(statusFanFacingCollapse(statusRule, fanRule)).toBe(false);
    expect(nameFillsRemainder(nameRule)).toBe(true);
    const { container } = render(
      createElement(FrontierTable, {
        rows: [{ id: 'T254.1', name: 'Hierarchical id', status: 'identified' }],
      }),
    );
    expect(container.querySelector('.ft-id')?.textContent).toBe('🎯T254.1');
  });

  itOracle('T485', 'hover cards are built on first hover and reused until the row source changes', () => {
    const fa = cardSourceFingerprint(CARD_ROW);
    const alias = cardSourceFingerprint({
      ...CARD_ROW,
      dependents: [{ id: 'T999', name: 'Downstream' }],
    });
    expect(fa).toBeTruthy();
    expect(fa).toBe(alias);
    expect(cardSourceFingerprint({ ...CARD_ROW, name: 'Renamed' })).not.toBe(fa);

    const cache: HoverCardCache = {};
    const first = hoverCardMarkdown(cache, CARD_ROW);
    expect(first).toContain('**Acceptance**');
    expect(shouldReuseHoverCard(cache.T181, CARD_ROW)).toBe(true);
    const reused = hoverCardMarkdown(cache, CARD_ROW);
    expect(reused).toBe(first);
    expect(cache.T181.markdown).toBe(first);

    const sameFetch = expireCardCache(cache, [CARD_ROW]);
    expect(sameFetch.kept).toBe(1);
    expect(sameFetch.expired).toBe(0);
    expect(cache.T181.markdown).toBe(first);

    const afterRename = expireCardCache(cache, [{ ...CARD_ROW, name: 'Renamed' }]);
    expect(afterRename.expired).toBe(1);
    expect(cache.T181).toBeUndefined();
    expect(shouldReuseHoverCard(undefined, CARD_ROW)).toBe(false);
  });
});
