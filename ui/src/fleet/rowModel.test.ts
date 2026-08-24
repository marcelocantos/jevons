// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { describe, expect, it } from 'vitest';
import { agentDotState, fleetSecondary, formatFleetProgress, isBusyAgent } from './rowModel';

describe('formatFleetProgress (🎯T545.4 / T211)', () => {
  it('status=running phase=idle is idle, not busy', () => {
    const a = { status: 'running', phase: 'idle', progress: 'running' };
    expect(formatFleetProgress(a)).toBe('idle');
    expect(isBusyAgent(a)).toBe(false);
  });

  it('phase=working shows working · tool', () => {
    const a = { status: 'running', phase: 'working', step: 'Bash' };
    expect(formatFleetProgress(a)).toBe('working · Bash');
    expect(isBusyAgent(a)).toBe(true);
  });

  it('phase=blocked is blocked, not working', () => {
    const a = { status: 'running', running: true, phase: 'blocked' };
    expect(formatFleetProgress(a)).toBe('blocked');
    expect(isBusyAgent(a)).toBe(false);
    expect(formatFleetProgress({ phase: 'blocked', step: 'waiting on CI' })).toBe('blocked · waiting on CI');
  });

  it('bare status=running with no phase → idle', () => {
    expect(formatFleetProgress({ status: 'running' })).toBe('idle');
    expect(formatFleetProgress({ status: 'stopped' })).toBe('stopped');
  });
});

describe('fleetSecondary (🎯T545.4)', () => {
  it('same-workdir leaf worker paints idle/working/blocked, not the GitHub path', () => {
    const wd = '/Users/x/work/github.com/marcelocantos/jevons';
    const ctx = { parentWorkdir: wd, hasChildren: false };
    expect(fleetSecondary({ name: 'jv-idle', parent: 'jevons-po', workdir: wd, status: 'running', phase: 'idle' }, ctx)).toEqual({
      kind: 'status',
      text: 'idle',
    });
    expect(fleetSecondary({ name: 'jv-work', parent: 'jevons-po', workdir: wd, status: 'running', phase: 'working', step: 'Read' }, ctx)).toEqual({
      kind: 'progress',
      text: 'working · Read',
    });
    expect(fleetSecondary({ name: 'jv-block', parent: 'jevons-po', workdir: wd, status: 'running', phase: 'blocked' }, ctx)).toEqual({
      kind: 'progress',
      text: 'blocked',
    });
  });

  it('overseer home paints stopped/blocked, not idle path chrome', () => {
    const home = '/Users/x/.jevons/jevons';
    expect(
      fleetSecondary({
        name: 'jevons',
        purpose: 'overseer',
        workdir: home,
        status: 'stopped',
        running: false,
        phase: 'idle',
        progress: 'stopped',
      }),
    ).toEqual({ kind: 'status', text: 'stopped' });
    expect(
      fleetSecondary({
        name: 'jevons',
        purpose: 'overseer',
        workdir: home,
        status: 'running',
        running: true,
        phase: 'idle',
      }),
    ).toEqual({ kind: '', text: '' });
  });
});

describe('agentDotState (🎯T545.5)', () => {
  it('uses running=Alive, not a missing status as live', () => {
    expect(agentDotState({ running: false, status: 'running' })).toBe('stopped');
    expect(agentDotState({ running: true, status: 'stopped' })).toBe('running');
    expect(agentDotState({ status: 'dead' })).toBe('stopped');
    expect(agentDotState({})).toBe('stopped');
  });
});
