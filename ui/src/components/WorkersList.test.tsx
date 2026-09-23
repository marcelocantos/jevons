// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, render, screen } from '@testing-library/react';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createElement } from 'react';
import { WorkersList, workersLiveLabel } from './WorkersList';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('workers list', () => {
  it('says none yet only when the API list is empty', () => {
    expect(workersLiveLabel([])).toBe('NONE YET');
    expect(workersLiveLabel([{ id: 'a', status: 'completed' }])).toBe('none live');
    expect(workersLiveLabel([{ id: 'a', status: 'running' }])).toBe('1 live');
  });

  it('shows finished workers instead of a permanent none-yet', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [{
        id: '65733b05',
        status: 'completed',
        task: 'run bin/gate show\nand nothing else',
        started_at: '2026-09-12T14:18:38Z',
        outcome: 'GATE green',
      }],
    }));
    render(createElement(WorkersList));
    expect(await screen.findByText('65733b05')).toBeTruthy();
    expect(screen.getByText('none live')).toBeTruthy();
    expect(screen.getByText('run bin/gate show')).toBeTruthy();
    expect(screen.queryByText('NONE YET')).toBeNull();
  });

  it('shows the sentence when a finish line ends on a code fence', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [{
        id: 'abcdef01',
        status: 'completed',
        task: 'run the gates',
        started_at: '2026-09-12T14:18:38Z',
        outcome: "I'll paste the outputs verbatim.```\n```\nGATE green\n```",
      }],
    }));
    render(createElement(WorkersList));
    expect(await screen.findByText("I'll paste the outputs verbatim.")).toBeTruthy();
    expect(screen.queryByText(/```/)).toBeNull();
  });

  it('skips a fence-only first line and shows the prose under it', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [{
        id: 'abcdef02',
        status: 'completed',
        task: 'run the gates',
        started_at: '2026-09-12T14:18:38Z',
        outcome: '```\nGATE green\n```',
      }],
    }));
    render(createElement(WorkersList));
    expect(await screen.findByText('GATE green')).toBeTruthy();
  });

  it('stops a long finish line where the next sentence is glued on', async () => {
    const glued = 'I will confirm the named helpers exist on HEAD.Pulling the target, the commit, and the gate, then writing a much longer finish that runs past the preview limit and keeps going so the line is longer than the preview cap.';
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [{
        id: 'ebf1e021',
        status: 'completed',
        task: 'verify the claim',
        started_at: '2026-09-12T14:18:38Z',
        outcome: glued,
      }],
    }));
    render(createElement(WorkersList));
    expect(await screen.findByText('I will confirm the named helpers exist on HEAD.')).toBeTruthy();
    expect(screen.queryByText(/Pulling/)).toBeNull();
  });

  it('does not let the fleet column squeeze the list down to one row', () => {
    const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../cockpit.css'), 'utf8');
    const block = css.slice(css.indexOf('#workers {'), css.indexOf('.worker-row'));
    expect(block).toMatch(/flex:\s*0 0 auto/);
    expect(block).toMatch(/max-height:\s*220px/);
  });
});
