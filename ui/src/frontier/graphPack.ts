// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

/** Multi-diagram Frontier Graph payload (vanilla frontier_table.js 🎯T185/T190/T294). */

export const FRONTIER_GRAPH_PACK = 'wrap-grid';
export const GRAPH_DIAGRAM_KIND_COMPONENT = 'component';

export type GraphDiagram = {
  id: string;
  kind: string;
  title: string;
  mermaid: string;
  nodeCount: number;
  edgeCount: number;
};

export type GraphModel = {
  available: boolean;
  mermaid: string;
  diagrams: GraphDiagram[];
  pack: string;
  ledger: string;
  cwd: string;
  nodeCount: number;
  edgeCount: number;
  error: string;
};

export type GraphOpenPlan = {
  mode: 'empty' | 'single' | 'single-primary' | 'pack';
  mermaid: string;
  primary: GraphDiagram | null;
  diagrams: GraphDiagram[];
  diagramCount: number;
  statusNote: string;
};

export function pickPrimaryGraphDiagram(diagrams: GraphDiagram[]): GraphDiagram | null {
  const list = Array.isArray(diagrams) ? diagrams : [];
  let best: GraphDiagram | null = null;
  for (const d of list) {
    if (!d || !d.mermaid) continue;
    if (!best) {
      best = d;
      continue;
    }
    const dn = typeof d.nodeCount === 'number' ? d.nodeCount : 0;
    const bn = typeof best.nodeCount === 'number' ? best.nodeCount : 0;
    if (dn > bn) {
      best = d;
      continue;
    }
    if (dn < bn) continue;
    const de = typeof d.edgeCount === 'number' ? d.edgeCount : 0;
    const be = typeof best.edgeCount === 'number' ? best.edgeCount : 0;
    if (de > be) {
      best = d;
      continue;
    }
    if (de < be) continue;
    const idA = d.id != null ? String(d.id) : '';
    const idB = best.id != null ? String(best.id) : '';
    if (idA && idB && idA < idB) best = d;
  }
  return best;
}

export function resolveFrontierGraphOpenPlan(
  model: GraphModel | null | undefined,
  opts?: { preferPrimary?: boolean },
): GraphOpenPlan {
  const o = opts || {};
  const m = model || ({} as GraphModel);
  const diagrams = Array.isArray(m.diagrams) ? m.diagrams : [];
  const hasDiagrams = diagrams.some((d) => d && d.mermaid);
  const mermaid = m.mermaid != null ? String(m.mermaid) : '';
  if (!m.available || (!mermaid && !hasDiagrams)) {
    return { mode: 'empty', mermaid: '', primary: null, diagrams, diagramCount: diagrams.length, statusNote: '' };
  }
  if (diagrams.length > 1 && o.preferPrimary !== true) {
    return {
      mode: 'pack',
      mermaid,
      primary: pickPrimaryGraphDiagram(diagrams),
      diagrams,
      diagramCount: diagrams.length,
      statusNote: diagrams.length + ' components packed',
    };
  }
  if (diagrams.length === 1 && diagrams[0] && diagrams[0].mermaid) {
    return {
      mode: 'single',
      mermaid: diagrams[0].mermaid,
      primary: diagrams[0],
      diagrams,
      diagramCount: 1,
      statusNote: '',
    };
  }
  if (diagrams.length > 1) {
    const primary = pickPrimaryGraphDiagram(diagrams);
    if (primary && primary.mermaid) {
      return {
        mode: 'single-primary',
        mermaid: primary.mermaid,
        primary,
        diagrams,
        diagramCount: diagrams.length,
        statusNote: 'primary of ' + diagrams.length + ' components',
      };
    }
  }
  if (mermaid && mermaid.indexOf('jevons-frontier-pack') >= 0) {
    return {
      mode: 'empty',
      mermaid: '',
      primary: null,
      diagrams,
      diagramCount: diagrams.length,
      statusNote: 'joined pack source without renderable primary',
    };
  }
  return {
    mode: 'single',
    mermaid,
    primary: null,
    diagrams,
    diagramCount: diagrams.length || (mermaid ? 1 : 0),
    statusNote: '',
  };
}

export function normalizeGraphPayload(payload: unknown, err?: unknown): GraphModel {
  if (err) {
    const message = err instanceof Error ? err.message : String(err);
    return {
      available: false,
      mermaid: '',
      diagrams: [],
      pack: FRONTIER_GRAPH_PACK,
      ledger: '',
      cwd: '',
      nodeCount: 0,
      edgeCount: 0,
      error: message,
    };
  }
  const p = (payload || {}) as Record<string, unknown>;
  const mermaid = p.mermaid != null ? String(p.mermaid) : '';
  const pack = p.pack != null && String(p.pack).trim() ? String(p.pack).trim() : FRONTIER_GRAPH_PACK;
  const diagrams: GraphDiagram[] = [];
  const rawDiagrams = Array.isArray(p.diagrams) ? p.diagrams : [];
  rawDiagrams.forEach((raw, i) => {
    const d = (raw || {}) as Record<string, unknown>;
    const dMermaid = d.mermaid != null ? String(d.mermaid) : '';
    if (!dMermaid) return;
    diagrams.push({
      id: d.id != null ? String(d.id) : 'd' + i,
      kind: d.kind != null ? String(d.kind) : GRAPH_DIAGRAM_KIND_COMPONENT,
      title: d.title != null ? String(d.title) : '',
      mermaid: dMermaid,
      nodeCount: typeof d.node_count === 'number' ? d.node_count : typeof d.nodeCount === 'number' ? d.nodeCount : 0,
      edgeCount: typeof d.edge_count === 'number' ? d.edge_count : typeof d.edgeCount === 'number' ? d.edgeCount : 0,
    });
  });
  if (!diagrams.length && mermaid && mermaid.indexOf('jevons-frontier-pack') < 0) {
    diagrams.push({
      id: 'single',
      kind: GRAPH_DIAGRAM_KIND_COMPONENT,
      title: 'Graph',
      mermaid,
      nodeCount: typeof p.node_count === 'number' ? p.node_count : typeof p.nodeCount === 'number' ? p.nodeCount : 0,
      edgeCount: typeof p.edge_count === 'number' ? p.edge_count : typeof p.edgeCount === 'number' ? p.edgeCount : 0,
    });
  }
  return {
    available: !!p.available,
    mermaid,
    diagrams,
    pack,
    ledger: p.ledger != null ? String(p.ledger) : '',
    cwd: p.cwd != null ? String(p.cwd) : '',
    nodeCount: typeof p.node_count === 'number' ? p.node_count : typeof p.nodeCount === 'number' ? p.nodeCount : 0,
    edgeCount: typeof p.edge_count === 'number' ? p.edge_count : typeof p.edgeCount === 'number' ? p.edgeCount : 0,
    error: p.error != null ? String(p.error) : '',
  };
}
