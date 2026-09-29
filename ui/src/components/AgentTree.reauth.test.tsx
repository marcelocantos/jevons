// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { AgentTree } from './AgentTree';

describe('fleet authentication recovery', () => {
  afterEach(cleanup);
  // Signing in again is the plan bar's sign-in menu, one entry per backend.
  // A button on every seat of one plan repeated the same action per seat.
  it('offers no per-seat Reauth; the row still says why it stopped', () => {
    render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        agents={[
          { name: 'stopped', running: false, rehydrate: 'broken: invalid_grant', reauth_available: true },
          { name: 'healthy', running: true, reauth_available: true },
        ]}
      />,
    );
    expect(screen.queryByRole('button', { name: /Reauth/ })).toBeNull();
    expect(screen.getByText('broken: invalid_grant')).toBeTruthy();
  });
});
