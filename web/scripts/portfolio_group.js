// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure RHS domain-portfolio tree helpers (🎯T200).
// DOM-free so Node hermetic tests can require() it.
//
// Portfolios are interwoven into the fleet tree under the root overseer:
//   jevons → portfolio nodes → member POs
//   jevons → unassigned POs (no portfolio) directly
//
// Display-only reparent: lineage_parent keeps the real registry parent so
// kill/lineage rules stay safe. Membership is declarative path matching
// against workdir — never agent-name parse. Empty/missing → identity tree.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.PortfolioGroup = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  var PORTFOLIO_PREFIX = 'portfolio:';
  var FOLDER_ICON = '📁';

  function escHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function normalizeList(payload) {
    if (!payload) return [];
    if (Array.isArray(payload)) return payload;
    if (Array.isArray(payload.portfolios)) return payload.portfolios;
    return [];
  }

  function portfolioNodeName(id) {
    var s = String(id == null ? '' : id).trim();
    if (!s) return '';
    if (s.indexOf(PORTFOLIO_PREFIX) === 0) return s;
    return PORTFOLIO_PREFIX + s;
  }

  function portfolioIdFromNodeName(name) {
    var s = String(name == null ? '' : name);
    if (s.indexOf(PORTFOLIO_PREFIX) !== 0) return '';
    return s.slice(PORTFOLIO_PREFIX.length);
  }

  function isPortfolioNode(agent) {
    if (!agent || typeof agent !== 'object') return false;
    if (agent.is_portfolio === true) return true;
    var purpose = String(agent.purpose || agent.role || '').trim().toLowerCase();
    if (purpose === 'portfolio') return true;
    return String(agent.name || '').indexOf(PORTFOLIO_PREFIX) === 0;
  }

  function normalizePath(s) {
    s = String(s == null ? '' : s).replace(/\\/g, '/').replace(/\/+$/, '');
    if (!s) return '';
    while (s.indexOf('//') >= 0) s = s.replace(/\/\//g, '/');
    // /Users/<name>/rest → ~/rest
    var m = /^\/Users\/[^/]+(\/.*)?$/.exec(s);
    if (m) s = '~' + (m[1] || '');
    return s;
  }

  // Declarative workdir containment — never agent-name heuristics.
  function workdirMatchesMember(workdir, memberPath) {
    var wd = normalizePath(workdir);
    var mp = normalizePath(memberPath);
    if (!wd || !mp) return false;
    if (wd === mp) return true;
    if (wd.indexOf(mp) >= 0) return true;
    var rawWD = String(workdir || '').replace(/\\/g, '/').replace(/\/+$/, '');
    var rawMP = String(memberPath || '').replace(/\\/g, '/').replace(/\/+$/, '');
    if (rawWD && rawMP && (rawWD === rawMP || rawWD.indexOf(rawMP) >= 0)) return true;
    return false;
  }

  function memberPaths(portfolio) {
    var members = portfolio && Array.isArray(portfolio.members) ? portfolio.members : [];
    var out = [];
    for (var i = 0; i < members.length; i++) {
      var m = members[i];
      if (typeof m === 'string') {
        var t = m.trim();
        if (t) out.push(t);
      } else if (m && typeof m === 'object') {
        var p = String(m.path || '').trim();
        if (p) out.push(p);
      }
    }
    return out;
  }

  // First matching portfolio id for a workdir, or '' if none.
  function matchPortfolioId(workdir, portfolios) {
    var list = Array.isArray(portfolios) ? portfolios : [];
    for (var i = 0; i < list.length; i++) {
      var p = list[i];
      if (!p || typeof p !== 'object') continue;
      var id = String(p.id || '').trim();
      if (!id) continue;
      var paths = memberPaths(p);
      for (var j = 0; j < paths.length; j++) {
        if (workdirMatchesMember(workdir, paths[j])) return id;
      }
    }
    return '';
  }

  function findOverseerName(agents, explicit) {
    if (explicit) return String(explicit).trim();
    var list = Array.isArray(agents) ? agents : [];
    var root = null;
    for (var i = 0; i < list.length; i++) {
      var a = list[i];
      if (!a || !a.name) continue;
      if (a.parent) continue;
      if (String(a.name) === 'jevons') return 'jevons';
      if (!root) root = a;
    }
    return root ? String(root.name) : 'jevons';
  }

  function groupByParent(nodes) {
    var byParent = {};
    var list = Array.isArray(nodes) ? nodes : [];
    for (var i = 0; i < list.length; i++) {
      var a = list[i];
      if (!a || !a.name) continue;
      var p = a.parent || '';
      if (!byParent[p]) byParent[p] = [];
      byParent[p].push(a);
    }
    return byParent;
  }

  // Weave portfolio virtual parents under overseer; reparent matching
  // direct children of overseer for display only.
  // Returns { nodes, byParent, overseerName }.
  function weaveFleetTree(agents, portfolioPayload, opts) {
    opts = opts && typeof opts === 'object' ? opts : {};
    var list = Array.isArray(agents)
      ? agents.map(function (a) {
          return a && typeof a === 'object' ? Object.assign({}, a) : a;
        })
      : [];
    var portfolios = normalizeList(portfolioPayload);
    var overseerName = findOverseerName(list, opts.overseerName);

    // Always stamp lineage_parent from original parent for non-virtual rows.
    for (var i = 0; i < list.length; i++) {
      var row = list[i];
      if (!row || typeof row !== 'object') continue;
      if (row.lineage_parent == null || row.lineage_parent === '') {
        row.lineage_parent = row.parent || '';
      }
    }

    if (!portfolios.length) {
      return {
        nodes: list,
        byParent: groupByParent(list),
        overseerName: overseerName,
      };
    }

    var virtual = [];
    for (var pi = 0; pi < portfolios.length; pi++) {
      var p = portfolios[pi];
      if (!p || typeof p !== 'object') continue;
      var id = String(p.id || '').trim();
      if (!id) continue;
      var label = String(p.name || id).trim() || id;
      virtual.push({
        name: portfolioNodeName(id),
        description: label,
        purpose: 'portfolio',
        is_portfolio: true,
        parent: overseerName,
        lineage_parent: overseerName,
        status: '',
        workdir: '',
      });
    }

    for (var ai = 0; ai < list.length; ai++) {
      var a = list[ai];
      if (!a || typeof a !== 'object') continue;
      if (isPortfolioNode(a)) continue;
      if (String(a.name) === overseerName) continue;
      var lineage = a.lineage_parent != null ? a.lineage_parent : (a.parent || '');
      // Only reparent agents that hang directly under the overseer.
      if (String(lineage) !== overseerName) continue;
      var pid = matchPortfolioId(a.workdir, portfolios);
      if (pid) {
        a.parent = portfolioNodeName(pid);
      }
    }

    var nodes = list.concat(virtual);
    return {
      nodes: nodes,
      byParent: groupByParent(nodes),
      overseerName: overseerName,
    };
  }

  // Row chrome for a portfolio node: folder glyph, no status dot class.
  function portfolioRowChrome(agent) {
    var a = agent && typeof agent === 'object' ? agent : {};
    var title = String(a.description || '').trim();
    if (!title) {
      var id = portfolioIdFromNodeName(a.name) || String(a.name || '');
      title = id;
    }
    return {
      isPortfolio: true,
      title: title,
      // Marker for hermetic: folder chrome, not agent-dot.running/stopped.
      leadKind: 'folder',
      leadHtml: '<span class="agent-folder" aria-hidden="true">' + FOLDER_ICON + '</span>',
      leadClass: 'agent-folder',
      omitPath: true,
      secondaryHtml: '',
      secondaryKind: '',
    };
  }

  // Lead marker for any tree row: portfolio → folder; else status dot.
  function rowLeadHtml(agent) {
    if (isPortfolioNode(agent)) {
      return portfolioRowChrome(agent).leadHtml;
    }
    var st = String(agent && agent.status != null ? agent.status : '');
    return '<span class="agent-dot ' + escHtml(st) + '"></span>';
  }

  return {
    PORTFOLIO_PREFIX: PORTFOLIO_PREFIX,
    FOLDER_ICON: FOLDER_ICON,
    escHtml: escHtml,
    normalizeList: normalizeList,
    portfolioNodeName: portfolioNodeName,
    portfolioIdFromNodeName: portfolioIdFromNodeName,
    isPortfolioNode: isPortfolioNode,
    normalizePath: normalizePath,
    workdirMatchesMember: workdirMatchesMember,
    memberPaths: memberPaths,
    matchPortfolioId: matchPortfolioId,
    findOverseerName: findOverseerName,
    groupByParent: groupByParent,
    weaveFleetTree: weaveFleetTree,
    portfolioRowChrome: portfolioRowChrome,
    rowLeadHtml: rowLeadHtml,
  };
}));
