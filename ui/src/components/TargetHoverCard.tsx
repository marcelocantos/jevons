// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

export function TargetHoverCard(props: { id: string; name: string; visible: boolean }) {
  if (!props.visible) return null;
  return (
    <aside className="target-hover-card">
      <div className="target-hover-id">{props.id}</div>
      <div>{props.name}</div>
    </aside>
  );
}
