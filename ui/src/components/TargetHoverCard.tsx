// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

export function TargetHoverCard(props: { markdown: string; visible: boolean; id?: string; name?: string }) {
  if (!props.visible) return null;
  return (
    <aside className="target-hover-card instant-tip instant-tip-card">
      <div className="target-hover-id">{props.id || ''}</div>
      {props.name ? <div>{props.name}</div> : null}
      <div className="target-hover-md" style={{ whiteSpace: 'pre-wrap' }}>
        {props.markdown}
      </div>
    </aside>
  );
}
