// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

import { statusBarText } from '../conversation/overseerPhase';

/** Composer-gap overseer phase (🎯T575). Vanilla glance slot, not top #status. */
export function OverseerPhaseStrip(props: {
  connected: boolean;
  meta: unknown;
}) {
  const text = statusBarText(props.connected, props.meta);
  const busy = props.connected && text !== 'idle';
  return (
    <div
      id="overseer-phase"
      className={'working-indicator' + (busy ? ' busy' : '')}
      role="status"
      aria-live="polite"
    >
      <span id="status-text">{text}</span>
      {busy ? (
        <span className="work-dots" aria-hidden="true">
          <span />
          <span />
          <span />
        </span>
      ) : null}
    </div>
  );
}
