// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Provider company icon + extremely condensed model label for fleet agent
// name chrome (🎯T287). DOM-free so Node hermetic tests can require() it.
//
// Rules:
//   - Company comes from the agent's stored provider (claude/bedrock →
//     anthropic, grok → xai, codex → openai). When the provider is missing,
//     the model id is sniffed as a fallback — never the agent name.
//   - Label is the model's family initial plus version, as short as it can
//     be while staying unambiguous: Anthropic Opus 4.8 → O4.8, Sonnet 4.5 →
//     S4.5. Grok has one flavour, so the family letter is dropped: 4.5.
//   - Unknown model → icon alone. We never invent a version the server did
//     not report; the icon still identifies the company.
//   - Unknown company → no prefix at all (row renders exactly as before).

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ModelPrefix = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Company keys (icon + tooltip vocabulary), not provider ids: several
  // providers map to one company (claude and bedrock are both Anthropic).
  const ANTHROPIC = 'anthropic';
  const XAI = 'xai';
  const OPENAI = 'openai';

  const PROVIDER_COMPANY = {
    claude: ANTHROPIC,
    anthropic: ANTHROPIC,
    bedrock: ANTHROPIC,
    grok: XAI,
    xai: XAI,
    codex: OPENAI,
    openai: OPENAI,
  };

  // Family → condensed initial. Grok maps to '' deliberately: one flavour,
  // so the version alone is unambiguous (owner spec: "4.5", not "G4.5").
  const FAMILY_INITIAL = {
    opus: 'O',
    sonnet: 'S',
    haiku: 'H',
    grok: '',
    gpt: '',
  };

  // Company marks, sized by CSS (.model-icon). Anthropic and OpenAI use
  // their published logomark paths; the xAI mark is a two-stroke X drawn
  // here (no vendor asset shipped in this repo).
  const ICON_PATHS = {
    anthropic: '<path fill="currentColor" d="M16.23 5h-3.02l5.507 15h3.02L16.23 5zM7.507 5L2 20h3.08l1.125-3.15h5.761L13.092 20h3.08L10.665 5H7.507zM7.2 14.064l1.885-5.271 1.884 5.271H7.2z"/>',
    xai: '<path fill="currentColor" d="M3 3h4.2l13.8 18h-4.2L3 3zm13.5 0H21l-5.4 7.05-2.25-2.94L16.5 3zM3 21l5.4-7.05 2.25 2.94L7.5 21H3z"/>',
    openai: '<path fill="currentColor" d="M12 2.5a5 5 0 0 1 4.33 2.5A5 5 0 0 1 20.66 12a5 5 0 0 1-4.33 7 5 5 0 0 1-8.66 0A5 5 0 0 1 3.34 12 5 5 0 0 1 7.67 5 5 5 0 0 1 12 2.5zm0 3.6L8.9 7.9v3.05L12 9.15l3.1 1.8v-3.05L12 6.1zM6.3 9.55v3.6L9.4 15v-3.6L6.3 9.55zm11.4 0L14.6 11.4V15l3.1-1.85v-3.6zM8.9 16.1v3.05L12 20.95l3.1-1.8V16.1L12 17.9l-3.1-1.8z"/>',
  };

  const COMPANY_LABEL = {
    anthropic: 'Anthropic',
    xai: 'xAI',
    openai: 'OpenAI',
  };

  // A numeric segment this long is a release date (20260514), not a version.
  const DATE_DIGITS = 6;

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

  // Sniff the company from a model id when the provider is unknown.
  function companyFromModel(model) {
    const m = norm(model);
    if (!m) return '';
    if (/claude|opus|sonnet|haiku/.test(m)) return ANTHROPIC;
    if (/grok/.test(m)) return XAI;
    if (/gpt|codex|^o\d/.test(m)) return OPENAI;
    return '';
  }

  function companyFor(provider, model) {
    const p = norm(provider);
    if (p && PROVIDER_COMPANY[p]) return PROVIDER_COMPANY[p];
    return companyFromModel(model);
  }

  // Model family: the word the version hangs off ('opus', 'grok', …).
  function familyOf(model) {
    const m = norm(model);
    const hit = /(opus|sonnet|haiku|grok|gpt)/.exec(m);
    return hit ? hit[1] : '';
  }

  // Version digits that follow the family, joined with dots: 'opus-4-5-2026…'
  // → '4.5'; 'grok-4.5' → '4.5'; 'opus-5[1m]' → '5'. A release date, a
  // trailing word ('-preview', '-v1'), or any other break ends the version.
  function versionAfter(model, family) {
    const m = norm(model);
    if (!family) return '';
    const idx = m.indexOf(family);
    if (idx < 0) return '';
    // Drop the separator between family and version ('-', '.', ' ', none).
    let tail = m.slice(idx + family.length).replace(/^[^0-9a-z]+/, '');
    const parts = [];
    while (tail) {
      const hit = /^(\d+)/.exec(tail);
      if (!hit || hit[1].length >= DATE_DIGITS) break;
      parts.push(hit[1]);
      tail = tail.slice(hit[1].length);
      if (!/^[.\-_]/.test(tail)) break;
      tail = tail.slice(1);
    }
    return parts.join('.');
  }

  // Extremely condensed model label: family initial + version (🎯T287).
  // Empty when the model is unknown or carries no legible version.
  function condenseModel(model) {
    const m = norm(model);
    if (!m) return '';
    const family = familyOf(m);
    const initial = family && Object.prototype.hasOwnProperty.call(FAMILY_INITIAL, family)
      ? FAMILY_INITIAL[family]
      : '';
    const version = versionAfter(m, family);
    const label = initial + version;
    if (label) return label;
    // Family with no version (bare 'opus') still says which model it is.
    return initial;
  }

  function companyIconHtml(company) {
    const path = ICON_PATHS[company];
    if (!path) return '';
    return '<svg class="model-icon" viewBox="0 0 24 24" aria-hidden="true">' + path + '</svg>';
  }

  // { company, label, title } — the pure model behind the HTML.
  function modelPrefix(agent) {
    const a = agent && typeof agent === 'object' ? agent : {};
    const provider = String(a.provider || '');
    const model = String(a.model || '');
    const company = companyFor(provider, model);
    if (!company) return { company: '', label: '', title: '' };
    const label = condenseModel(model);
    const shown = model || provider;
    const title = (COMPANY_LABEL[company] || company) + (shown ? ' · ' + shown : '');
    return { company: company, label: label, title: title };
  }

  // Prefix chrome painted before the bare agent name. Empty string when the
  // company is unknown, so unwired rows are untouched.
  function modelPrefixHtml(agent) {
    const p = modelPrefix(agent);
    if (!p.company) return '';
    return '<span class="model-badge" data-company="' + escHtml(p.company)
      + '" title="' + escHtml(p.title) + '">'
      + companyIconHtml(p.company)
      + (p.label ? '<sub>' + escHtml(p.label) + '</sub>' : '')
      + '</span>';
  }

  return {
    companyFor: companyFor,
    companyFromModel: companyFromModel,
    familyOf: familyOf,
    condenseModel: condenseModel,
    companyIconHtml: companyIconHtml,
    modelPrefix: modelPrefix,
    modelPrefixHtml: modelPrefixHtml,
    COMPANY_LABEL: COMPANY_LABEL,
  };
}));
