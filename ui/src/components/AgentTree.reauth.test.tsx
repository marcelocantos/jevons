// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { AgentTree } from './AgentTree';

describe('fleet authentication recovery', () => {
  afterEach(cleanup);
  it('shows the Claudia reauth error when recovery fails', async () => {
    render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        onReauth={async () => {
          throw new Error('Claudia could not recover authentication: sign-in was canceled');
        }}
        agents={[
          { name: 'stopped', running: false, rehydrate: 'broken: invalid_grant', reauth_available: true },
        ]}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Reauth stopped' }));
    await waitFor(() => {
      expect(screen.getByRole('alert').textContent).toContain('sign-in was canceled');
    });
  });

  it('offers per-seat Reauth on a stopped plan-auth seat and shows the broker error', () => {
    render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        onReauth={async () => {}}
        agents={[
          { name: 'stopped', running: false, rehydrate: 'broken: invalid_grant', reauth_available: true },
          { name: 'healthy', running: true, reauth_available: true },
        ]}
      />,
    );
    expect(screen.getByRole('button', { name: 'Reauth stopped' })).toBeTruthy();
    expect(screen.getByText('broken: invalid_grant')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Reauth healthy/ })).toBeNull();
  });
});
