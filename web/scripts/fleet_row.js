// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure RHS fleet-row chrome helpers (🎯T115 / T84).
// DOM-free so Node hermetic tests can require() it.
//
// Rules:
//   - Root overseer state-dir home (~/.jevons/<name>) → no path column
//   - purpose/role aside (side chat) → 💡 title prefix, no path column
//   - Work agents keep compact path + GitHub icon (T84)

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.FleetRow = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const ASIDE_PURPOSES = new Set(['aside', 'side', 'side-chat', 'file-target']);

  function escHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function escapeRegExp(s) {
    return String(s).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  }

  // 🎯T84: compact workdir for RHS; github.com paths get an inline GitHub icon
  // instead of a bare ~ prefix.
  function shortDir(d) {
    const s = String(d || '');
    const gh = /(?:^|\/)(?:Users\/[^/]+\/)?work\/github\.com\/(.+)$/.exec(s)
      || /github\.com\/(.+)$/.exec(s);
    if (gh) {
      return { html: true, text: gh[1], github: true };
    }
    const home = s.replace(/^\/Users\/[^/]+/, '~');
    return { html: false, text: home };
  }

  function formatAgentDir(d) {
    const info = shortDir(d);
    if (!info.text) return '';
    if (info.github) {
      const icon = '<svg class="gh-icon" viewBox="0 0 16 16" aria-hidden="true"><path fill="currentColor" d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>';
      return icon + escHtml(info.text);
    }
    return escHtml(info.text);
  }

  function isAsidePurpose(purpose) {
    const p = String(purpose == null ? '' : purpose).trim().toLowerCase();
    return ASIDE_PURPOSES.has(p);
  }

  // State-dir overseer home: …/.jevons/<agentName>
  function isStateDirOverseerHome(workdir, name) {
    const n = String(name == null ? '' : name).trim();
    if (!n) return false;
    const s = String(workdir == null ? '' : workdir).replace(/\\/g, '/').replace(/\/+$/, '');
    if (!s) return false;
    const re = new RegExp('(?:^|/)\\.jevons/' + escapeRegExp(n) + '$');
    return re.test(s);
  }

  function shouldOmitPath(agent) {
    if (!agent || typeof agent !== 'object') return true;
    if (isAsidePurpose(agent.purpose || agent.role)) return true;
    if (isStateDirOverseerHome(agent.workdir, agent.name)) return true;
    return false;
  }

  function asideTitle(title) {
    const t = String(title == null ? '' : title).replace(/^\s+|\s+$/g, '') || 'aside';
    if (/^💡\s*/.test(t)) return t.replace(/^💡\s*/, '💡 ');
    return '💡 ' + t;
  }

  function fleetRowModel(agent) {
    const a = agent && typeof agent === 'object' ? agent : {};
    const purpose = a.purpose || a.role || '';
    const isAside = isAsidePurpose(purpose);
    const omitPath = shouldOmitPath(a);
    const baseLabel = String(a.description || a.name || '').trim() || (isAside ? 'aside' : '');
    const title = isAside ? asideTitle(baseLabel) : baseLabel;
    const dirHtml = omitPath ? '' : formatAgentDir(a.workdir || '');
    return {
      name: String(a.name || ''),
      title: title,
      isAside: isAside,
      omitPath: omitPath,
      dirHtml: dirHtml,
      purpose: String(purpose || ''),
    };
  }

  return {
    escHtml: escHtml,
    shortDir: shortDir,
    formatAgentDir: formatAgentDir,
    isAsidePurpose: isAsidePurpose,
    isStateDirOverseerHome: isStateDirOverseerHome,
    shouldOmitPath: shouldOmitPath,
    asideTitle: asideTitle,
    fleetRowModel: fleetRowModel,
  };
}));
