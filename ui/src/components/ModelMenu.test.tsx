// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { migrateBody, migrateUrl, ModelMenu } from './ModelMenu';

describe('ModelMenu', () => {
  it('offers an eligible model and refuses an ineligible provider', () => {
    const onPick = vi.fn();
    render(
      <ModelMenu
        onPick={onPick}
        onClose={() => {}}
        options={[
          { provider: 'cursor', eligible: true, models: ['claude-opus-5', 'gpt-6-astra'] },
          { provider: 'grok', eligible: false, reason: 'weekly band is hot', models: ['grok-4.6'] },
        ]}
      />,
    );
    fireEvent.click(screen.getByRole('menuitem', { name: 'claude-opus-5' }));
    expect(onPick).toHaveBeenCalledWith('cursor', 'claude-opus-5');
    expect((screen.getByRole('menuitem', { name: 'grok-4.6' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/weekly band is hot/)).toBeTruthy();
  });

  it('sends the overseer to its own migrate route', () => {
    expect(migrateUrl('overseer')).toBe('/api/overseer/migrate');
    expect(migrateUrl('work')).toBe('/api/agents/migrate');
    expect(migrateBody('jevons', 'overseer', 'grok', 'grok-4.6')).toEqual({
      provider: 'grok',
      model: 'grok-4.6',
    });
    expect(migrateBody('jevons-po', 'work', 'cursor', 'claude-opus-5')).toEqual({
      name: 'jevons-po',
      provider: 'cursor',
      model: 'claude-opus-5',
    });
  });
});
