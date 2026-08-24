// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createElement } from 'react';
import { render } from '@testing-library/react';
import { expect } from 'vitest';
import { AgentTree, buildAgentForest, type AgentNode, type AgentRow } from '../../components/AgentTree';
import { fleetSecondary, isStateDirOverseerHome, shouldShowPathSecondary } from '../../fleet/rowModel';
import { CompanyMark, companyOfProvider } from '../../plan/companyMark';
import {
  companyFor,
  familyInitial,
  modelPrefix,
  versionOf,
} from '../../plan/modelPrefix';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const uiSrc = join(dirname(fileURLToPath(import.meta.url)), '../..');
const css = readFileSync(join(uiSrc, 'cockpit.css'), 'utf8');
const appSrc = readFileSync(join(uiSrc, 'App.tsx'), 'utf8');
const treeSrc = readFileSync(join(uiSrc, 'components/AgentTree.tsx'), 'utf8');
const markSrc = readFileSync(join(uiSrc, 'plan/companyMark.tsx'), 'utf8');

const RETIRED_MARKS = [
  'M3 3h4.2l13.8 18h-4.2L3 3z',
  'M13.6 2.6h7L9.4 21.4h-7l11.2-18.8z',
  'M16.23 5h-3.02l5.507 15h3.02L16.23 5z',
  'M12 12L12 3M12 12L15.89 5.94',
];

function flattenNames(nodes: AgentNode[]): string[] {
  const out: string[] = [];
  const walk = (ns: AgentNode[]) => {
    for (const n of ns) {
      out.push(n.name);
      walk(n.children);
    }
  };
  walk(nodes);
  return out;
}

function childNames(nodes: AgentNode[], parent: string): string[] {
  const walk = (ns: AgentNode[]): string[] | undefined => {
    for (const n of ns) {
      if (n.name === parent) return n.children.map((c) => c.name);
      const hit = walk(n.children);
      if (hit) return hit;
    }
    return undefined;
  };
  return walk(nodes) || [];
}

function cssRule(selector: string): string {
  const re = new RegExp(selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^}]*)\\}');
  return re.exec(css)?.[1] || '';
}

function paintedMark(company: string): { mark: string; d: string; viewBox: string } {
  const { container } = render(createElement(CompanyMark, { company }));
  const svg = container.querySelector('svg.model-icon');
  return {
    mark: svg?.getAttribute('data-mark') || '',
    d: svg?.querySelector('path')?.getAttribute('d') || '',
    viewBox: svg?.getAttribute('viewBox') || '',
  };
}

describeOracle(family('fleet-tree'), () => {
  itOracle('T287', 'fleet badge is company + condensed model prefix', () => {
    const p = modelPrefix({ provider: 'claude', model: 'claude-fable-5' });
    expect(p.company).toBe('anthropic');
    expect(p.label).toBe('F5');
  });

  itOracle('T507', 'Codex maps to the OpenAI/ChatGPT company mark', () => {
    expect(companyOfProvider('codex')).toBe('openai');
    const icon = paintedMark('openai');
    expect(icon.mark).toBe('chatgpt');
    expect(icon.viewBox).toBe('0 0 320 320');
    expect(icon.d.length).toBe(1655);
    expect(createHash('sha256').update(icon.d).digest('hex')).toBe(
      '9499e7547042a397882e0d96308f4795ead582b2e8502e062f57bcbc783ce1bb',
    );
  });

  itOracle('T68', 'RHS shows who-started-whom as a relationship tree', () => {
    const agents: AgentRow[] = [
      { name: 'jv-t1', parent: 'jevons-po' },
      { name: 'jevons-po', parent: 'jevons' },
      { name: 'jevons' },
      { name: 'att-z', parent: 'jevons' },
      { name: 'att-a', parent: 'jevons' },
    ];
    const forest = buildAgentForest(agents);
    expect(forest.map((n) => n.name)).toEqual(['jevons']);
    expect(childNames(forest, 'jevons')).toEqual(['att-a', 'att-z', 'jevons-po']);
    expect(childNames(forest, 'jevons-po')).toEqual(['jv-t1']);
    expect(flattenNames(forest)).toEqual(['jevons', 'att-a', 'att-z', 'jevons-po', 'jv-t1']);
  });

  itOracle('T72', 'RHS reflects the full live agent graph', () => {
    const agents: AgentRow[] = [
      { name: 'jevons' },
      { name: 'jevons-po', parent: 'jevons' },
      { name: 'jv-a', parent: 'jevons-po' },
      { name: 'orphan', parent: 'missing-parent' },
    ];
    const forest = buildAgentForest(agents);
    expect(new Set(flattenNames(forest))).toEqual(new Set(agents.map((a) => a.name)));
    expect(forest.map((n) => n.name).sort()).toEqual(['jevons', 'orphan']);
    const { container } = render(
      createElement(AgentTree, { agents, selected: '', onSelect: () => {} }),
    );
    const painted = [...container.querySelectorAll('.agent-name')].map((el) => el.textContent);
    expect(painted.sort()).toEqual(agents.map((a) => a.name).sort());
  });

  itOracle('T72.1', 'every live fleet agent appears while it exists', () => {
    const live: AgentRow[] = [
      { name: 'jevons' },
      { name: 'jevons-po', parent: 'jevons' },
      { name: 'jv-t72.1', parent: 'jevons-po' },
    ];
    expect(flattenNames(buildAgentForest(live)).sort()).toEqual(['jevons', 'jevons-po', 'jv-t72.1']);
    const after = live.filter((a) => a.name !== 'jv-t72.1');
    expect(flattenNames(buildAgentForest(after))).not.toContain('jv-t72.1');
    expect(flattenNames(buildAgentForest(after)).sort()).toEqual(['jevons', 'jevons-po']);
  });

  itOracle('T115', 'root overseer omits ~/.jevons/jevons; asides use description chrome', () => {
    expect(isStateDirOverseerHome('/Users/marcelo/.jevons/jevons', 'jevons')).toBe(true);
    expect(isStateDirOverseerHome('~/.jevons/jevons', 'jevons')).toBe(true);
    expect(isStateDirOverseerHome('/Users/marcelo/work/github.com/org/repo', 'jevons')).toBe(false);
    expect(isStateDirOverseerHome('/Users/marcelo/.jevons/other', 'jevons')).toBe(false);

    const overseer = {
      name: 'jevons',
      purpose: 'overseer',
      workdir: '/Users/x/.jevons/jevons',
      status: 'running',
    };
    expect(shouldShowPathSecondary(overseer)).toBe(false);
    expect(fleetSecondary(overseer)).toEqual({ kind: '', text: '' });

    const aside = {
      name: 'att-abc',
      purpose: 'aside',
      workdir: '/Users/x/.jevons/threads/att-abc',
      status: 'running',
      progress: 'working · tool',
    };
    expect(shouldShowPathSecondary(aside)).toBe(false);
    expect(fleetSecondary(aside)).toEqual({ kind: '', text: '' });
    expect(shouldShowPathSecondary({ name: 'side', purpose: 'side-chat', workdir: '/r' })).toBe(false);
    expect(shouldShowPathSecondary({ name: 'file', purpose: 'file-target', workdir: '/r' })).toBe(false);
    expect(css).toMatch(/\.agent-node\.agent-aside \.agent-dir \{\s*display:\s*none/);
  });

  itOracle.skip(
    'T285.2',
    'icon menu migrates any agent; ! when its provider is ahead or hot',
    'no provider-menu helper in ui/ — CSS ! bands exist, migrate POST + menu model do not',
  );

  itOracle('T293', 'Grok prefix uses the Grok mark and a condensed version subscript', () => {
    const p = modelPrefix({ provider: 'grok', model: 'grok-4.5-build' });
    expect(p.company).toBe('xai');
    expect(p.initial).toBe('');
    expect(p.version).toBe('4.5');
    expect(p.label).toBe('4.5');
    expect(familyInitial('grok-4.5')).toBe('');
    const icon = paintedMark('xai');
    expect(icon.mark).toBe('grok');
    expect(icon.d.startsWith('M213.235 306.019l178.976-180.002')).toBe(true);
    expect(icon.d).toContain('zm-25.786 22.437');
    expect(companyOfProvider('grok')).toBe('xai');
  });

  itOracle('T295', 'Claude splat + version drops leading zeros', () => {
    expect(versionOf('claude-opus-4-05')).toBe('4.5');
    expect(versionOf('claude-opus-05')).toBe('5');
    expect(versionOf('claude-sonnet-04-05')).toBe('4.5');
    expect(versionOf('claude-opus-000-007')).toBe('0.7');
    expect(versionOf('claude-opus-10')).toBe('10');
    expect(versionOf('claude-opus-4-10')).toBe('4.10');
    expect(modelPrefix({ provider: 'claude', model: 'claude-opus-4-05' }).label).toBe('O4.5');
    expect(paintedMark('anthropic').mark).toBe('claude-splat');
    expect(paintedMark(companyOfProvider('claude')).d.startsWith('m4.7144 15.9555 4.7174-2.6471')).toBe(
      true,
    );
  });

  itOracle('T298', 'model subscript is unambiguous — no letter reads as a digit', () => {
    const ids = [
      'claude-opus-5',
      'claude-opus-05',
      'claude-opus-5[1m]',
      'claude-opus-4-8',
      'claude-opus-4-05',
      'claude-opus-4-5-20250929',
      'claude-sonnet-04-05',
      'claude-haiku-4-5-20251001',
      'us.anthropic.claude-opus-4-5-v1:0',
      'claude-fable-5',
      'claude-fable-05',
      'grok-4.5-build',
      'grok-05',
    ];
    for (const id of ids) {
      const version = versionOf(id);
      expect(version).not.toMatch(/(^|\.)0\d/);
      expect(version).toMatch(/^[\d.]*$/);
      const p = modelPrefix({
        provider: /grok/.test(id) ? 'grok' : 'claude',
        model: id,
      });
      expect(p.label).toBe(p.initial + p.version);
    }
    expect(familyInitial('claude-opus-5')).toBe('O');
    expect(familyInitial('claude-sonnet-5')).toBe('S');
    expect(familyInitial('claude-haiku-5')).toBe('H');
    expect(familyInitial('claude-fable-5')).toBe('F');
    const badges = ['opus', 'sonnet', 'haiku', 'fable'].map(
      (f) => modelPrefix({ provider: 'claude', model: 'claude-' + f + '-4-5' }).label,
    );
    expect(new Set(badges).size).toBe(4);
    const familyRule = cssRule('.agent-node .model-badge sub .model-family');
    expect(familyRule).toMatch(/color:\s*var\(--accent\)/);
    expect(familyRule).toMatch(/font-weight:\s*700/);
    const subRule = cssRule('.agent-node .model-badge sub');
    const familyW = Number(/font-weight:\s*(\d+)/.exec(familyRule)?.[1]);
    const subW = Number(/font-weight:\s*(\d+)/.exec(subRule)?.[1]);
    expect(familyW).toBeGreaterThan(subW);
    expect(treeSrc).toMatch(/className="model-family"/);
    expect(treeSrc).toMatch(/<sub>/);
  });

  itOracle('T299', 'badges use authentic brand SVGs and true sub-baseline', () => {
    const claude = paintedMark('anthropic');
    expect(claude.mark).toBe('claude-splat');
    expect(claude.d.length).toBeGreaterThan(1500);
    expect(claude.d).toContain('.5343-.7042.0546-.3522.4797-.3218');
    expect(markSrc).toMatch(/fill="currentColor"/);
    expect(markSrc).not.toMatch(/<(circle|rect|ellipse)\b/);

    const grok = paintedMark('xai');
    expect(grok.mark).toBe('grok');
    expect(grok.d.length).toBeGreaterThan(600);
    const box = grok.viewBox.trim().split(/\s+/).map(Number);
    expect(box).toHaveLength(4);
    expect(box[2]).toBe(box[3]);
    expect(box[0]).toBeLessThanOrEqual(68.09);
    expect(box[1]).toBeLessThanOrEqual(74.42);

    const marks = ['anthropic', 'xai', 'openai', 'cursor'].map((c) => paintedMark(c).d);
    expect(new Set(marks).size).toBe(4);
    for (const retired of RETIRED_MARKS) {
      for (const d of marks) expect(d).not.toContain(retired);
    }

    const badge = cssRule('.agent-node .model-badge');
    expect(badge).toMatch(/align-items:\s*flex-end/);
    const gap = /(^|[;{\s])gap:\s*([^;]+);/.exec(badge);
    if (gap) expect(gap[2].trim()).toMatch(/^0(px|em|rem)?$/);
    const sub = cssRule('.agent-node .model-badge sub');
    expect(sub).toMatch(/position:\s*relative/);
    const fontPx = Number(/font-size:\s*([\d.]+)px/.exec(sub)?.[1]);
    const bottom = Number(/bottom:\s*(-?[\d.]+)em/.exec(sub)?.[1]);
    const dropPx = -bottom * fontPx;
    expect(dropPx).toBeGreaterThan(1);
    const iconH = Number(/height:\s*([\d.]+)px/.exec(cssRule('.agent-node .model-badge .model-icon'))?.[1]);
    expect(dropPx).toBeLessThan(iconH / 3);
  });

  itOracle('T348', 'Claude badges always show condensed version when model is known', () => {
    expect(modelPrefix({ provider: 'claude', model: 'claude-zephyr-6' }).label).toBe('Z6');
    expect(familyInitial('claude-zephyr-6')).toBe('Z');
    expect(versionOf('claude-zephyr-6')).toBe('6');
    expect(modelPrefix({ provider: 'claude', model: 'us.anthropic.claude-nova-4-5-v1:0' }).label).toBe(
      'N4.5',
    );
    expect(modelPrefix({ provider: 'claude', model: 'claude-2' }).label).toBe('2');
    expect(familyInitial('claude-2')).toBe('');
    expect(modelPrefix({ provider: 'claude', model: 'claude' }).label).toBe('');
    for (const id of [
      'claude-opus-5',
      'claude-sonnet-5',
      'claude-haiku-4-5-20251001',
      'claude-fable-5',
      'claude-fable-5[1m]',
      'claude-zephyr-6',
      'claude-3-5-sonnet-20240620',
      'claude-2',
    ]) {
      expect(modelPrefix({ provider: 'claude', model: id }).label).not.toBe('');
    }
    expect(modelPrefix({ provider: 'claude', model: 'mystery-9' }).label).toBe('');
    expect(familyInitial('grok-4.5')).toBe('');
    expect(familyInitial('gpt-5')).toBe('');
  });

  itOracle('T383', 'fleet-tree selection is sticky across background refresh', () => {
    const queryFn = appSrc.slice(appSrc.indexOf("queryKey: ['agents']"), appSrc.indexOf("queryKey: ['frontier']"));
    expect(queryFn).not.toContain('navigate');
    expect(queryFn).not.toContain('selected');
    expect(appSrc).toContain("selected={fixture ? '' : agent}");
    expect(appSrc).toContain('onSelect={(name) => navigate({ search: { agent: name, tab } })}');
    expect(appSrc).toContain('keepPreviousData');
    expect(appSrc).toContain('mergeAgentChrome');
    expect(appSrc).toContain('onTab={(next) => navigate({ search: { agent, tab: next } })}');
  });

  itOracle('T506', 'model selector is readable and tappable at normal desktop zoom', () => {
    const badge = cssRule('.agent-node .model-badge');
    expect(badge).toMatch(/min-width:\s*32px/);
    expect(badge).toMatch(/min-height:\s*32px/);
    expect(css).toMatch(/\.agent-node \.model-badge:focus-visible\s*\{[^}]*outline:/);
    const sub = cssRule('.agent-node .model-badge sub');
    expect(sub).toMatch(/font-size:\s*12px/);
    expect(badge).toMatch(/color:\s*var\(--text-dim\)/);
  });

  itOracle('T508', 'Bedrock selectors show the Amazon mark before the vendor mark', () => {
    expect(companyOfProvider('bedrock')).toBe('anthropic');
    expect(companyFor('bedrock', 'claude-opus-4-8')).toBe('anthropic');
    const p = modelPrefix({ provider: 'bedrock', model: 'claude-opus-4-8' });
    expect(p.company).toBe('anthropic');
    expect(p.label).toBe('O4.8');
    expect(css).toContain('model-provider-icon[data-mark="amazon-bedrock"]');
    expect(css).toMatch(/sits\s+before the vendor glyph/);
    expect(css).toContain("url('assets/bedrock.svg')");
  });
});
