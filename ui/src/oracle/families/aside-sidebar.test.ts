// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import '../../composer/ensureLocalStorage';
import { createElement } from 'react';
import { expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { render } from '@testing-library/react';
import {
  inspectDisplayUserText,
  isAsideWireUserText,
  parseAsideWireUserText,
  shouldPaintMainUserText,
} from '../../conversation/asideWire';
import { displayRows } from '../../conversation/display';
import { parsePrefixAfterImages } from '../../composer/images';
import { transcriptChannel } from '../../mux/protocol';
import { useDrafts } from '../../store/drafts';
import { UserRequest } from '../../components/UserRequest';
import { normalizeDensity } from '../../density';
import { family } from '../catalog';
import { assistantProse, userTurn } from '../fixtures';
import { describeOracle, itOracle } from '../harness';

const uiSrc = join(dirname(fileURLToPath(import.meta.url)), '../..');
const app = readFileSync(join(uiSrc, 'App.tsx'), 'utf8');
const widget = readFileSync(join(uiSrc, 'components/AgentInteraction.tsx'), 'utf8');
const panel = readFileSync(join(uiSrc, 'components/SidebarPanel.tsx'), 'utf8');
const composer = readFileSync(join(uiSrc, 'components/UserRequest.tsx'), 'utf8');
const conv = readFileSync(join(uiSrc, 'conversation/useConversation.ts'), 'utf8');
const draftsSrc = readFileSync(join(uiSrc, 'store/drafts.ts'), 'utf8');

const ATTENTION_WIRE = '[attention:att-billing|billing nit]\nbilling body';
const TARGET_WIRE =
  '[target-aside: att-file | Chat paste images]\nChat paste images work\n\n(Ceremony: short-lived…)';

function agentInteractionMounts(src: string): string[] {
  return src.match(/<AgentInteraction\b[^/]*\/>/g) || [];
}

describeOracle(family('aside-sidebar'), () => {
  itOracle('T309.1', 'main and sidebar mount the same AgentInteraction widget', () => {
    const mounts = agentInteractionMounts(app);
    expect(mounts.length).toBe(2);
    expect(app).toContain('density="comfortable"');
    expect(app).toContain('density="compact"');
    expect(widget).toContain("comfortable ? 'chat-pane' : 'agent-inspect'");
    expect(normalizeDensity('compact')).toBe('compact');
    expect(normalizeDensity('comfortable')).toBe('comfortable');
  });

  itOracle('T65', 'attention prefixes live in the composer — park/pursue, not a commit', () => {
    expect(parsePrefixAfterImages('aside: billing nit').command).toBe('aside');
    expect(parsePrefixAfterImages('park:').command).toBe('park');
    expect(parsePrefixAfterImages('pursue: First').command).toBe('pursue');
    expect(parsePrefixAfterImages('pursue: First').body).toBe('First');
    expect(widget).toMatch(/id="attention-bar"[\s\S]*hidden/);
    expect(app).not.toMatch(/git\s+commit|commitThreads|attention.*commit/i);
  });

  itOracle('T95', 'target: prefix is a filing aside wire, not a main user bubble', () => {
    const prefix = parsePrefixAfterImages('target: Chat paste images work');
    expect(prefix.command).toBe('target');
    expect(prefix.body).toBe('Chat paste images work');
    const parsed = parseAsideWireUserText(TARGET_WIRE);
    expect(parsed?.kind).toBe('target-aside');
    expect(parsed?.id).toBe('att-file');
    expect(parsed?.displayText).toBe('Chat paste images work');
    expect(parsed?.displayText).not.toMatch(/Ceremony/);
    expect(shouldPaintMainUserText(TARGET_WIRE)).toBe(false);
    const rows = displayRows([userTurn(TARGET_WIRE), assistantProse('filed')]);
    expect(rows.filter((r) => r.kind === 'user')).toEqual([]);
    expect(rows.map((r) => r.kind)).toEqual(['assistant']);
  });

  itOracle('T124', 'RHS selection shows that agent’s transcript out of band from main', () => {
    const mounts = agentInteractionMounts(app);
    expect(mounts[0]).toContain('name="jevons"');
    expect(mounts[1]).toMatch(/name=\{agent\}/);
    expect(mounts[0]).not.toEqual(mounts[1]);
    expect(transcriptChannel('jevons')).toBe('transcript:jevons');
    expect(transcriptChannel('att-billing')).toBe('transcript:att-billing');
    expect(transcriptChannel('jevons')).not.toBe(transcriptChannel('jevons-po'));
    expect(conv).toMatch(/mux\.openTranscript\(name/);
    expect(conv).toMatch(/mux\?\.sendTranscript\(name, t\)/);
  });

  itOracle('T250', 'asides are not visible in the main transcript fold', () => {
    expect(shouldPaintMainUserText(ATTENTION_WIRE)).toBe(false);
    expect(shouldPaintMainUserText(TARGET_WIRE)).toBe(false);
    expect(shouldPaintMainUserText('plain main message')).toBe(true);
    expect(isAsideWireUserText('[attention:att-msftck4l-9sguxj|how does…]')).toBe(true);
    expect(
      shouldPaintMainUserText('[image: d592b0380b1a9e9b]\n[attention:att-x|billing nit]\nbilling body'),
    ).toBe(false);

    const rows = displayRows([
      userTurn('main hello'),
      userTurn(ATTENTION_WIRE),
      assistantProse('main reply'),
      userTurn(TARGET_WIRE),
    ]);
    expect(rows.map((r) => r.kind)).toEqual(['user', 'assistant']);
    expect(rows[0].text).toBe('main hello');
    expect(rows[1].text).toBe('main reply');
    expect(rows.some((r) => /attention:|target-aside:|billing body/.test(r.text))).toBe(false);
  });

  itOracle('T251', 'sidebar transcript has its own composer and send UX', () => {
    expect(panel).toContain("id: 'transcript'");
    expect(app).toContain('sidebarComposerVisible: tab === \'transcript\'');
    expect(widget).toContain('onSend={(t) => conv.send(t)}');
    expect(composer).toContain("compact ? 'agent-inspect-input' : 'input'");
    expect(composer).toContain("compact ? 'agent-inspect-send' : 'send'");
    expect(composer).toContain("compact ? 'agent-inspect-composer' : 'input-bar'");
    expect(composer).toContain('data-composer={props.name === \'jevons\' ? \'main\' : \'sidebar\'}');
  });

  itOracle('T265', 'aside Transcript is a microcosm of main — same look, no nested sidebar', () => {
    expect(widget).toContain('conversation-widget density-');
    expect(widget).not.toMatch(/AgentTree|SidebarPanel/);
    expect(panel).toContain('{props.transcript}');
    expect(panel).not.toMatch(/<AgentTree|<SidebarPanel/);
    expect(inspectDisplayUserText(ATTENTION_WIRE)).toBe('billing body');
    expect(inspectDisplayUserText(TARGET_WIRE)).toBe('Chat paste images work');
    const inspect = displayRows([userTurn('billing body'), assistantProse('aside agent reply')]);
    expect(inspect.map((r) => r.kind)).toEqual(['user', 'assistant']);
    expect(inspect[0].text).toBe('billing body');
  });

  itOracle('T367', 'sidebar drafts persist per agent; transcript reopens by name', () => {
    expect(draftsSrc).toContain('persist');
    expect(draftsSrc).toContain("name: 'jevons-drafts'");
    expect(conv).toMatch(/mux\.openTranscript\(name/);
    const restored = 'sidebar draft after reload';
    useDrafts.getState().setDraft('att-billing', restored);
    expect(localStorage.getItem('jevons-drafts')).toContain(restored);
    const { container, unmount } = render(
      createElement(UserRequest, { name: 'att-billing', density: 'compact', onSend: () => {} }),
    );
    const ta = container.querySelector('#agent-inspect-input') as HTMLTextAreaElement;
    expect(ta.value).toBe(restored);
    expect(useDrafts.getState().drafts.jevons || '').toBe('');
    unmount();
    useDrafts.setState({ drafts: {} });
    localStorage.removeItem('jevons-drafts');
  });

  itOracle('T371', 'aside and main composers share one send/display path', () => {
    expect(widget.match(/onSend=\{\(t\) => conv\.send\(t\)\}/g)?.length).toBe(1);
    expect(conv.match(/sendTranscript\(name, t\)/g)?.length).toBe(1);
    expect(conv.match(/from '\.\/display'/)).toBeTruthy();
    const main = displayRows([userTurn('does this send?'), assistantProse('yes')]);
    const side = displayRows([userTurn('does this send?'), assistantProse('yes')]);
    expect(main.map((r) => r.kind + ':' + r.text)).toEqual(side.map((r) => r.kind + ':' + r.text));
    expect(composer).toContain('props.onSend(payload)');
  });

  itOracle('T372', 'one chat widget + one agent contract; seats differ only by role', () => {
    const mounts = agentInteractionMounts(app);
    expect(mounts.length).toBe(2);
    expect(app.match(/from '\.\/components\/AgentInteraction'/g)?.length).toBe(1);
    expect(mounts[0]).toContain('density="comfortable"');
    expect(mounts[1]).toContain('density="compact"');
    expect(mounts[0]).toContain('name="jevons"');
    expect(mounts[1]).toMatch(/name=\{agent\}/);
    expect(widget).toMatch(/useConversation\(props\.mux, props\.name\)/);
    expect(app).not.toMatch(/ConversationWidget|renderAgentInspect|SidebarTranscript/);
    expect(normalizeDensity('COMPACT')).toBe(normalizeDensity('compact'));
  });
});
