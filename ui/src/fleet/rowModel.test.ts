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

describe('placeholder rows (🎯T1304)', () => {
  it('a row painted before /api/agents answers claims no status', () => {
    expect(fleetSecondary({ name: 'jevons', placeholder: true })).toEqual({ kind: '', text: '' });
  });
  it('the same row without the placeholder mark still reads stopped', () => {
    expect(fleetSecondary({ name: 'jevons' })).toEqual({ kind: 'status', text: 'stopped' });
    expect(agentDotState({ name: 'jevons' })).toBe('stopped');
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

  it('a worker in a Jevons worktree of its parent repo paints its progress, not the worktree path', () => {
    const parent = '/Users/x/work/github.com/marcelocantos/jevons';
    const wt = '/Users/x/work/github.com/marcelocantos/.jevons-worktrees-jevons/jv-t1237-gate';
    const ctx = { parentWorkdir: parent, hasChildren: false };
    expect(fleetSecondary({ name: 'jv-t1237-gate', parent: 'jevons-po', workdir: wt, status: 'running', phase: 'working', step: 'Bash' }, ctx)).toEqual({
      kind: 'progress',
      text: 'working · Bash',
    });
    // A worktree of some other repo still names where it works.
    const other = '/Users/x/work/github.com/marcelocantos/.jevons-worktrees-claudia/cl-t1';
    expect(fleetSecondary({ name: 'cl-t1', parent: 'jevons-po', workdir: other, status: 'running', phase: 'idle' }, ctx).kind).toBe('path');
  });

  it('a stopped seat with its own repo says stopped, not the path', () => {
    expect(
      fleetSecondary({
        name: 'ge-po',
        parent: 'jevons',
        purpose: 'work',
        workdir: '/Users/x/work/github.com/squz/ge',
        status: 'stopped',
        running: false,
        phase: 'idle',
      }),
    ).toEqual({ kind: 'status', text: 'stopped' });
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

describe('formatFleetProgress provider failure (🎯T845)', () => {
  it('surfaces last_error when status=failed', () => {
    expect(
      formatFleetProgress({
        status: 'failed',
        running: false,
        last_error: 'model is not supported',
        failure_class: 'client_bug',
      }),
    ).toBe('model is not supported');
  });
});
