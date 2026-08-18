// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Fleet-tree provider icon menu + "!" band mark (🎯T285.2). DOM-free so
// Node hermetic tests can require() it.
//
// Rules:
//   - The "!" between the provider icon and the agent name is painted from
//     the SERVER's weekly band (GET /api/migrate/options — the same Go
//     WeeklyBandOf the sweep uses), never a client-side classifyPace.
//     Orange = mint-ineligible (ahead); red = migrate-off (hot or
//     exhausted); nothing for ok/under/locked/unpublished. Hover is a
//     native title carrying the server's reason string.
//   - Clicking the provider ICON (not the name) opens a borderless
//     two-column table: provider mark | that provider's model ids
//     left-to-right, best first (server order). Choosing one runs the
//     migrate: POST /api/overseer/migrate for the overseer seat,
//     POST /api/agents/migrate for every other seat. Same-provider model
//     choice is a pin for fleet seats; the overseer cannot re-pin without
//     a rotation, so its same-provider entries are visible but disabled.
//   - Ineligible providers (hot/exhausted/ahead) stay VISIBLE but not
//     choosable, wearing the same reason string as the "!" tooltip.
//     The current provider+model entry is marked current and inert.
//   - Never auto-switch any seat: everything here waits for an explicit
//     owner click.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory(require('./model_prefix.js'));
  } else {
    root.ProviderMenu = factory(root.ModelPrefix);
  }
}(typeof self !== 'undefined' ? self : this, function (ModelPrefix) {
  'use strict';

  // Bands that make a provider's seats leave (red "!") vs merely
  // mint-ineligible (orange "!"). Mirrors planusage.MigrateOff /
  // MintIneligible — the server still decides the band itself.
  const MARK_OFF = 'off';
  const MARK_AHEAD = 'ahead';

  function escHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function norm(s) {
    return String(s == null ? '' : s).trim().toLowerCase();
  }

  // band → '' | 'ahead' (orange) | 'off' (red). The acceptance's mapping:
  // red when migrate-off (hot, exhausted), orange when mint-ineligible
  // but still tolerable (ahead), nothing otherwise.
  function bandMark(band) {
    switch (norm(band)) {
      case 'hot':
      case 'exhausted':
        return MARK_OFF;
      case 'ahead':
        return MARK_AHEAD;
      default:
        return '';
    }
  }

  // Index the GET /api/migrate/options payload by provider.
  function optionsIndex(payload) {
    const out = {};
    const list = payload && Array.isArray(payload.providers) ? payload.providers : [];
    for (let i = 0; i < list.length; i++) {
      const p = norm(list[i] && list[i].provider);
      if (p) out[p] = list[i];
    }
    return out;
  }

  // The "!" chrome between icon and name. Empty when the agent's provider
  // is missing from the payload or its band carries no mark.
  function markHtml(agent, options) {
    const a = agent && typeof agent === 'object' ? agent : {};
    const idx = optionsIndex(options);
    const opt = idx[norm(a.provider)];
    if (!opt) return '';
    const mark = bandMark(opt.band);
    if (!mark) return '';
    return '<span class="prov-mark prov-mark-' + mark
      + '" title="' + escHtml(opt.reason || '') + '">!</span>';
  }

  function isOverseerAgent(agent) {
    return norm(agent && (agent.purpose || agent.role)) === 'overseer';
  }

  // Menu model: one row per provider from the server payload (its order is
  // the server's: seat-bearing first), each with clickable model entries.
  // A provider with no models offers its default (empty model id).
  function menuModel(payload, agent) {
    const a = agent && typeof agent === 'object' ? agent : {};
    const overseer = isOverseerAgent(a);
    const curProv = norm(a.provider);
    const curModel = norm(a.model);
    const list = payload && Array.isArray(payload.providers) ? payload.providers : [];
    const rows = [];
    for (let i = 0; i < list.length; i++) {
      const opt = list[i] || {};
      const provider = norm(opt.provider);
      if (!provider) continue;
      const same = provider === curProv;
      const ineligible = !opt.eligible && !same;
      const ids = Array.isArray(opt.models) && opt.models.length ? opt.models : [''];
      const models = [];
      for (let j = 0; j < ids.length; j++) {
        const id = String(ids[j] || '');
        const current = same && norm(id) === curModel && curModel !== '';
        let disabled = ineligible || current;
        let reason = ineligible ? String(opt.reason || '') : '';
        if (current) reason = 'current model';
        // The overseer cannot re-pin without a rotation; same-provider
        // entries stay visible but inert for that seat.
        if (overseer && same && !current) {
          disabled = true;
          reason = 'same-provider model pin needs a rotation the overseer seat does not support';
        }
        models.push({
          id: id,
          label: modelLabel(provider, id),
          title: id || (String(opt.provider) + ' default'),
          current: current,
          disabled: disabled,
          reason: reason,
        });
      }
      rows.push({
        provider: provider,
        company: ModelPrefix ? ModelPrefix.companyFor(provider, '') : '',
        current: same,
        disabled: ineligible,
        reason: String(opt.reason || ''),
        models: models,
      });
    }
    return rows;
  }

  // Compact chip text: the condensed model label the badge vocabulary
  // already uses (O5, F5, 4.5); full ids only in the title. A provider
  // default (empty id) reads "default".
  function modelLabel(provider, id) {
    if (!id) return 'default';
    const condensed = ModelPrefix ? ModelPrefix.condenseModel(id) : '';
    return condensed || id;
  }

  // Borderless two-column table: column 1 the provider company mark,
  // column 2 that provider's models left-to-right best first. No border
  // classes anywhere — the acceptance greps for their absence.
  function menuHtml(rows) {
    let html = '<table class="prov-menu-table">';
    for (let i = 0; i < (rows ? rows.length : 0); i++) {
      const r = rows[i];
      const icon = (ModelPrefix && r.company)
        ? ModelPrefix.companyIconHtml(r.company)
        : escHtml(r.provider);
      html += '<tr class="prov-menu-row' + (r.disabled ? ' prov-disabled' : '') + '"'
        + ' data-provider="' + escHtml(r.provider) + '"'
        + (r.disabled && r.reason ? ' title="' + escHtml(r.reason) + '"' : '')
        + '><td class="prov-cell-mark">' + icon + '</td><td class="prov-cell-models">';
      for (let j = 0; j < r.models.length; j++) {
        const m = r.models[j];
        html += '<button type="button" class="prov-model'
          + (m.current ? ' prov-current' : '')
          + (m.disabled ? ' prov-disabled' : '') + '"'
          + ' data-provider="' + escHtml(r.provider) + '"'
          + ' data-model="' + escHtml(m.id) + '"'
          + (m.disabled ? ' disabled' : '')
          + ' title="' + escHtml(m.reason || m.title) + '"'
          + '>' + escHtml(m.label) + '</button>';
      }
      html += '</td></tr>';
    }
    return html + '</table>';
  }

  // The wire call for one owner click. Overseer → its own endpoint (chat
  // re-attach lives server-side there); every other seat → the thin
  // fleet wrapper.
  function migrateRequest(agent, provider, model) {
    const a = agent && typeof agent === 'object' ? agent : {};
    if (isOverseerAgent(a)) {
      return {
        path: '/api/overseer/migrate',
        body: { provider: String(provider || ''), model: String(model || '') },
      };
    }
    return {
      path: '/api/agents/migrate',
      body: {
        name: String(a.name || ''),
        provider: String(provider || ''),
        model: String(model || ''),
      },
    };
  }

  // Click routing: the provider ICON opens the menu; the name (and the
  // rest of the row) keeps selecting the agent. classChain is the class
  // strings from the event target up to the row root.
  function clickOpensMenu(classChain) {
    const chain = Array.isArray(classChain) ? classChain : [];
    for (let i = 0; i < chain.length; i++) {
      const cls = ' ' + String(chain[i] || '') + ' ';
      if (cls.indexOf(' model-badge ') !== -1) return true;
      if (cls.indexOf(' model-icon ') !== -1) return true;
      if (cls.indexOf(' prov-mark ') !== -1) return true;
      if (cls.indexOf(' agent-name ') !== -1) return false;
    }
    return false;
  }

  return {
    bandMark: bandMark,
    optionsIndex: optionsIndex,
    markHtml: markHtml,
    isOverseerAgent: isOverseerAgent,
    menuModel: menuModel,
    modelLabel: modelLabel,
    menuHtml: menuHtml,
    migrateRequest: migrateRequest,
    clickOpensMenu: clickOpensMenu,
  };
}));
