// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createElement } from 'react';
import { fireEvent, render } from '@testing-library/react';
import { AgentTranscript, ClippedBubble } from '../../components/AgentTranscript';
import { AgentTree } from '../../components/AgentTree';
import { displayRows, userTurnOrigin } from '../../conversation/display';
import { reduceTranscriptBodies } from '../../conversation/stream';
import { assistantProse, assistantTool, userTurn } from '../fixtures';
import { isAsidePurpose } from '../../fleet/rowModel';
import { family } from '../catalog';
import { describeOracle, itOracle } from '../harness';

const root = join(dirname(fileURLToPath(import.meta.url)), '../..');
const app = readFileSync(join(root, 'App.tsx'), 'utf8');
const userReq = readFileSync(join(root, 'components/UserRequest.tsx'), 'utf8');
const css = readFileSync(join(root, 'cockpit.css'), 'utf8');
const transcriptSrc = readFileSync(join(root, 'components/AgentTranscript.tsx'), 'utf8');
const interactionSrc = readFileSync(join(root, 'components/AgentInteraction.tsx'), 'utf8');

function inspectRender(frames: unknown[]) {
  return render(
    createElement(AgentTranscript, {
      name: 'jv-t1-work',
      density: 'compact',
      frames,
      meta: { start: 0, older: 0, total: frames.length },
      ready: true,
    }),
  );
}

const MD_FIXTURE = 'Plan: **bold claim** then\n\n```go\nfunc x() {}\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |';

/** Mount what the RHS inspect mounts: `#agent-inspect-body` (density=compact)
 *  around the same ClippedBubble main uses. jsdom has no ResizeObserver, so the
 *  virtualizer paints no rows; the bubble is rendered directly for the body. */
function renderInspectBody(frame: unknown, sealed: boolean): HTMLElement {
  const shell = render(
    createElement(AgentTranscript, {
      name: 'jv-t1-work',
      density: 'compact',
      frames: [frame],
      meta: { start: 0, older: 0, total: 1 },
      ready: true,
    }),
  );
  expect(shell.container.querySelector('#agent-inspect-body'), 'RHS inspect mounts #agent-inspect-body').toBeTruthy();
  expect(shell.container.querySelector('#messages')).toBeNull();
  const text = ((frame as { message: { content: { text: string }[] } }).message.content[0].text);
  const { container } = render(
    createElement(ClippedBubble, { index: 0, kind: 'assistant', text, sealed, start: 0 }),
  );
  const body = container.querySelector<HTMLElement>('.msg.jevons .msg-body');
  expect(body, 'assistant bubble takes the shared .msg chrome').toBeTruthy();
  return body as HTMLElement;
}

describeOracle(family('aside-sidebar'), () => {
  itOracle('T309.1', 'main and sidebar mount the same AgentInteraction widget', () => {
    const mounts = app.match(/<AgentInteraction\b/g) || [];
    expect(mounts.length).toBeGreaterThanOrEqual(2);
  });

  itOracle('T371', 'aside and main composers share one send/display path', () => {
    expect(userReq).toMatch(/data-composer=\{compact \? 'sidebar' : 'main'\}/);
    expect((app.match(/<AgentInteraction\b/g) || []).length).toBeGreaterThanOrEqual(2);
  });

  itOracle('T372', 'one chat widget + one agent contract; seats differ only by role', () => {
    expect(app).toContain("name=\"jevons\"");
    expect(app).toMatch(/AgentInteraction/);
    expect(app).not.toMatch(/class SidebarComposer|function SidebarComposer/);
  });

  itOracle.skip('T65', 'attention threads live in human↔overseer chat — park/pursue, not a commit', 'journey is the arbiter (J26)');
  itOracle.skip('T95', 'target: prefix is a short-lived aside that files and auto-closes', 'journey is the arbiter (J26)');
  itOracle.skip('T124', 'RHS selection shows that agent’s full transcript out of band from main', 'journey is the arbiter (J25/J26)');
  itOracle.skip('T250', 'asides are not visible in the main transcript', 'journey is the arbiter (J26)');
  itOracle.skip('T251', 'sidebar transcript has its own composer and send UX', 'journey is the arbiter (J26)');
  itOracle.skip('T265', 'aside Transcript is a microcosm of main — same look, no nested sidebar', 'journey is the arbiter (J26)');
  itOracle.skip('T367', 'sidebar messages persist across reload and daemon reboot', 'journey is the arbiter (J26 persist residual)');
  itOracle('T157', 'RHS agent inspect renders a sealed assistant body as full markdown (strong/pre/table)', () => {
    const msg = renderInspectBody(assistantProse(MD_FIXTURE), true);
    expect(msg.querySelector('strong')?.textContent).toBe('bold claim');
    expect(msg.querySelector('pre code')).toBeTruthy();
    expect(msg.querySelector('table td')?.textContent).toBe('1');
  });

  itOracle('T216', 'RHS inspect and main chat paint assistant markdown through one sealed path', () => {
    // One ClippedBubble for both densities: the compact body id is the only fork.
    expect(transcriptSrc).toMatch(/const bodyId = density === 'compact' \? 'agent-inspect-body' : 'messages'/);
    expect(transcriptSrc).not.toMatch(/ai-turn/);
    expect(transcriptSrc.match(/<MarkdownBody text=\{props\.text\}/g)?.length).toBe(1);
    // Both densities map rows through the one <ClippedBubble …> element.
    expect(transcriptSrc.match(/<ClippedBubble\b/g)?.length).toBe(1);
    const inspect = renderInspectBody(assistantProse(MD_FIXTURE), true).innerHTML;
    expect(inspect).toContain('<strong>bold claim</strong>');
    expect(inspect).toContain('<pre>');
  });

  itOracle('T217', 'RHS inspect never dumps raw ** or textContent — sealed and streaming assistant bodies alike', () => {
    for (const sealed of [true, false]) {
      const stop = sealed ? 'end_turn' : null;
      const msg = renderInspectBody(assistantProse(MD_FIXTURE, stop as unknown as string), sealed);
      expect(msg.querySelector('strong'), `stop=${stop}: <strong> painted`).toBeTruthy();
      expect(msg.querySelector('pre'), `stop=${stop}: <pre> painted`).toBeTruthy();
      expect(msg.textContent, `stop=${stop}: no raw **`).not.toContain('**');
      expect(msg.textContent, `stop=${stop}: no raw fence`).not.toContain('```');
    }
  });

  itOracle('T269', 'aside rows carry a hover-gated × that dismisses without selecting', () => {
    expect(isAsidePurpose('aside')).toBe(true);
    expect(isAsidePurpose('File-Target')).toBe(true);
    expect(isAsidePurpose('work')).toBe(false);
    expect(isAsidePurpose(undefined)).toBe(false);
    const selected: string[] = [];
    const dismissed: string[] = [];
    const { container } = render(
      createElement(AgentTree, {
        agents: [
          { name: 'jevons', purpose: 'overseer' },
          { name: 'jevons-po', purpose: 'po', parent: 'jevons' },
          { name: 'jv-t1-work', purpose: 'work', parent: 'jevons-po' },
          { name: 'aside-1', purpose: 'aside', parent: 'jevons' },
        ],
        selected: '',
        onSelect: (n: string) => selected.push(n),
        onDismiss: (n: string) => dismissed.push(n),
      }),
    );
    const xs = [...container.querySelectorAll<HTMLButtonElement>('.agent-dismiss')];
    expect(xs.map((b) => b.dataset.agentDismiss)).toEqual(['aside-1']);
    expect(container.querySelectorAll('.agent-node.agent-aside').length).toBe(1);
    // Hover-only is CSS: hidden until the aside row is hovered/focused.
    const css = readFileSync(join(root, 'cockpit.css'), 'utf8');
    expect(css).toMatch(/\.agent-node \.agent-dismiss \{[^}]*opacity: 0/);
    expect(css).toMatch(/\.agent-node\.agent-aside:hover \.agent-dismiss/);
    fireEvent.click(xs[0]);
    expect(dismissed).toEqual(['aside-1']);
    expect(selected).toEqual([]);
  });

  // ── 🎯T540.3 inspect pocket: T221 / T329 / T167 / T205 ──
  // jsdom has no ResizeObserver, so the virtualizer paints no rows: the shell
  // asserts the mount and ClippedBubble is rendered directly for body paint.

  function bubble(kind: 'user' | 'assistant', text: string, origin?: 'owner' | 'agent'): HTMLElement {
    const { container } = render(createElement(ClippedBubble, { index: 0, kind, text, origin, sealed: true, start: 0 }));
    return container.querySelector<HTMLElement>('.msg') as HTMLElement;
  }

  itOracle('T221', 'inspect paints markdown for <user_query> fleet injects; owner MD-shaped text stays literal', () => {
    const inject = '<user_query>\n**Prefer option 2**\n\n- keep the pin\n- drop the fork\n</user_query>';
    expect(userTurnOrigin(userTurn(inject), inject, true)).toBe('agent');
    expect(userTurnOrigin(userTurn(inject), inject, false), 'main: owner echo is also wrapped (T537.1.2)').toBe('owner');
    expect(userTurnOrigin(userTurn('**owner typed stars**'), '**owner typed stars**', true)).toBe('owner');
    expect(transcriptSrc).toMatch(/displayRows\(props\.frames, \{ inspect: density === 'compact' \}\)/);
    expect(userTurnOrigin({ type: 'user', turn_origin: 'agent', message: { role: 'user', content: 'x' } }, 'x')).toBe('agent');
    const rows = displayRows([userTurn(inject), userTurn('**owner typed stars**')], { inspect: true });
    expect(rows.map((r) => [r.kind, r.origin])).toEqual([['user', 'agent'], ['user', 'owner']]);
    expect(rows[0].text).not.toMatch(/user_query/);
    const a = bubble('user', rows[0].text, rows[0].origin);
    expect(a.querySelector('strong')?.textContent).toBe('Prefer option 2');
    expect(a.querySelectorAll('li').length).toBe(2);
    expect(a.textContent).not.toMatch(/\*\*/);
    const b = bubble('user', rows[1].text, rows[1].origin);
    expect(b.querySelector('strong')).toBeNull();
    expect(b.textContent).toContain('**owner typed stars**');
  });

  itOracle('T329', 'one assistant bubble per owner turn — tool rounds and reminder injects do not fragment it', () => {
    const withSid = (f: unknown) => ({ ...(f as Record<string, unknown>), stream_id: 's1' });
    const bodies = [
      userTurn('do the thing'),
      withSid(assistantProse('Looking…', 'tool_use')),
      withSid(assistantTool('Read', { path: 'a.go' })),
      { type: 'tool_result', content: 'ok' },
      userTurn('<system-reminder>ignore me</system-reminder>'),
      withSid(assistantProse('Found it.', 'tool_use')),
      withSid(assistantTool('Edit', { path: 'a.go' })),
      { type: 'tool_result', content: 'ok' },
      withSid(assistantProse('Done.', 'end_turn')),
    ];
    const rows = displayRows(reduceTranscriptBodies(bodies).frames);
    const assistants = rows.filter((r) => r.kind === 'assistant');
    expect(assistants.length).toBe(1);
    expect(assistants[0].text).toMatch(/Looking…[\s\S]*Found it\.[\s\S]*Done\./);
    expect(rows.filter((r) => r.kind === 'user').length).toBe(1);
  });

  itOracle('T167', 'RHS inspect is a single scroll surface — no nested per-turn scroll regions', () => {
    expect(css).toMatch(/#agent-inspect-body \{[^}]*overflow-y: auto/);
    for (const m of css.matchAll(/#agent-inspect-body\s+[^{]+\{([^}]*)\}/g)) {
      expect(m[1], m[0]).not.toMatch(/overflow(-y)?:\s*(auto|scroll)/);
    }
    // Nothing under a .msg bubble scrolls on its own (the vanilla .worker-body 40vh scroller is gone).
    const rules = css.replace(/\/\*[\s\S]*?\*\//g, '');
    for (const m of rules.matchAll(/(?:^|[\s,}])\.msg(?:[.:\s>][^{]*)?\{([^}]*)\}/gm)) {
      expect(m[1], m[0]).not.toMatch(/overflow-y:\s*(auto|scroll)/);
    }
    const { container } = inspectRender([userTurn('hi'), assistantProse('there')]);
    expect(container.querySelector('#agent-inspect-body')).toBeTruthy();
    expect(container.querySelectorAll('[id$="-body"]').length).toBe(1);
    const msg = bubble('assistant', 'there');
    for (const el of [msg, ...msg.querySelectorAll<HTMLElement>('*')]) {
      expect(el.style.overflowY, el.className).not.toMatch(/auto|scroll/);
    }
  });

  itOracle('T205', 'inspect is main in microcosm — same AgentTranscript, .msg chrome, and follow-pin policy', () => {
    expect(interactionSrc.match(/<AgentTranscript\b/g)?.length).toBe(1);
    expect(transcriptSrc).toMatch(/density === 'compact' \? 'agent-inspect-body' : 'messages'/);
    const main = render(
      createElement(AgentTranscript, { name: 'jevons', density: 'comfortable', frames: [userTurn('hello')], meta: { start: 0, older: 0, total: 1 }, ready: true }),
    );
    expect(main.container.querySelector('#messages')).toBeTruthy();
    expect(inspectRender([userTurn('hello')]).container.querySelector('#agent-inspect-body')).toBeTruthy();
    const msg = bubble('assistant', '**hi** there');
    expect(msg.classList.contains('jevons')).toBe(true);
    expect(msg.querySelector('.msg-body strong')?.textContent).toBe('hi');
    expect(transcriptSrc).toMatch(/from '\.\.\/transcript\/followPin'/);
    expect(transcriptSrc).not.toMatch(/density === 'compact'[^\n]*(followPin|pinWriteScrollTop|distanceFromEnd)/);
  });
});
