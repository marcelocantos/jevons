// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

export type AgentRow = { name: string; purpose?: string };

export function AgentTree(props: {
  agents: AgentRow[];
  selected: string;
  onSelect: (name: string) => void;
}) {
  return (
    <nav className="agent-tree">
      {props.agents.map((a) => (
        <button
          key={a.name}
          type="button"
          className={a.name === props.selected ? 'agent-tree-row selected' : 'agent-tree-row'}
          onClick={() => props.onSelect(a.name)}
        >
          {a.name}
        </button>
      ))}
    </nav>
  );
}
