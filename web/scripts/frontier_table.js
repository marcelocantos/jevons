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
  // Hide when 0 (🎯T173). Nonzero: "N᚛" + InstantTip lead count + bullets "• TID — name" (🎯T179).
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
        if (d.name) bullet += ' — ' + d.name;
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

  // 🎯T190: Mermaid layout knobs — keep ~30–50 nodes inside the ~90% panel.
  var MERMAID_NODE_SPACING = 28;
  var MERMAID_RANK_SPACING = 36;
  var MERMAID_WRAPPING_WIDTH = 180;

  // mermaidActiveGraphHeader — init + flowchart TB (🎯T185 + 🎯T190).
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

  // packActiveGraphIslands — connected components for vertical island packing
  // (🎯T190). ids sorted; undirected edges from {from,to} original ids.
  // Returns array of islands (each island = sorted id list); islands ordered
  // by first id.
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

  // buildActiveDependencyMermaid(targets) — full unachieved graph (🎯T185).
  // targets: [{ id, name?, depends_on?: [{id}|string] }]. Only nodes present
  // in the set are drawn; edges whose dep is missing are dropped (among
  // non-achieved). Returns raw Mermaid (no fences) for panel render.
  // 🎯T190: layout init + flowchart TB + island subgraph packing (stack
  // components vertically so the graph is not one infinite LR strip).
  function buildActiveDependencyMermaid(targets) {
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
    var rawEdges = []; // { from, to } original ids
    for (i = 0; i < ids.length; i++) {
      var fromId = ids[i];
      var deps = normalizeDependents(
        byId[fromId].depends_on != null
          ? byId[fromId].depends_on
          : byId[fromId].dependsOn
      );
      for (var j = 0; j < deps.length; j++) {
        var depId = deps[j].id;
        if (!byId[depId]) continue; // only edges among unachieved set
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
    var lines = [mermaidActiveGraphHeader()];
    for (i = 0; i < islands.length; i++) {
      lines.push('    subgraph island_' + i + '[" "]');
      lines.push('        direction TB');
      var island = islands[i];
      var islandSet = {};
      var n;
      for (n = 0; n < island.length; n++) {
        islandSet[island[n]] = true;
        var nid = mermaidNodeId(island[n]);
        var row = byId[island[n]];
        var name = row && row.name != null ? String(row.name).trim() : '';
        var label = mermaidLabel(island[n], name);
        lines.push('        ' + nid + '["' + label + '"]');
      }
      for (n = 0; n < rawEdges.length; n++) {
        var e = rawEdges[n];
        if (!islandSet[e.from] || !islandSet[e.to]) continue;
        lines.push(
          '        ' +
            mermaidNodeId(e.from) +
            ' -.->|needs| ' +
            mermaidNodeId(e.to)
        );
      }
      lines.push('    end');
    }
    // Vertical packing chain between islands (invisible layout links).
    if (islands.length > 1) {
      var realEdgeCount = rawEdges.length;
      var packIdx = [];
      for (i = 0; i < islands.length - 1; i++) {
        lines.push('    island_' + i + ' ~~~ island_' + (i + 1));
        packIdx.push(String(realEdgeCount + i));
      }
      lines.push('    linkStyle ' + packIdx.join(',') + ' stroke:none,fill:none');
    }
    return lines.join('\n') + '\n';
  }

  // normalizeGraphPayload(apiJSON|err) → { available, mermaid, ledger, nodes, edges, error }
  function normalizeGraphPayload(payload, err) {
    if (err) {
      return {
        available: false,
        mermaid: '',
        ledger: '',
        nodeCount: 0,
        edgeCount: 0,
        error: String(err && err.message ? err.message : err),
      };
    }
    var p = payload || {};
    var mermaid = p.mermaid != null ? String(p.mermaid) : '';
    return {
      available: !!p.available,
      mermaid: mermaid,
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
  var DEFAULT_PLAY_PO = 'jevons-po';

  // resolvePlayPO — residual multi-PO: default jevons-po for jevons ledger.
  function resolvePlayPO(opts) {
    var o = opts || {};
    if (o.po) return String(o.po).trim() || DEFAULT_PLAY_PO;
    if (o.agent) return String(o.agent).trim() || DEFAULT_PLAY_PO;
    return DEFAULT_PLAY_PO;
  }

  // agentSendPath(name) — product HTTP proxy for fleet agent_send (🎯T182).
  function agentSendPath(name) {
    var n = String(name || DEFAULT_PLAY_PO).trim() || DEFAULT_PLAY_PO;
    return '/api/agents/' + encodeURIComponent(n) + '/send';
  }

  // buildPlayKickoffText(row) — body for PO: target id + name + spawn brief.
  // Asks PO to kick off with full brief, parent=jevons-po (not toast-only).
  function buildPlayKickoffText(row, opts) {
    if (!row || !row.id) return '';
    var id = String(row.id).trim();
    var name = row.name != null ? String(row.name).trim() : '';
    var po = resolvePlayPO(opts);
    var lines = [];
    lines.push('Start work on frontier target 🎯' + id +
      (name ? (' — ' + name) : '') + '.');
    lines.push('');
    lines.push(
      'Kick off now: spawn/brief a fleet worker with parent=' + po +
      ' and a full brief to execute this target end-to-end ' +
      '(local commits + oracle evidence; no Ship/PR unless the owner asks). ' +
      'Do not only toast or acknowledge — actually start the worker.'
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
  // { url, method, body: { text }, po }
  function playKickoffRequest(row, opts) {
    var po = resolvePlayPO(opts);
    var text = buildPlayKickoffText(row, opts);
    return {
      url: agentSendPath(po),
      method: 'POST',
      body: { text: text },
      po: po,
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
    DEFAULT_PLAY_PO: DEFAULT_PLAY_PO,
    nextBottomTab: nextBottomTab,
    tabAfterAgentSelect: tabAfterAgentSelect,
    targetIDLess: targetIDLess,
    targetIDCompare: targetIDCompare,
    normalizePayload: normalizePayload,
    normalizeGraphPayload: normalizeGraphPayload,
    formatStatus: formatStatus,
    statusTitle: statusTitle,
    formatFanout: formatFanout,
    normalizeDependents: normalizeDependents,
    shortName: shortName,
    emptyMessage: emptyMessage,
    formatTargetCardMarkdown: formatTargetCardMarkdown,
    formatTargetCardPlain: formatTargetCardPlain,
    formatDepMinigraph: formatDepMinigraph,
    buildActiveDependencyMermaid: buildActiveDependencyMermaid,
    mermaidActiveGraphHeader: mermaidActiveGraphHeader,
    packActiveGraphIslands: packActiveGraphIslands,
    MERMAID_NODE_SPACING: MERMAID_NODE_SPACING,
    MERMAID_RANK_SPACING: MERMAID_RANK_SPACING,
    MERMAID_WRAPPING_WIDTH: MERMAID_WRAPPING_WIDTH,
    mermaidNodeId: mermaidNodeId,
    resolvePlayPO: resolvePlayPO,
    agentSendPath: agentSendPath,
    buildPlayKickoffText: buildPlayKickoffText,
    playKickoffRequest: playKickoffRequest,
  };
}));
