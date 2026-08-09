// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// RHS bullseye frontier table helpers (🎯T131). DOM-free pure logic for
// hermetic tests: payload → rows, tab model, calm empty state. Ledger path
// always comes from the server/bullseye discovery response — never invented
// client-side as a fixed bullseye.yaml string.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.FrontierTable = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  var TAB_TRANSCRIPT = 'transcript';
  var TAB_FRONTIER = 'frontier';
  var POLL_MS = 8000;

  // nextBottomTab(prev, click) — tab ids only; unknown → frontier.
  function nextBottomTab(prev, click) {
    var t = String(click || '').toLowerCase();
    if (t === TAB_TRANSCRIPT || t === TAB_FRONTIER) return t;
    return prev === TAB_TRANSCRIPT ? TAB_TRANSCRIPT : TAB_FRONTIER;
  }

  // 🎯T199: natural/version compare for bullseye-style target ids.
  // Digit runs compare as integers; non-digit runs byte-wise.
  // Example: T1 < T1.1 < T2 < T10 < T10.2 < T10.10 < T27 < T27.3 < T100.
  // Mirrors server targetIDLess / naturalLess.
  function isDigitChar(c) {
    return c >= '0' && c <= '9';
  }

  function cmpDigitRun(a, b) {
    var sa = 0;
    var sb = 0;
    while (sa < a.length - 1 && a.charAt(sa) === '0') sa++;
    while (sb < b.length - 1 && b.charAt(sb) === '0') sb++;
    var da = a.slice(sa);
    var db = b.slice(sb);
    if (da.length !== db.length) return da.length < db.length ? -1 : 1;
    if (da < db) return -1;
    if (da > db) return 1;
    if (a.length !== b.length) return a.length < b.length ? -1 : 1;
    return 0;
  }

  // targetIDLess(a, b) — true when a should sort before b (natural order).
  function targetIDLess(a, b) {
    return targetIDCompare(a, b) < 0;
  }

  // targetIDCompare(a, b) → -1 | 0 | 1 for Array.sort comparators.
  function targetIDCompare(a, b) {
    a = a == null ? '' : String(a);
    b = b == null ? '' : String(b);
    var ia = 0;
    var ib = 0;
    while (ia < a.length && ib < b.length) {
      var aDig = isDigitChar(a.charAt(ia));
      var bDig = isDigitChar(b.charAt(ib));
      if (aDig && bDig) {
        var ea = ia;
        var eb = ib;
        while (ea < a.length && isDigitChar(a.charAt(ea))) ea++;
        while (eb < b.length && isDigitChar(b.charAt(eb))) eb++;
        var c = cmpDigitRun(a.slice(ia, ea), b.slice(ib, eb));
        if (c !== 0) return c;
        ia = ea;
        ib = eb;
        continue;
      }
      if (aDig !== bDig) {
        return a.charAt(ia) < b.charAt(ib) ? -1 : 1;
      }
      if (a.charAt(ia) !== b.charAt(ib)) {
        return a.charAt(ia) < b.charAt(ib) ? -1 : 1;
      }
      ia++;
      ib++;
    }
    var ra = a.length - ia;
    var rb = b.length - ib;
    if (ra < rb) return -1;
    if (ra > rb) return 1;
    return 0;
  }

  // When a fleet agent is selected, product switches to transcript tab.
  // When selection clears, stay on current tab (usually frontier remains).
  function tabAfterAgentSelect(hasSelection) {
    return hasSelection ? TAB_TRANSCRIPT : TAB_FRONTIER;
  }

  function normalizeStringList(raw) {
    if (!Array.isArray(raw)) return [];
    var out = [];
    for (var i = 0; i < raw.length; i++) {
      var s = raw[i] == null ? '' : String(raw[i]).trim();
      if (s) out.push(s);
    }
    return out;
  }

  // normalizeNumber — finite number or undefined (omit empty zeros only if never set).
  function normalizeNumber(raw) {
    if (typeof raw === 'number' && isFinite(raw)) return raw;
    if (raw == null || raw === '') return undefined;
    var n = Number(raw);
    return isFinite(n) ? n : undefined;
  }

  // normalizeExtra — object of string key → string value (unknown ledger fields).
  function normalizeExtra(raw) {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {};
    var out = {};
    Object.keys(raw).forEach(function (k) {
      var key = String(k || '').trim();
      if (!key) return;
      var v = raw[k];
      if (v == null) return;
      var s = typeof v === 'string' ? v.trim() : String(v).trim();
      if (s) out[key] = s;
    });
    return out;
  }

  // normalizePayload(apiJSON|err) → { available, ledger, cwd, rows, empty, error }
  function normalizePayload(payload, err) {
    if (err) {
      return {
        available: false,
        ledger: '',
        cwd: '',
        rows: [],
        empty: true,
        error: String(err.message || err),
      };
    }
    var p = payload || {};
    var targets = Array.isArray(p.targets) ? p.targets : [];
    var rows = targets.map(function (t) {
      if (!t) return null;
      var deps = normalizeDependents(t.dependents);
      var dependsOn = normalizeDependents(t.depends_on != null ? t.depends_on : t.dependsOn);
      var fanout = typeof t.fanout === 'number' ? t.fanout : (parseInt(t.fanout, 10) || 0);
      // Prefer dependents length when present (API Fanout == len(Dependents)).
      if (deps.length > 0 || Array.isArray(t.dependents)) {
        fanout = deps.length;
      }
      return {
        id: String(t.id || ''),
        name: String(t.name || ''),
        status: String(t.status || ''),
        fanout: fanout,
        dependents: deps,
        depends_on: dependsOn,
        value: normalizeNumber(t.value),
        cost: normalizeNumber(t.cost),
        actual_cost: normalizeNumber(t.actual_cost != null ? t.actual_cost : t.actualCost),
        acceptance: normalizeStringList(t.acceptance),
        context: t.context != null ? String(t.context).trim() : '',
        tags: normalizeStringList(t.tags),
        attestation: t.attestation != null ? String(t.attestation).trim() : '',
        origin: t.origin != null ? String(t.origin).trim() : '',
        discovered: t.discovered != null ? String(t.discovered).trim() : '',
        achieved: t.achieved != null ? String(t.achieved).trim() : '',
        extra: normalizeExtra(t.extra),
      };
    }).filter(function (r) {
      return r && r.id;
    });
    var available = !!p.available;
    var error = p.error ? String(p.error) : '';
    return {
      available: available,
      ledger: p.ledger ? String(p.ledger) : '',
      cwd: p.cwd ? String(p.cwd) : '',
      rows: rows,
      empty: !available || rows.length === 0,
      error: error,
      updatedAt: p.updated_at ? String(p.updated_at) : '',
    };
  }

  // Ogham feather mark (U+169B) — fanout glyph (🎯T173).
  var FANOUT_MARK = '\u169B';

  // Canonical status → short code + display title (🎯T173).
  // Keys are lowercased with underscores/spaces stripped for lookup.
  var STATUS_ABBR = {
    identified: { abbr: 'Id', title: 'Identified' },
    converging: { abbr: 'Cv', title: 'Converging' },
    achieved: { abbr: 'Ac', title: 'Achieved' },
    setaside: { abbr: 'Sa', title: 'Set aside' },
    set_aside: { abbr: 'Sa', title: 'Set aside' },
    postponed: { abbr: 'Pp', title: 'Postponed' },
    assigned: { abbr: 'As', title: 'Assigned' },
  };

  function statusKey(status) {
    return String(status || '').trim().toLowerCase().replace(/[\s-]+/g, '_');
  }

  // formatStatus — abbreviated code for cell text (🎯T173). Empty → em dash.
  function formatStatus(status) {
    var s = String(status || '').trim();
    if (!s) return '—';
    var key = statusKey(s);
    var hit = STATUS_ABBR[key] || STATUS_ABBR[key.replace(/_/g, '')];
    if (hit) return hit.abbr;
    // Fallback: first letters of CamelCase / snake words, max 2–3 chars.
    var parts = s.replace(/_/g, ' ').split(/(?=[A-Z])|[\s-]+/).filter(Boolean);
    if (parts.length >= 2) {
      return parts.map(function (p) { return p.charAt(0).toUpperCase(); }).join('').slice(0, 3);
    }
    return s.slice(0, 2).charAt(0).toUpperCase() + s.slice(1, 2).toLowerCase();
  }

  // statusTitle — full status string for hover/title.
  function statusTitle(status) {
    var s = String(status || '').trim();
    if (!s) return '';
    var key = statusKey(s);
    var hit = STATUS_ABBR[key] || STATUS_ABBR[key.replace(/_/g, '')];
    if (hit) return hit.title;
    // Pretty-print unknown: snake → words, capitalize.
    return s.replace(/_/g, ' ').replace(/\b\w/g, function (c) { return c.toUpperCase(); });
  }

  // normalizeDependents — API objects {id,name}, bare strings, or parallel fields.
  function normalizeDependents(raw) {
    if (!Array.isArray(raw)) return [];
    var out = [];
    for (var i = 0; i < raw.length; i++) {
      var d = raw[i];
      if (d == null) continue;
      if (typeof d === 'string') {
        var sid = String(d).trim();
        if (sid) out.push({ id: sid, name: '' });
        continue;
      }
      var id = String(d.id || d.ID || '').trim();
      if (!id) continue;
      out.push({ id: id, name: String(d.name || d.Name || '').trim() });
    }
    return out;
  }

  // formatFanout(n, id, dependents?) → { text, title, visible }.
  // Hide when 0 (🎯T173). Nonzero: "N᚛" + InstantTip lead count + bullets
  // "• TID Name" (space separator, not em-dash — 🎯T179 owner pin).
  function formatFanout(n, id, dependents) {
    var deps = normalizeDependents(dependents);
    var count = deps.length > 0
      ? deps.length
      : (typeof n === 'number' ? n : (parseInt(n, 10) || 0));
    if (count <= 0) {
      return { text: '', title: '', visible: false };
    }
    var tid = String(id || '').trim() || '?';
    var text = String(count) + FANOUT_MARK;
    var lead = count === 1
      ? ('1 target depends on ' + tid)
      : (String(count) + ' targets depend on ' + tid);
    var title = lead;
    if (deps.length > 0) {
      var lines = [lead];
      for (var i = 0; i < deps.length; i++) {
        var d = deps[i];
        var bullet = '• ' + d.id;
        if (d.name) bullet += ' ' + d.name;
        lines.push(bullet);
      }
      title = lines.join('\n');
    }
    return { text: text, title: title, visible: true };
  }

  // shortName truncates long target names for the compact table.
  function shortName(name, maxLen) {
    var n = String(name || '');
    var max = maxLen || 48;
    if (n.length <= max) return n;
    return n.slice(0, max - 1) + '…';
  }

  // 🎯T332: recommended .ft-id width in ch from the longest id (clamped).
  // Hierarchical pins (T254.1, T262.3) are 6ch; floor keeps short ids stable under
  // table-layout:fixed; ceiling avoids the rem-sized id↔name chasm.
  function maxIdChWidth(ids, minCh, maxCh) {
    var lo = typeof minCh === 'number' && isFinite(minCh) ? minCh : 4;
    var hi = typeof maxCh === 'number' && isFinite(maxCh) ? maxCh : 9;
    if (lo < 1) lo = 1;
    if (hi < lo) hi = lo;
    var n = lo;
    if (Array.isArray(ids)) {
      for (var i = 0; i < ids.length; i++) {
        var len = String(ids[i] == null ? '' : ids[i]).length;
        if (len > n) n = len;
      }
    }
    if (n > hi) n = hi;
    if (n < lo) n = lo;
    return n;
  }

  // emptyMessage for calm unavailable / zero-frontier states.
  function emptyMessage(model) {
    if (!model) return 'Frontier unavailable.';
    if (model.error && !model.available) return model.error;
    if (model.available && model.rows.length === 0) return 'Frontier is empty — no ready targets.';
    if (!model.available) return 'No bullseye ledger for this workdir.';
    return '';
  }

  // mermaidNodeId — safe mermaid identifier from a target id (T27.1 → T27_1).
  function mermaidNodeId(id) {
    var s = String(id || '').trim();
    if (!s) return 'node';
    var safe = s.replace(/[^A-Za-z0-9_]/g, '_');
    if (!/^[A-Za-z_]/.test(safe)) safe = 'n_' + safe;
    return safe;
  }

  // mermaidLabel — short quoted label; escape " for mermaid node text.
  function mermaidLabel(id, name) {
    var label = String(id || '').trim() || '?';
    var n = name != null ? String(name).trim() : '';
    if (n) {
      // Keep graph readable: id + truncated name.
      if (n.length > 28) n = n.slice(0, 27) + '…';
      label = label + ' · ' + n;
    }
    return label.replace(/"/g, "'").replace(/\n/g, ' ');
  }

  // 🎯T190: Mermaid layout knobs per component diagram; panel packs blocks.
  // 🎯T274: cap nodes/diagram — browser Mermaid hangs on 60+ node single graphs
  // (orthograph external ledger was one 64-node component → empty viewer).
  var MERMAID_NODE_SPACING = 28;
  var MERMAID_RANK_SPACING = 36;
  var MERMAID_WRAPPING_WIDTH = 180;
  var MERMAID_MAX_NODES_PER_DIAGRAM = 24;
  var FRONTIER_GRAPH_PACK = 'wrap-grid';
  var GRAPH_DIAGRAM_KIND_COMPONENT = 'component';
  var GRAPH_DIAGRAM_KIND_ORPHANS = 'orphans';

  // mermaidActiveGraphHeader — init + flowchart TB for one component (🎯T185 + 🎯T190).
  function mermaidActiveGraphHeader() {
    return (
      "%%{init: {'flowchart': {'useMaxWidth': true, 'nodeSpacing': " +
      MERMAID_NODE_SPACING +
      ", 'rankSpacing': " +
      MERMAID_RANK_SPACING +
      ", 'wrappingWidth': " +
      MERMAID_WRAPPING_WIDTH +
      "}}}%%\nflowchart TB"
    );
  }

  // packActiveGraphIslands — connected-component partition (🎯T190).
  // Returns array of islands (each = sorted id list); islands ordered by first id.
  function packActiveGraphIslands(ids, undirectedPairs) {
    var list = Array.isArray(ids) ? ids.slice() : [];
    if (!list.length) return [];
    var adj = {};
    var i;
    for (i = 0; i < list.length; i++) adj[list[i]] = [];
    var pairs = Array.isArray(undirectedPairs) ? undirectedPairs : [];
    for (i = 0; i < pairs.length; i++) {
      var p = pairs[i];
      if (!p || p.from == null || p.to == null) continue;
      var a = String(p.from);
      var b = String(p.to);
      if (!adj[a] || !adj[b]) continue;
      adj[a].push(b);
      adj[b].push(a);
    }
    var visited = {};
    var islands = [];
    for (i = 0; i < list.length; i++) {
      var start = list[i];
      if (visited[start]) continue;
      var queue = [start];
      visited[start] = true;
      var comp = [];
      while (queue.length) {
        var cur = queue.shift();
        comp.push(cur);
        var nbs = adj[cur] || [];
        for (var k = 0; k < nbs.length; k++) {
          if (visited[nbs[k]]) continue;
          visited[nbs[k]] = true;
          queue.push(nbs[k]);
        }
      }
      comp.sort(targetIDCompare);
      islands.push(comp);
    }
    islands.sort(function (x, y) {
      return targetIDCompare(x[0] || '', y[0] || '');
    });
    return islands;
  }

  // splitOrphanComponents — size-1 islands → orphans list (🎯T190).
  function splitOrphanComponents(islands) {
    var connected = [];
    var orphans = [];
    var list = Array.isArray(islands) ? islands : [];
    for (var i = 0; i < list.length; i++) {
      var island = list[i];
      if (!island || !island.length) continue;
      if (island.length === 1) orphans.push(island[0]);
      else connected.push(island);
    }
    orphans.sort(targetIDCompare);
    return { connected: connected, orphans: orphans };
  }

  // targetRootFamily — T1.2.3 → T1 (🎯T274 hierarchical re-split).
  function targetRootFamily(id) {
    var s = String(id == null ? '' : id).trim();
    if (!s) return s;
    var dot = s.indexOf('.');
    return dot >= 0 ? s.slice(0, dot) : s;
  }

  // chunkIDList — hard slices of maxNodes (🎯T274).
  function chunkIDList(ids, maxNodes) {
    var max = maxNodes > 0 ? maxNodes : MERMAID_MAX_NODES_PER_DIAGRAM;
    var list = Array.isArray(ids) ? ids : [];
    if (!list.length) return [];
    if (list.length <= max) return [list.slice()];
    var out = [];
    for (var i = 0; i < list.length; i += max) {
      out.push(list.slice(i, i + max));
    }
    return out;
  }

  // splitOversizedComponents — re-partition components > maxNodes (🎯T274).
  // Prefer hierarchical root-family groups packed into bins ≤ max; hard-chunk
  // if a single family is still huge (avoids one diagram per tiny root).
  function splitOversizedComponents(connected, maxNodes) {
    var max = maxNodes > 0 ? maxNodes : MERMAID_MAX_NODES_PER_DIAGRAM;
    var comps = Array.isArray(connected) ? connected : [];
    var out = [];
    for (var c = 0; c < comps.length; c++) {
      var comp = comps[c];
      if (!comp || !comp.length) continue;
      if (comp.length <= max) {
        out.push(comp.slice());
        continue;
      }
      var groups = {};
      var roots = [];
      for (var i = 0; i < comp.length; i++) {
        var id = comp[i];
        var root = targetRootFamily(id);
        if (!Object.prototype.hasOwnProperty.call(groups, root)) {
          groups[root] = [];
          roots.push(root);
        }
        groups[root].push(id);
      }
      roots.sort(targetIDCompare);
      var bin = [];
      function flush() {
        if (!bin.length) return;
        out.push(bin);
        bin = [];
      }
      for (var r = 0; r < roots.length; r++) {
        var ids = groups[roots[r]].slice().sort(targetIDCompare);
        if (ids.length > max) {
          flush();
          var chunks = chunkIDList(ids, max);
          for (var k = 0; k < chunks.length; k++) out.push(chunks[k]);
          continue;
        }
        if (bin.length + ids.length > max) flush();
        bin = bin.concat(ids);
      }
      flush();
    }
    return out;
  }

  // emitMermaidForNodes — one flowchart TB for a node set + among-set edges.
  function emitMermaidForNodes(ids, byId, rawEdges) {
    var lines = [mermaidActiveGraphHeader()];
    var set = {};
    var i;
    for (i = 0; i < ids.length; i++) {
      set[ids[i]] = true;
      var row = byId && byId[ids[i]];
      var name = row && row.name != null ? String(row.name).trim() : '';
      lines.push('  ' + mermaidNodeId(ids[i]) + '["' + mermaidLabel(ids[i], name) + '"]');
    }
    var edgeCount = 0;
    var edges = Array.isArray(rawEdges) ? rawEdges : [];
    for (i = 0; i < edges.length; i++) {
      var e = edges[i];
      if (!set[e.from] || !set[e.to]) continue;
      lines.push(
        '  ' + mermaidNodeId(e.from) + ' -.->|needs| ' + mermaidNodeId(e.to)
      );
      edgeCount++;
    }
    return { mermaid: lines.join('\n') + '\n', edgeCount: edgeCount };
  }

  // packActiveGraphDiagrams — multi-diagram blocks, largest first, orphans last.
  function packActiveGraphDiagrams(connected, orphans, byId, rawEdges) {
    var ranked = [];
    var i;
    var comps = Array.isArray(connected) ? connected : [];
    for (i = 0; i < comps.length; i++) {
      var ids = comps[i];
      if (!ids || !ids.length) continue;
      var set = {};
      for (var n = 0; n < ids.length; n++) set[ids[n]] = true;
      var ec = 0;
      var edges = Array.isArray(rawEdges) ? rawEdges : [];
      for (var e = 0; e < edges.length; e++) {
        if (set[edges[e].from] && set[edges[e].to]) ec++;
      }
      ranked.push({ ids: ids, nodes: ids.length, edgeCount: ec, firstID: ids[0] });
    }
    ranked.sort(function (a, b) {
      if (a.edgeCount !== b.edgeCount) return b.edgeCount - a.edgeCount;
      if (a.nodes !== b.nodes) return b.nodes - a.nodes;
      return targetIDCompare(a.firstID, b.firstID);
    });
    var blocks = [];
    for (i = 0; i < ranked.length; i++) {
      var r = ranked[i];
      var emitted = emitMermaidForNodes(r.ids, byId, rawEdges);
      blocks.push({
        id: 'c' + i,
        kind: GRAPH_DIAGRAM_KIND_COMPONENT,
        title: 'Component (' + r.nodes + ' nodes)',
        mermaid: emitted.mermaid,
        nodeCount: r.nodes,
        edgeCount: emitted.edgeCount,
      });
    }
    // 🎯T274: cap orphan strips the same way (large orphan list also hangs Mermaid).
    var orph = Array.isArray(orphans) ? orphans : [];
    if (orph.length) {
      var ochunks = chunkIDList(orph, MERMAID_MAX_NODES_PER_DIAGRAM);
      for (var oi = 0; oi < ochunks.length; oi++) {
        var chunk = ochunks[oi];
        var oe = emitMermaidForNodes(chunk, byId, rawEdges);
        blocks.push({
          id: ochunks.length > 1 ? ('orphans_' + oi) : 'orphans',
          kind: GRAPH_DIAGRAM_KIND_ORPHANS,
          title: ochunks.length > 1
            ? ('Orphans part ' + (oi + 1) + ' (' + chunk.length + ')')
            : ('Orphans (' + chunk.length + ')'),
          mermaid: oe.mermaid,
          nodeCount: chunk.length,
          edgeCount: oe.edgeCount,
        });
      }
    }
    return blocks;
  }

  function joinGraphDiagramSources(pack, blocks) {
    var list = Array.isArray(blocks) ? blocks : [];
    var lines = ['%% jevons-frontier-pack pack=' + pack + ' diagrams=' + list.length + ' %%'];
    for (var i = 0; i < list.length; i++) {
      var d = list[i];
      lines.push(
        '%% --- diagram ' + i + ' id=' + d.id + ' kind=' + d.kind + ' --- %%'
      );
      lines.push(String(d.mermaid || '').replace(/\s+$/, ''));
    }
    return lines.join('\n') + '\n';
  }

  // buildActiveDependencyDiagrams(targets) — multi-diagram pack (🎯T190 option 2).
  // Each connected component is its own Mermaid diagram; size-1 isolates share
  // one orphans diagram; pack=wrap-grid for panel block layout.
  function buildActiveDependencyDiagrams(targets) {
    var list = Array.isArray(targets) ? targets : [];
    var byId = {};
    var ids = [];
    var i;
    for (i = 0; i < list.length; i++) {
      var t = list[i];
      if (!t || t.id == null) continue;
      var id = String(t.id).trim();
      if (!id || byId[id]) continue;
      byId[id] = t;
      ids.push(id);
    }
    ids.sort(targetIDCompare);
    var edgeKeys = {};
    var rawEdges = [];
    for (i = 0; i < ids.length; i++) {
      var fromId = ids[i];
      var deps = normalizeDependents(
        byId[fromId].depends_on != null
          ? byId[fromId].depends_on
          : byId[fromId].dependsOn
      );
      for (var j = 0; j < deps.length; j++) {
        var depId = deps[j].id;
        if (!byId[depId]) continue;
        var ek = fromId + '\0' + depId;
        if (edgeKeys[ek]) continue;
        edgeKeys[ek] = true;
        rawEdges.push({ from: fromId, to: depId });
      }
    }
    rawEdges.sort(function (a, b) {
      var cf = targetIDCompare(a.from, b.from);
      if (cf !== 0) return cf;
      return targetIDCompare(a.to, b.to);
    });

    var islands = packActiveGraphIslands(ids, rawEdges);
    var split = splitOrphanComponents(islands);
    // 🎯T274: re-split oversized components before emit (external large ledgers).
    var connected = splitOversizedComponents(
      split.connected,
      MERMAID_MAX_NODES_PER_DIAGRAM
    );
    var diagrams = packActiveGraphDiagrams(
      connected,
      split.orphans,
      byId,
      rawEdges
    );
    if (!diagrams.length) {
      diagrams = [{
        id: 'empty',
        kind: GRAPH_DIAGRAM_KIND_COMPONENT,
        title: 'Empty',
        mermaid: mermaidActiveGraphHeader() + '\n',
        nodeCount: 0,
        edgeCount: 0,
      }];
    }
    return {
      pack: FRONTIER_GRAPH_PACK,
      diagrams: diagrams,
      mermaid: joinGraphDiagramSources(FRONTIER_GRAPH_PACK, diagrams),
      nodeCount: ids.length,
      edgeCount: rawEdges.length,
    };
  }

  // buildActiveDependencyMermaid(targets) — joined multi-diagram pin source (🎯T185/T190).
  function buildActiveDependencyMermaid(targets) {
    return buildActiveDependencyDiagrams(targets).mermaid;
  }

  // ── 🎯T280: owner-default Frontier Graph = single primary SVG (not multi-pack) ──

  // pickPrimaryGraphDiagram — largest component by nodes, then edges (stable id).
  // Multi-diagram pack stays residual; owner Graph opens the primary only.
  function pickPrimaryGraphDiagram(diagrams) {
    var list = Array.isArray(diagrams) ? diagrams : [];
    var best = null;
    for (var i = 0; i < list.length; i++) {
      var d = list[i];
      if (!d || !d.mermaid) continue;
      if (!best) {
        best = d;
        continue;
      }
      var dn = typeof d.nodeCount === 'number' ? d.nodeCount : 0;
      var bn = typeof best.nodeCount === 'number' ? best.nodeCount : 0;
      if (dn > bn) {
        best = d;
        continue;
      }
      if (dn < bn) continue;
      var de = typeof d.edgeCount === 'number' ? d.edgeCount : 0;
      var be = typeof best.edgeCount === 'number' ? best.edgeCount : 0;
      if (de > be) {
        best = d;
        continue;
      }
      if (de < be) continue;
      // Stable: prefer lower id for ties.
      var idA = d.id != null ? String(d.id) : '';
      var idB = best.id != null ? String(best.id) : '';
      if (idA && idB && idA < idB) best = d;
    }
    return best;
  }

  // resolveFrontierGraphOpenPlan — which diagrams the Graph control opens.
  // mode: empty | single | single-primary | pack
  //
  // 🎯T280 made single-primary the owner default. 🎯T294 reverts that: hiding
  // 6 of 7 components left one wide flat strip in a huge empty pane, and the
  // pane had nothing to fill the rest with. Default is now the full pack, laid
  // out by planFrontierGraphFit (pane-aspect pack → scale, or reflow at the
  // legibility floor). opts.preferPrimary keeps the single-primary view.
  function resolveFrontierGraphOpenPlan(model, opts) {
    var o = opts || {};
    var m = model || {};
    var diagrams = Array.isArray(m.diagrams) ? m.diagrams : [];
    var hasDiagrams = false;
    var i;
    for (i = 0; i < diagrams.length; i++) {
      if (diagrams[i] && diagrams[i].mermaid) {
        hasDiagrams = true;
        break;
      }
    }
    var mermaid = m.mermaid != null ? String(m.mermaid) : '';
    if (!m.available || (!mermaid && !hasDiagrams)) {
      return {
        mode: 'empty',
        mermaid: '',
        primary: null,
        diagrams: diagrams,
        diagramCount: diagrams.length,
        statusNote: '',
      };
    }
    // 🎯T294 owner default: show every component, packed into the pane.
    if (diagrams.length > 1 && o.preferPrimary !== true) {
      return {
        mode: 'pack',
        mermaid: mermaid,
        primary: pickPrimaryGraphDiagram(diagrams),
        diagrams: diagrams,
        diagramCount: diagrams.length,
        statusNote: diagrams.length + ' components packed',
      };
    }
    if (diagrams.length === 1 && diagrams[0] && diagrams[0].mermaid) {
      return {
        mode: 'single',
        mermaid: diagrams[0].mermaid,
        primary: diagrams[0],
        diagrams: diagrams,
        diagramCount: 1,
        statusNote: '',
      };
    }
    if (diagrams.length > 1) {
      var primary = pickPrimaryGraphDiagram(diagrams);
      if (primary && primary.mermaid) {
        return {
          mode: 'single-primary',
          mermaid: primary.mermaid,
          primary: primary,
          diagrams: diagrams,
          diagramCount: diagrams.length,
          statusNote: 'primary of ' + diagrams.length + ' components',
        };
      }
    }
    // Joined multi-pack pin source is not a single Mermaid diagram — refuse.
    if (mermaid && mermaid.indexOf('jevons-frontier-pack') >= 0) {
      return {
        mode: 'empty',
        mermaid: '',
        primary: null,
        diagrams: diagrams,
        diagramCount: diagrams.length,
        statusNote: 'joined pack source without renderable primary',
      };
    }
    return {
      mode: 'single',
      mermaid: mermaid,
      primary: null,
      diagrams: diagrams,
      diagramCount: diagrams.length || (mermaid ? 1 : 0),
      statusNote: '',
    };
  }

  // normalizeGraphPayload(apiJSON|err) → multi-diagram pack model (🎯T185/T190).
  function normalizeGraphPayload(payload, err) {
    if (err) {
      return {
        available: false,
        mermaid: '',
        diagrams: [],
        pack: FRONTIER_GRAPH_PACK,
        ledger: '',
        nodeCount: 0,
        edgeCount: 0,
        error: String(err && err.message ? err.message : err),
      };
    }
    var p = payload || {};
    var mermaid = p.mermaid != null ? String(p.mermaid) : '';
    var pack = p.pack != null && String(p.pack).trim()
      ? String(p.pack).trim()
      : FRONTIER_GRAPH_PACK;
    var diagrams = [];
    var rawDiagrams = Array.isArray(p.diagrams) ? p.diagrams : [];
    for (var i = 0; i < rawDiagrams.length; i++) {
      var d = rawDiagrams[i] || {};
      var dMermaid = d.mermaid != null ? String(d.mermaid) : '';
      if (!dMermaid) continue;
      diagrams.push({
        id: d.id != null ? String(d.id) : ('d' + i),
        kind: d.kind != null ? String(d.kind) : GRAPH_DIAGRAM_KIND_COMPONENT,
        title: d.title != null ? String(d.title) : '',
        mermaid: dMermaid,
        nodeCount: typeof d.node_count === 'number' ? d.node_count
          : (typeof d.nodeCount === 'number' ? d.nodeCount : 0),
        edgeCount: typeof d.edge_count === 'number' ? d.edge_count
          : (typeof d.edgeCount === 'number' ? d.edgeCount : 0),
      });
    }
    // Fallback: single mermaid blob without diagrams[] → one component block.
    if (!diagrams.length && mermaid && mermaid.indexOf('jevons-frontier-pack') < 0) {
      diagrams = [{
        id: 'single',
        kind: GRAPH_DIAGRAM_KIND_COMPONENT,
        title: 'Graph',
        mermaid: mermaid,
        nodeCount: typeof p.node_count === 'number' ? p.node_count
          : (typeof p.nodeCount === 'number' ? p.nodeCount : 0),
        edgeCount: typeof p.edge_count === 'number' ? p.edge_count
          : (typeof p.edgeCount === 'number' ? p.edgeCount : 0),
      }];
    }
    return {
      available: !!p.available,
      mermaid: mermaid,
      diagrams: diagrams,
      pack: pack,
      ledger: p.ledger != null ? String(p.ledger) : '',
      cwd: p.cwd != null ? String(p.cwd) : '',
      nodeCount: typeof p.node_count === 'number' ? p.node_count
        : (typeof p.nodeCount === 'number' ? p.nodeCount : 0),
      edgeCount: typeof p.edge_count === 'number' ? p.edge_count
        : (typeof p.edgeCount === 'number' ? p.edgeCount : 0),
      error: p.error != null ? String(p.error) : '',
    };
  }

  // formatDepMinigraph(row) — mermaid LR of focus + incoming + outgoing (🎯T184).
  // Returns fenced ```mermaid block or '' when no focus id.
  function formatDepMinigraph(row) {
    if (!row || !row.id) return '';
    var focusId = String(row.id).trim();
    var focusNode = mermaidNodeId(focusId);
    var focusLabel = mermaidLabel(focusId, row.name);
    var depsOn = normalizeDependents(
      row.depends_on != null ? row.depends_on : row.dependsOn
    );
    var dependents = normalizeDependents(row.dependents);
    var lines = [];
    lines.push('```mermaid');
    lines.push('graph LR');
    lines.push('  ' + focusNode + '["' + focusLabel + '"]');
    lines.push('  style ' + focusNode + ' stroke-width:2px');
    var declared = {};
    declared[focusNode] = true;
    function ensureNode(rel) {
      var nid = mermaidNodeId(rel.id);
      if (declared[nid]) return nid;
      declared[nid] = true;
      lines.push('  ' + nid + '["' + mermaidLabel(rel.id, rel.name) + '"]');
      return nid;
    }
    var i;
    // Incoming: dependent → focus
    for (i = 0; i < dependents.length; i++) {
      var inc = ensureNode(dependents[i]);
      lines.push('  ' + inc + ' --> ' + focusNode);
    }
    // Outgoing: focus → depends_on
    for (i = 0; i < depsOn.length; i++) {
      var out = ensureNode(depsOn[i]);
      lines.push('  ' + focusNode + ' --> ' + out);
    }
    lines.push('```');
    return lines.join('\n');
  }

  // formatMetric — value/cost display; omit when undefined/null/empty.
  function formatMetric(n) {
    if (n == null || n === '') return '';
    if (typeof n === 'number' && isFinite(n)) {
      if (n === 0) return '0';
      return String(n);
    }
    var s = String(n).trim();
    return s;
  }

  // formatTargetCardMarkdown(row) — full semantic card (🎯T181 + 🎯T184).
  // Common fields semantic; markdown bodies for acceptance/context/attestation;
  // extra key-values; mermaid minigraph of deps. Product: parseAssistantMarkdown + mermaid.
  function formatTargetCardMarkdown(row) {
    if (!row || !row.id) return '';
    var id = String(row.id).trim();
    var name = String(row.name || '').trim();
    var lines = [];
    lines.push('**🎯' + id + '**' + (name ? (' — ' + name) : ''));
    var st = statusTitle(row.status) || String(row.status || '').trim();
    if (st) {
      lines.push('');
      lines.push('**Status:** ' + st);
    }
    var val = formatMetric(row.value);
    var cost = formatMetric(row.cost);
    var actual = formatMetric(row.actual_cost != null ? row.actual_cost : row.actualCost);
    if (val || cost || actual) {
      lines.push('');
      var metrics = [];
      if (val) metrics.push('value ' + val);
      if (cost) metrics.push('cost ' + cost);
      if (actual) metrics.push('actual_cost ' + actual);
      lines.push('**Value / cost:** ' + metrics.join(' · '));
    }
    var tags = normalizeStringList(row.tags);
    if (tags.length > 0) {
      lines.push('');
      lines.push('**Tags:** ' + tags.join(', '));
    }
    var depsOn = normalizeDependents(
      row.depends_on != null ? row.depends_on : row.dependsOn
    );
    if (depsOn.length > 0) {
      lines.push('');
      lines.push('**Depends on** (' + depsOn.length + ')');
      for (var di = 0; di < depsOn.length; di++) {
        var dout = depsOn[di];
        var outBullet = '- ' + dout.id;
        if (dout.name) outBullet += ' — ' + dout.name;
        lines.push(outBullet);
      }
    }
    var deps = normalizeDependents(row.dependents);
    if (deps.length > 0) {
      lines.push('');
      lines.push('**Dependents** (' + deps.length + ')');
      for (var j = 0; j < deps.length; j++) {
        var d = deps[j];
        var bullet = '- ' + d.id;
        if (d.name) bullet += ' — ' + d.name;
        lines.push(bullet);
      }
    }
    var acc = normalizeStringList(row.acceptance);
    if (acc.length > 0) {
      lines.push('');
      lines.push('**Acceptance**');
      for (var i = 0; i < acc.length; i++) {
        // Keep markdown in criterion text intact for marked render.
        lines.push('- ' + acc[i]);
      }
    }
    var ctx = row.context != null ? String(row.context).trim() : '';
    if (ctx) {
      // Compact tip: first paragraph; cap very long single paragraphs.
      var firstPara = ctx.split(/\n\s*\n/)[0].trim();
      if (firstPara.length > 720) {
        firstPara = firstPara.slice(0, 719) + '…';
      }
      if (firstPara) {
        lines.push('');
        lines.push('**Context**');
        lines.push(firstPara);
      }
    }
    var att = row.attestation != null ? String(row.attestation).trim() : '';
    if (att) {
      if (att.length > 480) att = att.slice(0, 479) + '…';
      lines.push('');
      lines.push('**Attestation**');
      lines.push(att);
    }
    var meta = [];
    if (row.origin) meta.push('origin: ' + String(row.origin).trim());
    if (row.discovered) meta.push('discovered: ' + String(row.discovered).trim());
    if (row.achieved) meta.push('achieved: ' + String(row.achieved).trim());
    if (meta.length > 0) {
      lines.push('');
      lines.push('**Meta:** ' + meta.join(' · '));
    }
    // Unknown / extra ledger fields as key-value pairs (🎯T184).
    var extra = normalizeExtra(row.extra);
    var extraKeys = Object.keys(extra).sort();
    if (extraKeys.length > 0) {
      lines.push('');
      lines.push('**Other fields**');
      for (var ei = 0; ei < extraKeys.length; ei++) {
        var ek = extraKeys[ei];
        lines.push('- **' + ek + ':** ' + extra[ek]);
      }
    }
    // Mermaid minigraph: focus + incoming + outgoing (may be one-sided).
    var graph = formatDepMinigraph(row);
    if (graph) {
      lines.push('');
      lines.push('**Dependencies**');
      lines.push(graph);
    }
    return lines.join('\n');
  }

  // formatTargetCardPlain — aria-label / text fallback (no markdown markers).
  function formatTargetCardPlain(row) {
    if (!row || !row.id) return '';
    var parts = ['🎯' + String(row.id).trim()];
    if (row.name) parts.push(String(row.name).trim());
    var st = statusTitle(row.status) || String(row.status || '').trim();
    if (st) parts.push('Status: ' + st);
    var acc = normalizeStringList(row.acceptance);
    if (acc.length > 0) {
      parts.push('Acceptance: ' + acc.join('; '));
    }
    var depsOn = normalizeDependents(
      row.depends_on != null ? row.depends_on : row.dependsOn
    );
    if (depsOn.length > 0) {
      parts.push('Depends on: ' + depsOn.map(function (d) { return d.id; }).join(', '));
    }
    var deps = normalizeDependents(row.dependents);
    if (deps.length > 0) {
      parts.push('Dependents: ' + deps.map(function (d) { return d.id; }).join(', '));
    }
    return parts.join('. ');
  }

  // API path constants — client never hard-codes ledger file paths.
  var API_PATH = '/api/frontier';
  // 🎯T185: full unachieved dependency graph (Mermaid) for Frontier Graph control.
  var GRAPH_API_PATH = '/api/frontier/graph';

  // 🎯T182: play control → message product PO to start work on a frontier row.
  var PLAY_GLYPH = '\u25B6'; // ▶
  // 🎯T198: stop when row is engaged (worker has matching target_id).
  var STOP_GLYPH = '\u25A0'; // ■
  // 🎯T278: submitted/working chrome while kickoff is in flight (before engage).
  var PLAY_MODE_PLAY = 'play';
  var PLAY_MODE_STOP = 'stop';
  var PLAY_MODE_SUBMITTED = 'submitted';
  var DEFAULT_PLAY_PO = 'jevons-po';
  var ENGAGEMENT_STOP_PATH = '/api/agents/engagement/stop';
  var AGENTS_API_PATH = '/api/agents';

  // Product owners conventionally end with -po (jevons-po, yourworld2-po).
  function isProductOwnerName(name) {
    var n = String(name || '').trim().toLowerCase();
    if (!n || n.length < 4) return false;
    return n.slice(-3) === '-po';
  }

  function findAgentByName(agents, name) {
    var want = String(name || '').trim();
    if (!want) return null;
    var list = Array.isArray(agents) ? agents : [];
    for (var i = 0; i < list.length; i++) {
      var a = list[i];
      if (a && String(a.name || '').trim() === want) return a;
    }
    return null;
  }

  // resolvePlayPO — 🎯T255: bind kickoff recipient to selected product PO.
  // Priority:
  //   1. explicit opts.po
  //   2. legacy opts.agent when no selection/agents (direct PO name)
  //   3. selectedAgent is *-po / product owner → that agent
  //   4. selected is worker with parent PO in agents list → parent PO (walk up)
  //   5. selected non-overseer with workdir and *-po sibling/same-path residual:
  //      if selected itself is non-overseer with workdir and name is PO-shaped, use it
  //   6. no selection / overseer / unresolved → DEFAULT_PLAY_PO (jevons-po)
  function resolvePlayPO(opts) {
    var o = opts || {};
    if (o.po != null && String(o.po).trim()) {
      return String(o.po).trim();
    }

    var agents = Array.isArray(o.agents) ? o.agents : [];
    var selected = '';
    if (o.selectedAgent != null) selected = String(o.selectedAgent).trim();
    else if (o.selected != null) selected = String(o.selected).trim();

    // Legacy: opts.agent was a direct PO name when no selection wiring existed.
    if (!selected && agents.length === 0 && o.agent != null && String(o.agent).trim()) {
      return String(o.agent).trim();
    }
    // If caller still passes agent as selection without selectedAgent, accept it.
    if (!selected && o.agent != null) selected = String(o.agent).trim();

    if (!selected) return DEFAULT_PLAY_PO;

    var row = findAgentByName(agents, selected);
    if (row) {
      var purpose = String(row.purpose || row.role || '').trim().toLowerCase();
      if (purpose === 'overseer') return DEFAULT_PLAY_PO;

      // Selected is a product owner → kickoff that agent.
      if (isProductOwnerName(row.name)) {
        return String(row.name).trim();
      }

      // Worker / boss: walk parent chain for a product owner in the agents list.
      var seen = {};
      var cur = row;
      var hops = 0;
      while (cur && hops < 16) {
        hops++;
        var parentName = String(cur.parent || '').trim();
        if (!parentName || seen[parentName]) break;
        seen[parentName] = true;
        if (isProductOwnerName(parentName)) return parentName;
        var parentRow = findAgentByName(agents, parentName);
        if (!parentRow) {
          // Parent name known but not in list — still honor *-po shape.
          break;
        }
        var pp = String(parentRow.purpose || parentRow.role || '').trim().toLowerCase();
        if (pp === 'overseer') break;
        if (isProductOwnerName(parentRow.name)) {
          return String(parentRow.name).trim();
        }
        cur = parentRow;
      }

      // Non-overseer with workdir but not PO-shaped and no parent PO → residual default.
      // (Do not POST to a random worker; kickoff must land on a PO.)
      return DEFAULT_PLAY_PO;
    }

    // Selection name known but not in agents list: honor *-po shape, else default.
    if (isProductOwnerName(selected)) return selected;
    return DEFAULT_PLAY_PO;
  }

  // playKickoffTitle(po) — tooltip / title for the play button (real recipient).
  function playKickoffTitle(po) {
    var n = String(po || DEFAULT_PLAY_PO).trim() || DEFAULT_PLAY_PO;
    return 'Start work via ' + n;
  }

  // agentSendPath(name) — product HTTP proxy for fleet agent_send (🎯T182).
  function agentSendPath(name) {
    var n = String(name || DEFAULT_PLAY_PO).trim() || DEFAULT_PLAY_PO;
    return '/api/agents/' + encodeURIComponent(n) + '/send';
  }

  // normalizeTargetID — strip 🎯 / whitespace. Engagement match key (🎯T198).
  // Never derived from agent names.
  function normalizeTargetID(raw) {
    var s = raw == null ? '' : String(raw).trim();
    if (!s) return '';
    // U+1F3AF TARGET (may arrive as surrogate pair or literal).
    s = s.replace(/^\uD83C\uDFAF/, '').replace(/^🎯/, '');
    return s.trim();
  }

  // engagementIndex(agents) — map target_id → { agents: [name,...] }.
  // Uses agent.target_id only (explicit registry field). No name parsing.
  function engagementIndex(agents) {
    var list = Array.isArray(agents) ? agents : [];
    var by = {};
    for (var i = 0; i < list.length; i++) {
      var a = list[i];
      if (!a) continue;
      var tid = normalizeTargetID(
        a.target_id != null ? a.target_id : (a.targetId != null ? a.targetId : '')
      );
      if (!tid) continue;
      var purpose = String(a.purpose || 'work').trim().toLowerCase();
      if (purpose === 'overseer') continue;
      var name = String(a.name || '').trim();
      if (!name) continue;
      if (!by[tid]) by[tid] = { agents: [] };
      if (by[tid].agents.indexOf(name) < 0) by[tid].agents.push(name);
    }
    Object.keys(by).forEach(function (k) {
      by[k].agents.sort();
    });
    return by;
  }

  // applyEngagement(rows, agents) — overlay engaged flag + sink engaged rows
  // to bottom. Free frontier items keep relative order; engaged keep relative
  // order among themselves after free. Pure Jevons UI overlay (not bullseye status).
  function applyEngagement(rows, agents) {
    var index = engagementIndex(agents);
    var list = Array.isArray(rows) ? rows : [];
    var free = [];
    var engaged = [];
    for (var i = 0; i < list.length; i++) {
      var row = list[i];
      if (!row || !row.id) continue;
      var tid = normalizeTargetID(row.id);
      var hit = index[tid];
      var copy = {};
      Object.keys(row).forEach(function (k) { copy[k] = row[k]; });
      if (hit && hit.agents && hit.agents.length > 0) {
        copy.engaged = true;
        copy.engaged_agents = hit.agents.slice();
        engaged.push(copy);
      } else {
        copy.engaged = false;
        copy.engaged_agents = [];
        free.push(copy);
      }
    }
    return free.concat(engaged);
  }

  // canPlayKickoff(row) — 🎯T222: refuse set_aside / achieved / already engaged.
  // Pure gate used by playKickoffRequest and UI. Residual: force via opts.force.
  function canPlayKickoff(row, opts) {
    var o = opts || {};
    if (o.force) {
      return { ok: true, reason: 'ok' };
    }
    if (!row || !row.id) {
      return { ok: false, reason: 'no_id', message: 'missing target id' };
    }
    if (row.engaged) {
      var agents = Array.isArray(row.engaged_agents) ? row.engaged_agents.slice() : [];
      return {
        ok: false,
        reason: 'already_engaged',
        agents: agents,
        message: 'target already has engaged implementer(s)' +
          (agents.length ? (': ' + agents.join(', ')) : '') +
          ' — focus existing engagement or stop first',
      };
    }
    var st = String(row.status || '').trim().toLowerCase().replace(/-/g, '_');
    if (st === 'set_aside' || st === 'achieved') {
      return {
        ok: false,
        reason: st,
        message: 'target is ' + st + ' — not available for kickoff',
      };
    }
    return { ok: true, reason: 'ok' };
  }

  // buildPlayKickoffText(row) — body for PO: target id + name + spawn brief.
  // Asks PO to kick off with full brief, parent=jevons-po (not toast-only).
  // 🎯T198: require target_id= on jevons_agent_start so UI can engage without name parse.
  // 🎯T222: empty string when canPlayKickoff refuses (caller uses blocked request).
  function buildPlayKickoffText(row, opts) {
    if (!row || !row.id) return '';
    var gate = canPlayKickoff(row, opts);
    if (!gate.ok) return '';
    var id = String(row.id).trim();
    var name = row.name != null ? String(row.name).trim() : '';
    var po = resolvePlayPO(opts);
    var lines = [];
    lines.push('Start work on frontier target 🎯' + id +
      (name ? (' — ' + name) : '') + '.');
    lines.push('');
    lines.push(
      'Kick off now: spawn/brief a fleet worker with parent=' + po +
      ' and target_id=' + id +
      ' (jevons_agent_start target_id arg — required for Frontier engagement overlay 🎯T198; do not encode the T-id only in the worker name) ' +
      'and a full brief to execute this target end-to-end ' +
      '(local commits + oracle evidence; no Ship/PR unless the owner asks). ' +
      'Do not only toast or acknowledge — actually start the worker.'
    );
    // 🎯T197: hierarchical target ids keep literal dots in worker names.
    lines.push(
      'Worker name: encode hierarchical target ids with literal dots ' +
      '(e.g. jv-t27.2-config not jv-t272-config); flat ids stay flat (jv-t159-seal).'
    );
    // 🎯T222: if an implementer is already engaged, do not spawn a second.
    lines.push(
      'If target_id=' + id + ' already has an engaged work agent, do not spawn a second — focus the existing engagement (🎯T222).'
    );
    var st = statusTitle(row.status) || String(row.status || '').trim();
    if (st) {
      lines.push('');
      lines.push('Status: ' + st);
    }
    var acc = normalizeStringList(row.acceptance);
    if (acc.length > 0) {
      lines.push('');
      lines.push('Acceptance:');
      for (var i = 0; i < acc.length; i++) {
        lines.push('- ' + acc[i]);
      }
    }
    return lines.join('\n');
  }

  // playKickoffRequest(row, opts) — pure request shape for hermetic mocks.
  // { url, method, body: { text }, po } or { blocked: true, reason, message }
  function playKickoffRequest(row, opts) {
    var gate = canPlayKickoff(row, opts);
    if (!gate.ok) {
      return {
        blocked: true,
        reason: gate.reason,
        message: gate.message || gate.reason,
        agents: gate.agents || [],
        po: resolvePlayPO(opts),
      };
    }
    var po = resolvePlayPO(opts);
    var text = buildPlayKickoffText(row, opts);
    return {
      url: agentSendPath(po),
      method: 'POST',
      body: { text: text },
      po: po,
      blocked: false,
    };
  }

  // stopEngagementRequest(targetId) — pure POST body for stop (🎯T198).
  function stopEngagementRequest(targetId) {
    var tid = normalizeTargetID(targetId);
    return {
      url: ENGAGEMENT_STOP_PATH,
      method: 'POST',
      body: { target_id: tid },
      target_id: tid,
    };
  }

  // ── 🎯T278: optimistic kickoff-submitted chrome (before PO reply / engage) ──
  // Set is a plain { [normalizedId]: true } map so hermetics stay JSON-friendly.

  function addKickoffSubmitted(set, targetId) {
    var tid = normalizeTargetID(targetId);
    if (!tid) return set && typeof set === 'object' ? set : {};
    var out = {};
    var src = set && typeof set === 'object' ? set : {};
    for (var k in src) {
      if (Object.prototype.hasOwnProperty.call(src, k) && src[k]) out[k] = true;
    }
    out[tid] = true;
    return out;
  }

  function removeKickoffSubmitted(set, targetId) {
    var tid = normalizeTargetID(targetId);
    var src = set && typeof set === 'object' ? set : {};
    if (!tid || !src[tid]) return src;
    var out = {};
    for (var k in src) {
      if (Object.prototype.hasOwnProperty.call(src, k) && src[k] && k !== tid) {
        out[k] = true;
      }
    }
    return out;
  }

  function isKickoffSubmitted(set, targetId) {
    var tid = normalizeTargetID(targetId);
    return !!(tid && set && typeof set === 'object' && set[tid]);
  }

  // Drop submitted flags once engagement lands (stop chrome owns the cell).
  function pruneKickoffSubmitted(set, rows) {
    var src = set && typeof set === 'object' ? set : {};
    var list = Array.isArray(rows) ? rows : [];
    var engaged = {};
    for (var i = 0; i < list.length; i++) {
      var r = list[i];
      if (r && r.engaged) {
        var eid = normalizeTargetID(r.id);
        if (eid) engaged[eid] = true;
      }
    }
    var out = {};
    var changed = false;
    for (var k in src) {
      if (!Object.prototype.hasOwnProperty.call(src, k) || !src[k]) continue;
      if (engaged[k]) {
        changed = true;
        continue;
      }
      out[k] = true;
    }
    return changed ? out : src;
  }

  // Overlay kickoff_submitted on free rows present in the set.
  function applyKickoffSubmitted(rows, set) {
    var list = Array.isArray(rows) ? rows : [];
    var src = set && typeof set === 'object' ? set : {};
    var out = [];
    for (var i = 0; i < list.length; i++) {
      var row = list[i];
      if (!row) continue;
      var copy = Object.assign({}, row);
      var tid = normalizeTargetID(copy.id);
      if (tid && src[tid] && !copy.engaged) {
        copy.kickoff_submitted = true;
      } else {
        copy.kickoff_submitted = false;
      }
      out.push(copy);
    }
    return out;
  }

  // playChromeMode(row) — stop > submitted > play.
  function playChromeMode(row) {
    if (row && row.engaged) return PLAY_MODE_STOP;
    if (row && row.kickoff_submitted) return PLAY_MODE_SUBMITTED;
    return PLAY_MODE_PLAY;
  }

  // playChromeSpec(row, opts) — pure button chrome for hermetic + render.
  // opts: { po } for play title when free.
  function playChromeSpec(row, opts) {
    var o = opts || {};
    var id = row && row.id != null ? String(row.id).replace(/^🎯/, '').trim() : '';
    var mode = playChromeMode(row);
    if (mode === PLAY_MODE_STOP) {
      return {
        mode: PLAY_MODE_STOP,
        className: 'ft-play-btn ft-stop-btn',
        glyph: STOP_GLYPH,
        ariaLabel: 'Stop work on 🎯' + id,
        title: 'Stop engaged worker(s) for this target',
        disabled: false,
        spinning: false,
      };
    }
    if (mode === PLAY_MODE_SUBMITTED) {
      return {
        mode: PLAY_MODE_SUBMITTED,
        className: 'ft-play-btn ft-submitted-btn',
        glyph: '',
        ariaLabel: 'Kickoff submitted for 🎯' + id,
        title: 'Kickoff submitted to PO — waiting for engagement',
        disabled: true,
        spinning: true,
      };
    }
    var po = o.po != null ? String(o.po).trim() : '';
    if (!po) po = resolvePlayPO(o);
    return {
      mode: PLAY_MODE_PLAY,
      className: 'ft-play-btn',
      glyph: PLAY_GLYPH,
      ariaLabel: 'Start work on 🎯' + id,
      title: playKickoffTitle(po),
      disabled: false,
      spinning: false,
      po: po,
    };
  }

  // 🎯T230: quiet poll / re-render must not remount while InstantTip hover is latched.
  // Pure policy for hermetics; index.html calls InstantTip.anyHoverLatched().
  function shouldSkipRerenderWhileTipLatched(latched) {
    return !!latched;
  }

  // 🎯T253: Frontier tab follows selected agent workdir ledger.
  // Empty string → server uses configured primary / process cwd (no ?cwd=).
  // Overseer / missing agent / blank workdir → primary. Fleet PO/worker with
  // workdir → that path (server discovers bullseye for it; no ledger → empty).
  function resolveFrontierCwd(selectedName, agents) {
    var name = selectedName == null ? '' : String(selectedName).trim();
    if (!name) return '';
    var list = Array.isArray(agents) ? agents : [];
    var row = null;
    for (var i = 0; i < list.length; i++) {
      var a = list[i];
      if (a && String(a.name || '').trim() === name) {
        row = a;
        break;
      }
    }
    if (!row) return '';
    var purpose = String(row.purpose || '').trim().toLowerCase();
    if (purpose === 'overseer') return '';
    var wd = row.workdir != null ? String(row.workdir).trim() : '';
    return wd;
  }

  // frontierAPIURL(basePath, cwd) — append ?cwd= when non-empty (encodeURIComponent).
  function frontierAPIURL(basePath, cwd) {
    var base = basePath == null || basePath === '' ? API_PATH : String(basePath);
    var c = cwd == null ? '' : String(cwd).trim();
    if (!c) return base;
    var sep = base.indexOf('?') >= 0 ? '&' : '?';
    return base + sep + 'cwd=' + encodeURIComponent(c);
  }

  // ── 🎯T267: target-ask → owning PO select + Frontier row highlight ──────
  // Coordinates with T266 TargetContextChrome when present. Pure helpers stay
  // hermetic without requiring the chrome module.

  function extractTargetIDs(text) {
    var s = String(text == null ? '' : text);
    var out = [];
    var seen = {};
    var re = /🎯\s*(T[0-9]+(?:\.[0-9]+)*)/g;
    var m;
    while ((m = re.exec(s)) !== null) {
      var id = normalizeTargetID(m[1]);
      if (!id || seen[id]) continue;
      seen[id] = true;
      out.push(id);
    }
    return out;
  }

  // detectTargetAsk(text) → { targetId, po } | null
  // Prefer __TARGET_ASK__:Tn[|@po]; also needs-owner / decision-packet prose.
  function detectTargetAsk(text) {
    var s = String(text == null ? '' : text);
    if (!s) return null;
    var m = s.match(
      /__TARGET_ASK__\s*:\s*(T[0-9]+(?:\.[0-9]+)*)(?:\s*[|@]\s*([A-Za-z0-9_.\-]+))?/i
    );
    if (m) {
      return {
        targetId: normalizeTargetID(m[1]),
        po: m[2] ? String(m[2]).trim() : '',
      };
    }
    var ids = extractTargetIDs(s);
    if (!ids.length) return null;
    var askish = /needs[- ]owner|decision\s*packet|owner\s+decision|please\s+(decide|choose|confirm|accept)|needs\s+your\s+(decision|input|call)|awaiting\s+owner|owner\s+call|owner\s+ask/i.test(s);
    if (!askish) return null;
    return { targetId: ids[0], po: '' };
  }

  // resolveOwningPOForTarget — preferredPO → engaged target_id → play-PO walk → default.
  function resolveOwningPOForTarget(opts) {
    var o = opts || {};
    var preferred = o.preferredPO != null ? String(o.preferredPO).trim() : '';
    if (!preferred && o.po != null) preferred = String(o.po).trim();
    if (preferred && isProductOwnerName(preferred)) return preferred;
    if (preferred && preferred.indexOf('-po') > 0) return preferred;

    var agents = Array.isArray(o.agents) ? o.agents : [];
    var tid = normalizeTargetID(o.targetId != null ? o.targetId : o.id);
    if (tid) {
      for (var i = 0; i < agents.length; i++) {
        var a = agents[i];
        if (!a) continue;
        var atid = normalizeTargetID(
          a.target_id != null ? a.target_id : (a.targetId != null ? a.targetId : '')
        );
        if (atid !== tid) continue;
        var purpose = String(a.purpose || a.role || '').trim().toLowerCase();
        if (purpose === 'overseer') continue;
        if (isProductOwnerName(a.name)) return String(a.name).trim();
        var via = resolvePlayPO({ selectedAgent: a.name, agents: agents });
        if (via) return via;
      }
    }
    return DEFAULT_PLAY_PO;
  }

  function rowMatchesHighlight(row, highlightId) {
    if (!row || !highlightId) return false;
    var rid = normalizeTargetID(row.id);
    var hid = normalizeTargetID(highlightId);
    return !!(rid && hid && rid === hid);
  }

  // planTargetAskFocus → { targetId, highlightId, po, tab } | null
  function planTargetAskFocus(opts) {
    var o = opts || {};
    var detected = null;
    if (o.targetId != null && String(o.targetId).trim()) {
      detected = {
        targetId: normalizeTargetID(o.targetId),
        po: o.po != null
          ? String(o.po).trim()
          : (o.preferredPO != null ? String(o.preferredPO).trim() : ''),
      };
    } else {
      detected = detectTargetAsk(o.text);
    }
    if (!detected || !detected.targetId) return null;
    var po = resolveOwningPOForTarget({
      targetId: detected.targetId,
      agents: o.agents,
      preferredPO: detected.po || o.po || o.preferredPO,
    });
    return {
      targetId: detected.targetId,
      highlightId: detected.targetId,
      po: po,
      tab: TAB_FRONTIER,
      selectPO: true,
    };
  }

  return {
    TAB_TRANSCRIPT: TAB_TRANSCRIPT,
    TAB_FRONTIER: TAB_FRONTIER,
    POLL_MS: POLL_MS,
    API_PATH: API_PATH,
    GRAPH_API_PATH: GRAPH_API_PATH,
    FANOUT_MARK: FANOUT_MARK,
    PLAY_GLYPH: PLAY_GLYPH,
    STOP_GLYPH: STOP_GLYPH,
    PLAY_MODE_PLAY: PLAY_MODE_PLAY,
    PLAY_MODE_STOP: PLAY_MODE_STOP,
    PLAY_MODE_SUBMITTED: PLAY_MODE_SUBMITTED,
    DEFAULT_PLAY_PO: DEFAULT_PLAY_PO,
    ENGAGEMENT_STOP_PATH: ENGAGEMENT_STOP_PATH,
    AGENTS_API_PATH: AGENTS_API_PATH,
    nextBottomTab: nextBottomTab,
    tabAfterAgentSelect: tabAfterAgentSelect,
    targetIDLess: targetIDLess,
    targetIDCompare: targetIDCompare,
    normalizePayload: normalizePayload,
    normalizeGraphPayload: normalizeGraphPayload,
    pickPrimaryGraphDiagram: pickPrimaryGraphDiagram,
    resolveFrontierGraphOpenPlan: resolveFrontierGraphOpenPlan,
    formatStatus: formatStatus,
    statusTitle: statusTitle,
    formatFanout: formatFanout,
    normalizeDependents: normalizeDependents,
    shortName: shortName,
    maxIdChWidth: maxIdChWidth,
    emptyMessage: emptyMessage,
    formatTargetCardMarkdown: formatTargetCardMarkdown,
    formatTargetCardPlain: formatTargetCardPlain,
    formatDepMinigraph: formatDepMinigraph,
    buildActiveDependencyMermaid: buildActiveDependencyMermaid,
    buildActiveDependencyDiagrams: buildActiveDependencyDiagrams,
    mermaidActiveGraphHeader: mermaidActiveGraphHeader,
    packActiveGraphIslands: packActiveGraphIslands,
    splitOrphanComponents: splitOrphanComponents,
    splitOversizedComponents: splitOversizedComponents,
    targetRootFamily: targetRootFamily,
    chunkIDList: chunkIDList,
    packActiveGraphDiagrams: packActiveGraphDiagrams,
    joinGraphDiagramSources: joinGraphDiagramSources,
    emitMermaidForNodes: emitMermaidForNodes,
    FRONTIER_GRAPH_PACK: FRONTIER_GRAPH_PACK,
    GRAPH_DIAGRAM_KIND_COMPONENT: GRAPH_DIAGRAM_KIND_COMPONENT,
    GRAPH_DIAGRAM_KIND_ORPHANS: GRAPH_DIAGRAM_KIND_ORPHANS,
    MERMAID_NODE_SPACING: MERMAID_NODE_SPACING,
    MERMAID_RANK_SPACING: MERMAID_RANK_SPACING,
    MERMAID_WRAPPING_WIDTH: MERMAID_WRAPPING_WIDTH,
    MERMAID_MAX_NODES_PER_DIAGRAM: MERMAID_MAX_NODES_PER_DIAGRAM,
    mermaidNodeId: mermaidNodeId,
    isProductOwnerName: isProductOwnerName,
    resolvePlayPO: resolvePlayPO,
    playKickoffTitle: playKickoffTitle,
    agentSendPath: agentSendPath,
    canPlayKickoff: canPlayKickoff,
    buildPlayKickoffText: buildPlayKickoffText,
    playKickoffRequest: playKickoffRequest,
    normalizeTargetID: normalizeTargetID,
    engagementIndex: engagementIndex,
    applyEngagement: applyEngagement,
    stopEngagementRequest: stopEngagementRequest,
    addKickoffSubmitted: addKickoffSubmitted,
    removeKickoffSubmitted: removeKickoffSubmitted,
    isKickoffSubmitted: isKickoffSubmitted,
    pruneKickoffSubmitted: pruneKickoffSubmitted,
    applyKickoffSubmitted: applyKickoffSubmitted,
    playChromeMode: playChromeMode,
    playChromeSpec: playChromeSpec,
    shouldSkipRerenderWhileTipLatched: shouldSkipRerenderWhileTipLatched,
    resolveFrontierCwd: resolveFrontierCwd,
    frontierAPIURL: frontierAPIURL,
    // 🎯T267 target-ask focus
    extractTargetIDs: extractTargetIDs,
    detectTargetAsk: detectTargetAsk,
    resolveOwningPOForTarget: resolveOwningPOForTarget,
    rowMatchesHighlight: rowMatchesHighlight,
    planTargetAskFocus: planTargetAskFocus,
  };

}));
