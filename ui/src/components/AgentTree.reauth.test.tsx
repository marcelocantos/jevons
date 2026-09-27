// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AgentTree } from './AgentTree';

describe('fleet authentication recovery', () => {
  afterEach(cleanup);
  it('offers reauth only on the stopped affected seat without selecting it', async () => {
    const onSelect = vi.fn();
    const onReauth = vi.fn(async () => {});
    render(
      <AgentTree
        selected=""
        onSelect={onSelect}
        onReauth={onReauth}
        agents={[
          { name: 'stopped', running: false, rehydrate: 'broken: invalid_grant', reauth_available: true },
          { name: 'healthy', running: true, reauth_available: true },
        ]}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Reauth healthy' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Reauth stopped' }));
    await waitFor(() => expect(onReauth).toHaveBeenCalledOnce());
    expect(onReauth).toHaveBeenCalledWith('stopped');
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('keeps a failed recovery actionable and visible beside the affected row', async () => {
    render(
      <AgentTree
        selected=""
        onSelect={() => {}}
        onReauth={async () => { throw new Error('Sign-in was cancelled; select Reauth to try again'); }}
        agents={[{ name: 'stopped', running: false, rehydrate: 'broken: invalid_grant', reauth_available: true }]}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Reauth stopped' }));
    expect((await screen.findByRole('alert')).textContent).toContain('Sign-in was cancelled');
    expect(screen.getByRole('button', { name: 'Reauth stopped' }).hasAttribute('disabled')).toBe(false);
  });
});
