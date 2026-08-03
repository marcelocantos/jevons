// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure RHS domain-portfolio chrome helpers (🎯T200).
// DOM-free so Node hermetic tests can require() it.
//
// Declarative portfolios group repos/products for one owner-visible surface
// without a standing GM agent. Empty/missing → no chrome (calm).

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.PortfolioGroup = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

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

  // Build display models for portfolio group chrome.
  // Empty/missing → [] (caller renders no headers — calm).
  function portfolioModels(payload) {
    const list = normalizeList(payload);
    const out = [];
    for (let i = 0; i < list.length; i++) {
      const p = list[i] && typeof list[i] === 'object' ? list[i] : null;
      if (!p) continue;
      const id = String(p.id || '').trim();
      if (!id) continue;
      const name = String(p.name || id).trim() || id;
      const membersIn = Array.isArray(p.members) ? p.members : [];
      const members = [];
      let agentCount = 0;
      for (let j = 0; j < membersIn.length; j++) {
        const m = membersIn[j] && typeof membersIn[j] === 'object' ? membersIn[j] : null;
        if (!m) continue;
        const path = String(m.path || '').trim();
        const label = String(m.label || '').trim() || path.split('/').pop() || path;
        const agents = Array.isArray(m.agents) ? m.agents.map(String) : [];
        agentCount += agents.length;
        members.push({
          path: path,
          label: label,
          agents: agents,
          agentCount: agents.length,
        });
      }
      out.push({
        id: id,
        name: name,
        members: members,
        memberCount: members.length,
        agentCount: agentCount,
      });
    }
    return out;
  }

  // HTML for one portfolio group (header + member rows). Pure string; no DOM.
  function renderPortfolioGroupHtml(model) {
    if (!model || !model.id) return '';
    const title = escHtml(model.name || model.id);
    const id = escHtml(model.id);
    let body = '';
    const members = Array.isArray(model.members) ? model.members : [];
    if (members.length === 0) {
      body = '<div class="portfolio-member portfolio-empty">no members</div>';
    } else {
      for (let i = 0; i < members.length; i++) {
        const m = members[i];
        const label = escHtml(m.label || m.path || 'member');
        const n = m.agentCount != null ? m.agentCount : (m.agents ? m.agents.length : 0);
        const meta = n === 1 ? '1 agent' : (n + ' agents');
        body += '<div class="portfolio-member" data-path="' + escHtml(m.path || '') + '">'
          + '<span class="portfolio-member-label">' + label + '</span>'
          + '<span class="portfolio-member-meta">' + escHtml(meta) + '</span>'
          + '</div>';
      }
    }
    return '<div class="portfolio-group" data-portfolio="' + id + '">'
      + '<div class="portfolio-header" title="Domain portfolio">'
      + '<span class="portfolio-title">' + title + '</span>'
      + '<span class="portfolio-count">' + members.length + '</span>'
      + '</div>'
      + '<div class="portfolio-members">' + body + '</div>'
      + '</div>';
  }

  // Concatenate all groups. Empty models → '' (calm: no chrome).
  function renderPortfoliosHtml(payload) {
    const models = portfolioModels(payload);
    if (!models.length) return '';
    let html = '<div class="portfolio-panel" aria-label="Domain portfolios">';
    for (let i = 0; i < models.length; i++) {
      html += renderPortfolioGroupHtml(models[i]);
    }
    html += '</div>';
    return html;
  }

  return {
    escHtml: escHtml,
    normalizeList: normalizeList,
    portfolioModels: portfolioModels,
    renderPortfolioGroupHtml: renderPortfolioGroupHtml,
    renderPortfoliosHtml: renderPortfoliosHtml,
  };
}));
