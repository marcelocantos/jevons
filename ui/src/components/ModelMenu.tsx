// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** One provider row from GET /api/migrate/options (🎯T285.2). */
export type MigrateProvider = {
  provider: string;
  band?: string;
  reason?: string;
  eligible?: boolean;
  models?: string[];
};

export function migrateUrl(purpose: string | undefined): string {
  return purpose === 'overseer' ? '/api/overseer/migrate' : '/api/agents/migrate';
}

export function migrateBody(
  name: string,
  purpose: string | undefined,
  provider: string,
  model: string,
): Record<string, string> {
  if (purpose === 'overseer') return { provider, model };
  return { name, provider, model };
}

/**
 * The fleet badge menu. An ineligible provider stays visible with its
 * reason and cannot be chosen. An empty model list is the provider default.
 */
export function ModelMenu(props: {
  options: MigrateProvider[];
  /** Viewport position. The fleet list scrolls, so an in-flow menu is clipped. */
  anchor?: { top: number; left: number };
  loading?: boolean;
  busy?: boolean;
  error?: string;
  onPick: (provider: string, model: string) => void;
  onClose: () => void;
}) {
  const placed = props.anchor
    ? { position: 'fixed' as const, top: props.anchor.top, left: props.anchor.left }
    : undefined;
  return (
    <div
      className="model-menu"
      role="menu"
      aria-label="Provider and model"
      style={placed}
      onClick={(e) => e.stopPropagation()}
    >
      <button type="button" className="model-menu-close" onClick={props.onClose}>
        Close
      </button>
      {props.error ? <p className="model-menu-error">{props.error}</p> : null}
      {props.loading ? <p className="model-menu-head">Loading…</p> : null}
      {!props.loading && !props.error && props.options.length === 0 ? (
        <p className="model-menu-head">No providers</p>
      ) : null}
      {props.options.map((p) => {
        const models = p.models && p.models.length ? p.models : [''];
        const eligible = p.eligible !== false;
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
                disabled={!eligible || props.busy}
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
