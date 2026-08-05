// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Pure RHS fleet-row chrome helpers (🎯T115 / T84 / T118 / T211 / T269).
// DOM-free so Node hermetic tests can require() it.
//
// Rules:
//   - Root overseer state-dir home (~/.jevons/<name>) → no path column
//   - purpose/role aside (side chat) → 💡 title prefix, no path column
//   - Work agents keep compact path + GitHub icon (T84) when path is policy
//   - 🎯T118: same-workdir fan-out workers prefer progress/status secondary
//     over redundant path; path stays for roots/POs (has children) or
//     when workdir differs from parent.
//   - 🎯T211: process-alive (status=running) ≠ turn-busy. Bare "running"
//     progress/status is never chrome as a busy action; phase idle (or no
//     in-flight prompt/tool) → idle/parked; phase working → action line.
//   - 🎯T269: purpose=aside rows expose a hover/focus dismiss × (right);
//     work/PO rows never paint it. Activate → DELETE /api/asides (T152).

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.FleetRow = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  const ASIDE_PURPOSES = new Set(['aside', 'side', 'side-chat', 'file-target']);
  const PROGRESS_MAX = 48;
  // Process / non-action labels: never treat as busy work chrome (🎯T211).
  const PROCESS_LABELS = new Set(['running', 'stopped', 'idle', 'parked']);

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

  function collapse(s) {
    return String(s == null ? '' : s).replace(/\s+/g, ' ').trim();
  }

  function truncate(s, n) {
    s = collapse(s);
    if (!s) return '';
    n = n == null ? PROGRESS_MAX : n;
    return s.length > n ? s.slice(0, n - 1) + '…' : s;
  }

  function isProcessOnlyLabel(text) {
    return PROCESS_LABELS.has(collapse(text).toLowerCase());
  }

  // 🎯T211: busy = turn in flight (working phase and/or real action line).
  // status=running with phase idle (or bare progress "running") is not busy.
  function isBusyAgent(agent) {
    const a = agent && typeof agent === 'object' ? agent : {};
    const phase = collapse(a.phase || '').toLowerCase();
    if (phase === 'working') return true;
    if (phase === 'idle' || phase === 'parked') return false;
    if (phase === 'blocked') return false;
    const step = collapse(a.step || a.last_tool || a.lastTool || '');
    if (step) return true;
    const explicit = collapse(a.progress || a.summary || '');
    if (!explicit) return false;
    if (isProcessOnlyLabel(explicit)) return false;
    const low = explicit.toLowerCase();
    if (low === 'working' || low.indexOf('working') === 0) return true;
    // Non-process secondary (e.g. "working · Bash") counts as busy action.
    if (low.indexOf(' · ') !== -1 || low.indexOf('·') !== -1) return true;
    return false;
  }

  // True when secondary text is a real ACP action (not process liveness).
  function isActionProgressText(text) {
    const t = collapse(text);
    if (!t || isProcessOnlyLabel(t)) return false;
    const low = t.toLowerCase();
    if (low === 'working' || low.indexOf('working') === 0) return true;
    if (low === 'blocked' || low.indexOf('blocked') === 0) return true;
    if (low.indexOf(' · ') !== -1 || low.indexOf('·') !== -1) return true;
    return false;
  }

  // Compare workdirs ignoring trailing slash and trivial home spellings.
  function normalizeWorkdir(d) {
    let s = String(d == null ? '' : d).replace(/\\/g, '/').replace(/\/+$/, '');
    if (!s) return '';
    s = s.replace(/^\/Users\/[^/]+/, '~');
    return s;
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

  // Hard omit path column (aside chrome / overseer state-dir home).
  function shouldOmitPath(agent) {
    if (!agent || typeof agent !== 'object') return true;
    if (isAsidePurpose(agent.purpose || agent.role)) return true;
    if (isStateDirOverseerHome(agent.workdir, agent.name)) return true;
    return false;
  }

  // 🎯T118: path is primary secondary for roots, POs (has children), or
  // distinct workdir; same-workdir leaf workers use progress instead.
  function shouldShowPathSecondary(agent, ctx) {
    if (!agent || typeof agent !== 'object') return false;
    if (shouldOmitPath(agent)) return false;
    ctx = ctx && typeof ctx === 'object' ? ctx : {};
    const parent = String(agent.parent || '').trim();
    if (!parent) return true; // root
    if (ctx.hasChildren) return true; // PO / subtree root establishes path context
    const parentWd = normalizeWorkdir(ctx.parentWorkdir || '');
    const selfWd = normalizeWorkdir(agent.workdir || '');
    if (parentWd && selfWd && parentWd !== selfWd) return true;
    // No parent workdir known → keep path so we never blank the column wrongly
    if (!parentWd && selfWd) return true;
    return false;
  }

  // Glanceable single-line status/progress from API / fixture fields (🎯T211).
  // Bare process labels (running/idle/stopped/parked) never masquerade as actions.
  function formatFleetProgress(agent, maxLen) {
    const a = agent && typeof agent === 'object' ? agent : {};
    maxLen = maxLen == null ? PROGRESS_MAX : maxLen;
    const phaseRaw = collapse(a.phase || '');
    const phase = phaseRaw.toLowerCase();
    const step = collapse(a.step || a.last_tool || a.lastTool || '');
    let explicit = collapse(a.progress || a.summary || '');
    // Strip bare process labels so progress="running" is not an action line.
    if (explicit && isProcessOnlyLabel(explicit)) {
      explicit = '';
    }

    // Working → show action (explicit progress or phase · step).
    if (phase === 'working') {
      if (explicit) return truncate(explicit, maxLen);
      if (step) return truncate('working · ' + step, maxLen);
      return 'working';
    }

    // Blocked → not busy-work chrome, but still a named state with optional step.
    if (phase === 'blocked') {
      if (explicit) return truncate(explicit, maxLen);
      if (step) return truncate('blocked · ' + step, maxLen);
      return 'blocked';
    }

    // Explicit non-process progress (API already composed "working · tool").
    if (explicit) {
      if (isActionProgressText(explicit)) return truncate(explicit, maxLen);
      return truncate(explicit, maxLen);
    }

    // Idle / parked / empty phase: process may still be status=running.
    if (phase === 'idle' || phase === 'parked' || phase === '') {
      const st = collapse(a.status || '').toLowerCase();
      if (st === 'stopped') return 'stopped';
      // Process-alive alone → idle (not "running").
      if (st === 'running' || phase === 'idle' || phase === 'parked') {
        return phase === 'parked' ? 'parked' : 'idle';
      }
      if (step) return truncate(step, maxLen);
      return phase === 'parked' ? 'parked' : (phase === 'idle' ? 'idle' : '');
    }

    if (phase && step) return truncate(phaseRaw + ' · ' + step, maxLen);
    if (step) return truncate(step, maxLen);
    if (phaseRaw) return truncate(phaseRaw, maxLen);

    const st = collapse(a.status || '').toLowerCase();
    if (st === 'running') return 'idle'; // 🎯T211: process-alive ≠ busy
    if (st === 'stopped') return 'stopped';
    if (st === 'blocked' || st === 'idle' || st === 'working' || st === 'parked') return st;
    return '';
  }

  function asideTitle(title) {
    const t = String(title == null ? '' : title).replace(/^\s+|\s+$/g, '') || 'aside';
    if (/^💡\s*/.test(t)) return t.replace(/^💡\s*/, '💡 ');
    return '💡 ' + t;
  }

  // 🎯T269: dismiss × only on purpose=aside (not work/PO/portfolio/overseer).
  function shouldShowAsideDismiss(agent) {
    if (!agent || typeof agent !== 'object') return false;
    if (agent.is_portfolio || agent.purpose === 'portfolio') return false;
    return isAsidePurpose(agent.purpose || agent.role);
  }

  // Pure HTML for the hover-gated dismiss control (wired in index renderTree).
  // Activate → dismissFleetAside → DELETE /api/asides/{id} (T152).
  function asideDismissButtonHtml(name) {
    const n = String(name == null ? '' : name).trim();
    if (!n) return '';
    const safe = escHtml(n);
    return '<button type="button" class="agent-dismiss" data-agent-dismiss="'
      + safe
      + '" aria-label="Dismiss aside '
      + safe
      + '" title="Dismiss">\u00d7</button>';
  }

  // Product path descriptor for hermetics (matches dismissFleetAside wire).
  function asideDismissPath(name) {
    const n = String(name == null ? '' : name).trim();
    if (!n) return '';
    return '/api/asides/' + encodeURIComponent(n);
  }

  // ctx: { parentWorkdir, hasChildren }
  function fleetRowModel(agent, ctx) {
    const a = agent && typeof agent === 'object' ? agent : {};
    ctx = ctx && typeof ctx === 'object' ? ctx : {};
    const purpose = a.purpose || a.role || '';
    const isAside = isAsidePurpose(purpose);
    const showDismiss = shouldShowAsideDismiss(a);
    const omitPath = shouldOmitPath(a);
    const showPath = shouldShowPathSecondary(a, ctx);
    const baseLabel = String(a.description || a.name || '').trim() || (isAside ? 'aside' : '');
    const title = isAside ? asideTitle(baseLabel) : baseLabel;
    const progressText = isAside ? '' : formatFleetProgress(a);
    const busy = !isAside && isBusyAgent(a);
    // Rich action chrome only for real ACP actions — not phase=idle or bare running.
    const hasAction = !!(
      busy ||
      isActionProgressText(progressText) ||
      (collapse(a.phase || '').toLowerCase() === 'working')
    );

    let secondaryHtml = '';
    let secondaryKind = '';
    if (isAside) {
      // Aside: title only (T115); dismiss × is separate chrome (T269).
    } else if (showPath) {
      secondaryHtml = formatAgentDir(a.workdir || '');
      secondaryKind = secondaryHtml ? 'path' : '';
    } else if (isStateDirOverseerHome(a.workdir, a.name)) {
      // Overseer home: no path; only non-trivial ACP action (no bare running/idle).
      if (hasAction && progressText && isActionProgressText(progressText)) {
        secondaryHtml = escHtml(progressText);
        secondaryKind = 'progress';
      }
    } else if (progressText) {
      secondaryHtml = escHtml(progressText);
      // Busy action → progress; process-only idle/stopped → status (not busy).
      secondaryKind = hasAction && isActionProgressText(progressText) ? 'progress' : 'status';
    }

    // Legacy omitPath: true when no path chrome is emitted (aside/overseer or worker progress).
    const dirHtml = secondaryKind === 'path' ? secondaryHtml : '';
    const dismissHtml = showDismiss ? asideDismissButtonHtml(a.name) : '';

    return {
      name: String(a.name || ''),
      title: title,
      isAside: isAside,
      showDismiss: showDismiss,
      dismissHtml: dismissHtml,
      omitPath: omitPath || secondaryKind !== 'path',
      showPath: showPath && secondaryKind === 'path',
      dirHtml: dirHtml,
      secondaryHtml: secondaryHtml,
      secondaryKind: secondaryKind,
      progressText: progressText,
      busy: busy,
      purpose: String(purpose || ''),
    };
  }

  return {
    escHtml: escHtml,
    shortDir: shortDir,
    formatAgentDir: formatAgentDir,
    normalizeWorkdir: normalizeWorkdir,
    isAsidePurpose: isAsidePurpose,
    isStateDirOverseerHome: isStateDirOverseerHome,
    shouldOmitPath: shouldOmitPath,
    shouldShowPathSecondary: shouldShowPathSecondary,
    shouldShowAsideDismiss: shouldShowAsideDismiss,
    isProcessOnlyLabel: isProcessOnlyLabel,
    isBusyAgent: isBusyAgent,
    isActionProgressText: isActionProgressText,
    formatFleetProgress: formatFleetProgress,
    asideTitle: asideTitle,
    asideDismissButtonHtml: asideDismissButtonHtml,
    asideDismissPath: asideDismissPath,
    fleetRowModel: fleetRowModel,
    PROGRESS_MAX: PROGRESS_MAX,
  };
}));
