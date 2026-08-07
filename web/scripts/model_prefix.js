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
//     Version segments are never zero-padded — '05' is version 5 (🎯T295).
//   - The mark is the product's, not the vendor's letterhead: Claude rows
//     wear the Claude splat, Grok rows the slashed ring (🎯T295 / 🎯T296).
//   - Icon and version subscript read as one word, not two pieces: the badge
//     CSS keeps them adjacent with no gap (🎯T296).
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

  // Company marks, sized by CSS (.model-icon). Each is drawn in currentColor
  // with no plate, ring, or outer border — the row supplies the colour.
  //
  // The Anthropic slot wears **Claude's splat**, not the Anthropic A-wordmark
  // (🎯T295): these rows name a Claude model, and the wordmark reads as the
  // company's letterhead rather than the product. Drawn here as the splat's
  // radiating blades — an approximation, since no vendor asset ships in this
  // repo — but it is unmistakably not the wordmark: a burst, not a letter.
  //
  // xAI is Grok's mark, not the X/Twitter one (🎯T293): the crossed-stroke X
  // named the wrong product entirely. 🎯T296 replaces T293's twin blades — the
  // owner read those as a generic chevron pair — with Grok's ring cut by a
  // single diagonal slash. The slash overshoots the ring on both ends so the
  // cut still reads at 12px, where a contained stroke closes up into a blob.
  const ICON_PATHS = {
    anthropic: '<path fill="none" stroke="currentColor" stroke-width="2.1" stroke-linecap="round" d="M12 12L12 3M12 12L15.89 5.94M12 12L20.19 8.26M12 12L19.13 13.02M12 12L18.8 17.89M12 12L14.03 18.91M12 12L9.46 20.64M12 12L6.56 16.71M12 12L3.09 13.28M12 12L5.45 9.01M12 12L7.13 4.43"/>',
    xai: '<circle cx="12" cy="12" r="8.4" fill="none" stroke="currentColor" stroke-width="2"/><path fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" d="M18.9 5.1L5.1 18.9"/>',
    openai: '<path fill="currentColor" d="M12 2.5a5 5 0 0 1 4.33 2.5A5 5 0 0 1 20.66 12a5 5 0 0 1-4.33 7 5 5 0 0 1-8.66 0A5 5 0 0 1 3.34 12 5 5 0 0 1 7.67 5 5 5 0 0 1 12 2.5zm0 3.6L8.9 7.9v3.05L12 9.15l3.1 1.8v-3.05L12 6.1zM6.3 9.55v3.6L9.4 15v-3.6L6.3 9.55zm11.4 0L14.6 11.4V15l3.1-1.85v-3.6zM8.9 16.1v3.05L12 20.95l3.1-1.8V16.1L12 17.9l-3.1-1.8z"/>',
  };

  // Which mark each slot wears, as a name a test can assert without pinning
  // the geometry: the bug 🎯T295 fixes is "wrong mark", not "wrong curve".
  const MARK_ID = {
    anthropic: 'claude-splat',
    xai: 'grok',
    openai: 'openai',
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

  // Version segments are never zero-padded (🎯T295): a model id that spells a
  // segment '05' is still version 5, and the subscript is small enough that a
  // leading zero reads as a different version entirely. Internal zeros are
  // significant — '10' stays '10', and a lone '0' stays '0'.
  function unpad(digits) {
    return digits.replace(/^0+(?=\d)/, '');
  }

  // Version digits that follow the family, joined with dots: 'opus-4-5-2026…'
  // → '4.5'; 'grok-4.5' → '4.5'; 'opus-5[1m]' → '5'; 'opus-4-05' → '4.5'. A
  // release date, a trailing word ('-preview', '-v1'), or any other break ends
  // the version.
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
      // Length is judged on the raw run: '202605' is a date whether or not it
      // would survive unpadding.
      if (!hit || hit[1].length >= DATE_DIGITS) break;
      parts.push(unpad(hit[1]));
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
    return '<svg class="model-icon" data-mark="' + escHtml(MARK_ID[company] || company)
      + '" viewBox="0 0 24 24" aria-hidden="true">' + path + '</svg>';
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
