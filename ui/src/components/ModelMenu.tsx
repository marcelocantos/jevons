// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useRef } from 'react';

/** One provider row from GET /api/migrate/options (🎯T285.2). */
export type ProviderOption = {
  provider: string;
  band?: string;
  reason?: string;
  eligible?: boolean;
  models?: string[];
  /** False when another model of this provider would hit the same wall. */
  model_pick?: boolean;
};

/**
 * Every seat, the overseer included, switches provider or model in place
 * through one route (claudia 🎯T195): it keeps its session and conversation.
 */
export const SWITCH_MODEL_URL = '/api/agents/switch-model';

export function switchModelBody(name: string, provider: string, model: string): Record<string, string> {
  return { name, provider, model };
}

/**
 * The fleet badge menu. An ineligible provider stays visible with its
 * reason and cannot be chosen. An empty model list is the provider default.
 */
export function ModelMenu(props: {
  options: ProviderOption[];
  /** Viewport position. The fleet list scrolls, so an in-flow menu is clipped. */
  anchor?: { top: number; left: number };
  /** The seat's provider. A model pick there is a pin, not a move onto that provider. */
  currentProvider?: string;
  loading?: boolean;
  busy?: boolean;
  error?: string;
  onPick: (provider: string, model: string) => void;
  onClose: () => void;
}) {
  const placed = props.anchor
    ? { position: 'fixed' as const, top: props.anchor.top, left: props.anchor.left }
    : undefined;
  const here = (props.currentProvider || '').trim().toLowerCase();
  const ref = useRef<HTMLDivElement>(null);
  const onClose = props.onClose;
  // A menu closes the way menus do: Escape, or a press anywhere outside it.
  // The labelled Close button alone read as one more menu row (2026-10-22).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    const onDown = (e: PointerEvent) => {
      // The opener lives beside the menu (the model badge) and toggles it
      // itself; a press there is not "outside".
      const scope = ref.current?.parentElement ?? ref.current;
      if (scope && !scope.contains(e.target as Node)) onClose();
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('pointerdown', onDown);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('pointerdown', onDown);
    };
  }, [onClose]);
  return (
    <div
      ref={ref}
      className="model-menu"
      role="menu"
      aria-label="Provider and model"
      style={placed}
      onClick={(e) => e.stopPropagation()}
    >
      <button type="button" className="model-menu-close" aria-label="Close" title="Close (Esc)" onClick={props.onClose}>
        ×
      </button>
      {props.error ? <p className="model-menu-error">{props.error}</p> : null}
      {props.loading ? <p className="model-menu-head">Loading…</p> : null}
      {!props.loading && !props.error && props.options.length === 0 ? (
        <p className="model-menu-head">No providers</p>
      ) : null}
      {props.options.map((p) => {
        const models = p.models && p.models.length ? p.models : [''];
        const eligible = p.eligible !== false;
        // Ahead/hot blocks a migration onto that provider. It does not
        // block choosing another model of the provider the seat is already on,
        // unless that pick would meet the same refusal.
        const sameProvider = p.provider.trim().toLowerCase() === here && p.model_pick !== false;
        const canPick = p.model_pick !== false && (eligible || sameProvider) && !props.busy;
        return (
          <div key={p.provider} className="model-menu-provider">
            <div className="model-menu-head">
              {p.provider}
              {eligible ? '' : ' — ' + (p.reason || 'not available')}
            </div>
            {models.map((m) => (
              <button
                key={p.provider + ':' + m}
                type="button"
                role="menuitem"
                disabled={!canPick}
                onClick={() => props.onPick(p.provider, m)}
              >
                {m || 'provider default'}
              </button>
            ))}
          </div>
        );
      })}
    </div>
  );
}
