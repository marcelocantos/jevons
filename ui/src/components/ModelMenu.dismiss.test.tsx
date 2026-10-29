// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ModelMenu } from './ModelMenu';

afterEach(cleanup);

const opts = [{ provider: 'cursor', eligible: true, models: ['composer-2.5'] }];

// The menu closes like a menu: a press outside it, Escape, or the × button.
// It does not close when the owner clicks inside it.
describe('ModelMenu dismissal', () => {
  it('closes on a press outside the box', () => {
    const onClose = vi.fn();
    render(<div><span data-testid="outside">elsewhere</span><span className="model-badge-wrap"><ModelMenu onPick={() => {}} onClose={onClose} options={opts} /></span></div>);
    fireEvent.pointerDown(screen.getByTestId('outside'));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('stays open on a press inside the box', () => {
    const onClose = vi.fn();
    render(<ModelMenu onPick={() => {}} onClose={onClose} options={opts} />);
    fireEvent.pointerDown(screen.getByRole('menuitem', { name: 'composer-2.5' }));
    expect(onClose).not.toHaveBeenCalled();
  });

  it('closes on Escape', () => {
    const onClose = vi.fn();
    render(<ModelMenu onPick={() => {}} onClose={onClose} options={opts} />);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('has a compact × close button labelled Close', () => {
    const onClose = vi.fn();
    render(<ModelMenu onPick={() => {}} onClose={onClose} options={opts} />);
    const btn = screen.getByRole('button', { name: 'Close' });
    expect(btn.textContent).toBe('×');
    fireEvent.click(btn);
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
