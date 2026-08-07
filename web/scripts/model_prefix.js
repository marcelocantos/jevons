// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Provider company icon + extremely condensed model label for fleet agent
// name chrome (🎯T287). DOM-free so Node hermetic tests can require() it.
//
// Rules:
//   - Company comes from the agent's stored provider (claude/bedrock →
//     anthropic, grok → xai, codex → openai). When the provider is missing,
//     the model id is sniffed as a fallback — never the agent name.
//   - Label is the bare version, no family letter: Opus 4.8 → 4.8, Grok 4.5
//     → 4.5. The initial was dropped in 🎯T299 — a capital O butted against
//     digits reads as a zero, so 'O5' was landing on the owner's screen as
//     '05'. The mark already says whose model it is; the tooltip carries the
//     full id when the family matters.
//   - Version segments are integers, not digit runs: each is parsed with
//     parseInt and re-stringified, so '05' is 5 and '4-05' is 4.5 (🎯T295 /
//     🎯T299). Internal zeros survive — '10' is ten.
//   - The mark is the product's, not the vendor's letterhead, and it is the
//     real brand geometry rather than an approximation of it (🎯T299).
//   - Icon and version subscript read as one word, not two pieces: the badge
//     CSS keeps them adjacent with no gap, and the subscript hangs below the
//     mark's foot as a true subscript (🎯T296 / 🎯T299).
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

  // Model families the version hangs off. No family initial is painted
  // (🎯T299) — see the header note on 'O' reading as a zero.
  const FAMILIES = ['opus', 'sonnet', 'haiku', 'grok', 'gpt'];
  const FAMILY_RE = new RegExp('(' + FAMILIES.join('|') + ')');

  // Company marks, sized by CSS (.model-icon), drawn in currentColor with no
  // plate, ring, or outer border — the row supplies the colour. Each mark is
  // the *product's*, not the vendor's letterhead: an Anthropic row names a
  // Claude model, so it wears the Claude splat, not the A-wordmark (🎯T295),
  // and an xAI row wears Grok's mark, not X/Twitter's (🎯T293).
  //
  // 🎯T299 replaces T295/T296's hand-drawn approximations — a radial burst and
  // a slashed ring, both invented here because no vendor asset shipped in the
  // repo — with the real brand geometry, unaltered:
  //
  //   anthropic — Claude, from Simple Icons (CC0-1.0), verbatim path data:
  //     https://github.com/simple-icons/simple-icons/blob/develop/icons/claude.svg
  //     Its own viewBox is 0 0 24 24 and the glyph fills it edge to edge.
  //   xai — Grok, the white mark lifted from the Grok app icon on Wikimedia
  //     Commons (CC0, "Own work" by Usertoop), verbatim path data:
  //     https://commons.wikimedia.org/wiki/File:Grok-icon.svg
  //     The source art centres the mark inside a 512-unit rounded plate; the
  //     plate is dropped and the viewBox tightened to the mark's own bounding
  //     box (68.09,74.42 375.81x360.79) squared and padded, so the glyph fills
  //     the 12px badge as fully as the Claude splat does.
  //   openai — still the local geometric stand-in: OpenAI rows are not in the
  //     fleet today and Simple Icons ships no openai mark to copy.
  //
  // Trademarks remain their owners'; the path data is CC0 and used to name the
  // model a row is running.
  const ICONS = {
    anthropic: {
      mark: 'claude-splat',
      viewBox: '0 0 24 24',
      path: '<path fill="currentColor" d="m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2.55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607.8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2.4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091.255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828.2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1.3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.8396-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.6393-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.1275.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2.3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.3886-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.9446-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9064-1.3114Z"/>',
    },
    xai: {
      mark: 'grok',
      viewBox: '66 64.82 380 380',
      path: '<path fill="currentColor" d="M213.235 306.019l178.976-180.002v.169l51.695-51.763c-.924 1.32-1.86 2.605-2.785 3.89-39.281 54.164-58.46 80.649-43.07 146.922l-.09-.101c10.61 45.11-.744 95.137-37.398 131.836-46.216 46.306-120.167 56.611-181.063 14.928l42.462-19.675c38.863 15.278 81.392 8.57 111.947-22.03 30.566-30.6 37.432-75.159 22.065-112.252-2.92-7.025-11.67-8.795-17.792-4.263l-124.947 92.341zm-25.786 22.437l-.033.034L68.094 435.217c7.565-10.429 16.957-20.294 26.327-30.149 26.428-27.803 52.653-55.359 36.654-94.302-21.422-52.112-8.952-113.177 30.724-152.898 41.243-41.254 101.98-51.661 152.706-30.758 11.23 4.172 21.016 10.114 28.638 15.639l-42.359 19.584c-39.44-16.563-84.629-5.299-112.207 22.313-37.298 37.308-44.84 102.003-1.128 143.81z"/>',
    },
    openai: {
      mark: 'openai',
      viewBox: '0 0 24 24',
      path: '<path fill="currentColor" d="M12 2.5a5 5 0 0 1 4.33 2.5A5 5 0 0 1 20.66 12a5 5 0 0 1-4.33 7 5 5 0 0 1-8.66 0A5 5 0 0 1 3.34 12 5 5 0 0 1 7.67 5 5 5 0 0 1 12 2.5zm0 3.6L8.9 7.9v3.05L12 9.15l3.1 1.8v-3.05L12 6.1zM6.3 9.55v3.6L9.4 15v-3.6L6.3 9.55zm11.4 0L14.6 11.4V15l3.1-1.85v-3.6zM8.9 16.1v3.05L12 20.95l3.1-1.8V16.1L12 17.9l-3.1-1.8z"/>',
    },
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
    const hit = FAMILY_RE.exec(norm(model));
    return hit ? hit[1] : '';
  }

  // A version segment is a number, not a digit run (🎯T295 / 🎯T299): parse it
  // and print it back, so '05' is 5 whatever the model id spells. The
  // subscript is small enough that a leading zero reads as a different version
  // entirely. Internal zeros are significant — '10' is ten, and a lone '0' is
  // a real segment.
  function segment(digits) {
    const n = parseInt(digits, 10);
    return Number.isNaN(n) ? '' : String(n);
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
      parts.push(segment(hit[1]));
      tail = tail.slice(hit[1].length);
      if (!/^[.\-_]/.test(tail)) break;
      tail = tail.slice(1);
    }
    return parts.join('.');
  }

  // Extremely condensed model label: the bare version (🎯T287 / 🎯T299).
  // Empty when the model is unknown or carries no legible version — the mark
  // alone still names the company, and the tooltip carries the full id.
  function condenseModel(model) {
    const m = norm(model);
    if (!m) return '';
    return versionAfter(m, familyOf(m));
  }

  function companyIconHtml(company) {
    const icon = ICONS[company];
    if (!icon) return '';
    return '<svg class="model-icon" data-mark="' + escHtml(icon.mark)
      + '" viewBox="' + escHtml(icon.viewBox) + '" aria-hidden="true">'
      + icon.path + '</svg>';
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
