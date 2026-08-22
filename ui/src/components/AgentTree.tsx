// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

export type AgentRow = {
  name: string;
  purpose?: string;
  parent?: string;
  status?: string;
};

export type AgentNode = AgentRow & { children: AgentNode[] };

export function buildAgentForest(agents: AgentRow[]): AgentNode[] {
  const byName = new Map<string, AgentNode>();
  for (const a of agents) byName.set(a.name, { ...a, children: [] });
  const roots: AgentNode[] = [];
  for (const n of byName.values()) {
    const p = n.parent && byName.get(n.parent);
    if (p && p.name !== n.name) p.children.push(n);
    else roots.push(n);
  }
  const sort = (xs: AgentNode[]) => {
    xs.sort((a, b) => a.name.localeCompare(b.name));
    xs.forEach((c) => sort(c.children));
  };
  sort(roots);
  return roots;
}

function Row(props: {
  node: AgentNode;
  depth: number;
  selected: string;
  onSelect: (name: string) => void;
}) {
  return (
    <>
      <button
        type="button"
        className={props.node.name === props.selected ? 'agent-tree-row selected' : 'agent-tree-row'}
        style={{ paddingLeft: `${0.75 + props.depth * 0.9}rem` }}
        onClick={() => props.onSelect(props.node.name)}
      >
        {props.node.name}
        {props.node.purpose ? <span className="agent-purpose">{props.node.purpose}</span> : null}
      </button>
      {props.node.children.map((c) => (
        <Row key={c.name} node={c} depth={props.depth + 1} selected={props.selected} onSelect={props.onSelect} />
      ))}
    </>
  );
}

export function AgentTree(props: {
  agents: AgentRow[];
  selected: string;
  onSelect: (name: string) => void;
}) {
  const roots = buildAgentForest(props.agents);
  return (
    <nav className="agent-tree">
      {roots.map((n) => (
        <Row key={n.name} node={n} depth={0} selected={props.selected} onSelect={props.onSelect} />
      ))}
    </nav>
  );
}
