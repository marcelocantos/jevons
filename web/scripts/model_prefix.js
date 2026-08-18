// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Provider company icon + extremely condensed model label for fleet agent
// name chrome (🎯T287). DOM-free so Node hermetic tests can require() it.
//
// Rules:
//   - Company comes from the agent's stored provider (claude/bedrock →
//     anthropic, grok → xai, codex → openai). When the provider is missing,
//     the model id is sniffed as a fallback — never the agent name.
//   - Label is the Anthropic family initial then the version: Opus 5 → O5,
//     Sonnet 4.5 → S4.5, Haiku → H. 🎯T299 deleted the initial to stop 'O5'
//     reading as '05'; 🎯T302 restores it, because the O was never a zero —
//     it is Opus, and dropping it made Opus 4.5 and Sonnet 4.5 the same badge.
//     The ambiguity is settled by *styling*: the initial is painted bold in
//     the --accent token, so it reads as a letter beside plain digits rather
//     than as another numeral. xAI keeps the bare version (one Grok flavour).
//   - Version is found on either side of the family word (🎯T302): the newer
//     ids trail it ('claude-opus-5'), the 3-series lead with it
//     ('claude-3-5-sonnet'). Reading only the tail lost the whole version for
//     the latter and painted a bare mark.
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
//   - An Anthropic family word we have never heard of must not erase the
//     badge (🎯T348): 'claude-fable-5' painted a bare splat for exactly as
//     long as 'fable' was missing from FAMILIES (🎯T312), and the next family
//     rename would do it again. A 'claude-…' id whose family word is not in
//     the list sniffs the first alphabetic token after 'claude' as the family
//     — initial from its first letter — and a family-less id ('claude-2')
//     paints the bare version. The subscript only ever repeats what the id
//     says; it is the FAMILIES list that stops being load-bearing.
//   - Unknown company → no prefix at all (row renders exactly as before).
//   - Provider wins for the company mark; family initial + version paint
//     only when the model id belongs to that company (🎯T323). Sticky
//     migrate residue (fable under provider=grok) fails closed to mark-only
//     — never Grok mark + Anthropic F, never a foreign version.

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

  // Model families the version hangs off. 🎯T312 adds Anthropic's fable line:
  // an unknown family word condenses to nothing, so a `claude-fable-5` row wore
  // the splat with no version beside it at all.
  const FAMILIES = ['opus', 'sonnet', 'haiku', 'fable', 'grok', 'gpt'];
  const FAMILY_RE = new RegExp('(' + FAMILIES.join('|') + ')');

  // The initial painted ahead of the version (🎯T302). Anthropic ships three
  // families at every version, so the letter is the only thing telling an
  // Opus row from a Sonnet row at a glance. xAI and OpenAI ship one family
  // each, so their initial would be pure noise — they stay bare, as 🎯T299
  // left them.
  const FAMILY_INITIAL = { opus: 'O', sonnet: 'S', haiku: 'H', fable: 'F' };

  // Company marks, sized by CSS (.model-icon), drawn in currentColor with no
  // plate, ring, or outer border — the CSS supplies the colour, keyed on the
  // data-mark below: the muted row colour for Grok, the Claude brand orange
  // for the splat (🎯T302). Orange rides the strokes over transparent ground;
  // the vendor's usual orange-field-with-light-mark lockup is inverted here,
  // so the badge stays a glyph on nothing like every other row. Each mark is
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
  //   openai — ChatGPT, from Wikimedia Commons (public-domain simple
  //     geometry), verbatim path data and source viewBox:
  //     https://commons.wikimedia.org/wiki/File:ChatGPT-Logo.svg
  //
  // Trademarks remain their owners'; the path data is CC0/public-domain and
  // used to name the model a row is running.
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
      mark: 'chatgpt',
      viewBox: '0 0 320 320',
      path: '<path fill="currentColor" d="m297.06 130.97c7.26-21.79 4.76-45.66-6.85-65.48-17.46-30.4-52.56-46.04-86.84-38.68-15.25-17.18-37.16-26.95-60.13-26.81-35.04-.08-66.13 22.48-76.91 55.82-22.51 4.61-41.94 18.7-53.31 38.67-17.59 30.32-13.58 68.54 9.92 94.54-7.26 21.79-4.76 45.66 6.85 65.48 17.46 30.4 52.56 46.04 86.84 38.68 15.24 17.18 37.16 26.95 60.13 26.8 35.06.09 66.16-22.49 76.94-55.86 22.51-4.61 41.94-18.7 53.31-38.67 17.57-30.32 13.55-68.51-9.94-94.51zm-120.28 168.11c-14.03.02-27.62-4.89-38.39-13.88.49-.26 1.34-.73 1.89-1.07l63.72-36.8c3.26-1.85 5.26-5.32 5.24-9.07v-89.83l26.93 15.55c.29.14.48.42.52.74v74.39c-.04 33.08-26.83 59.9-59.91 59.97zm-128.84-55.03c-7.03-12.14-9.56-26.37-7.15-40.18.47.28 1.3.79 1.89 1.13l63.72 36.8c3.23 1.89 7.23 1.89 10.47 0l77.79-44.92v31.1c.02.32-.13.63-.38.83l-64.41 37.19c-28.69 16.52-65.33 6.7-81.92-21.95zm-16.77-139.09c7-12.16 18.05-21.46 31.21-26.29 0 .55-.03 1.52-.03 2.2v73.61c-.02 3.74 1.98 7.21 5.23 9.06l77.79 44.91-26.93 15.55c-.27.18-.61.21-.91.08l-64.42-37.22c-28.63-16.58-38.45-53.21-21.95-81.89zm221.26 51.49-77.79-44.92 26.93-15.54c.27-.18.61-.21.91-.08l64.42 37.19c28.68 16.57 38.51 53.26 21.94 81.94-7.01 12.14-18.05 21.44-31.2 26.28v-75.81c.03-3.74-1.96-7.2-5.2-9.06zm26.8-40.34c-.47-.29-1.3-.79-1.89-1.13l-63.72-36.8c-3.23-1.89-7.23-1.89-10.47 0l-77.79 44.92v-31.1c-.02-.32.13-.63.38-.83l64.41-37.16c28.69-16.55 65.37-6.7 81.91 22 6.99 12.12 9.52 26.31 7.15 40.1zm-168.51 55.43-26.94-15.55c-.29-.14-.48-.42-.52-.74v-74.39c.02-33.12 26.89-59.96 60.01-59.94 14.01 0 27.57 4.92 38.34 13.88-.49.26-1.33.73-1.89 1.07l-63.72 36.8c-3.26 1.85-5.26 5.31-5.24 9.06l-.04 89.79zm14.63-31.54 34.65-20.01 34.65 20v40.01l-34.65 20-34.65-20z"/>',
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
    if (/claude|opus|sonnet|haiku|fable/.test(m)) return ANTHROPIC;
    if (/grok/.test(m)) return XAI;
    if (/gpt|codex|^o\d/.test(m)) return OPENAI;
    return '';
  }

  function companyFor(provider, model) {
    const p = norm(provider);
    if (p && PROVIDER_COMPANY[p]) return PROVIDER_COMPANY[p];
    return companyFromModel(model);
  }

  // Model family: the word the version hangs off ('opus', 'grok', …). When
  // no listed family matches a 'claude-…' id, the family is sniffed from the
  // id itself (🎯T348) — a brand-new Anthropic family name must degrade to
  // its own initial, never to a bare mark. See claudeFamilySniff.
  function familyOf(model) {
    const m = norm(model);
    const hit = FAMILY_RE.exec(m);
    return hit ? hit[1] : claudeFamilySniff(m);
  }

  // The first alphabetic token after 'claude' in the id: that position is
  // where every Anthropic spelling to date has put the family word
  // ('claude-fable-5', 'us.anthropic.claude-opus-4-5-v1:0'). Digit runs
  // (versions, dates) and revision markers ('v1') are stepped over; an id
  // with no such token ('claude-2') has no family and condenses to its bare
  // version. Empty for non-Claude ids — sniffing stays an Anthropic affair.
  function claudeFamilySniff(m) {
    const idx = m.indexOf('claude');
    if (idx < 0) return '';
    const tokens = m.slice(idx + 'claude'.length).split(/[^0-9a-z]+/);
    for (let i = 0; i < tokens.length; i++) {
      const t = tokens[i];
      if (!t || /^\d/.test(t) || /^v\d+$/.test(t)) continue;
      return t;
    }
    return '';
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

  // Segments read off the front of a string, joined with dots: '4-5-20250929'
  // → '4.5'. A release date, a word ('preview', 'v1'), or any other break ends
  // the version.
  function segmentsFrom(s) {
    let tail = s;
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

  // The version a model id hangs on its family word, from whichever side it
  // sits (🎯T302). Anthropic has spelled it both ways: 'claude-opus-5' trails
  // the family, 'claude-3-5-sonnet' leads with it. Reading only the tail —
  // what 🎯T287 shipped — returned '' for the whole 3-series, which is why
  // some Claude rows reached the owner as a bare mark with no version.
  // The tail wins when both sides carry digits, since that is where the
  // current naming puts it.
  function versionNear(model, family) {
    const m = norm(model);
    if (!family) return '';
    const idx = m.indexOf(family);
    if (idx < 0) return '';
    // Drop the separator between family and version ('-', '.', ' ', none).
    const after = segmentsFrom(m.slice(idx + family.length).replace(/^[^0-9a-z]+/, ''));
    if (after) return after;
    // Nothing after: take the run that butts up against the family word from
    // the left, separator and all — 'claude-3-5-sonnet' → '3-5' → '3.5'.
    const before = /(\d+(?:[.\-_]\d+)*)[^0-9a-z]*$/.exec(m.slice(0, idx));
    return before ? segmentsFrom(before[1]) : '';
  }

  // The version alone, no family letter: 'claude-opus-5' → '5'. A Claude id
  // with no family word at all ('claude-2') hangs its version off 'claude'
  // itself (🎯T348) — the version is real and painting it beats a bare mark.
  function versionOf(model) {
    const m = norm(model);
    if (!m) return '';
    const family = familyOf(m);
    if (family) return versionNear(m, family);
    return versionNear(m, 'claude');
  }

  // The letter painted ahead of the version: 'claude-opus-5' → 'O' (🎯T302).
  // A sniffed family not in the map takes its own first letter (🎯T348);
  // listed non-Anthropic families (grok, gpt) stay deliberately bare — one
  // family per company, the letter would be noise (🎯T299).
  function familyInitial(model) {
    const family = familyOf(model);
    if (!family) return '';
    if (Object.prototype.hasOwnProperty.call(FAMILY_INITIAL, family)) {
      return FAMILY_INITIAL[family];
    }
    if (FAMILIES.indexOf(family) !== -1) return '';
    return family.charAt(0).toUpperCase();
  }

  // Extremely condensed model label: family initial + version (🎯T287 /
  // 🎯T302) — 'O5', 'S4.5', '4.5' for Grok. Empty when the model is unknown
  // in both halves; the mark alone still names the company and the tooltip
  // carries the full id. A family with no version paints the bare letter —
  // that is read off the id, not invented, and it is the half that separates
  // Opus from Sonnet.
  function condenseModel(model) {
    const m = norm(model);
    if (!m) return '';
    return familyInitial(m) + versionOf(m);
  }

  function companyIconHtml(company) {
    const icon = ICONS[company];
    if (!icon) return '';
    return '<svg class="model-icon" data-mark="' + escHtml(icon.mark)
      + '" viewBox="' + escHtml(icon.viewBox) + '" aria-hidden="true">'
      + icon.path + '</svg>';
  }

  // True when the model id is empty, unrecognised, or sniffs as the same
  // company the badge mark names (🎯T323). Provider wins for the mark;
  // family/version only paint when the model belongs to that company —
  // otherwise migrate residue (fable under grok) would paint Grok + F.
  function modelMatchesCompany(company, model) {
    if (!model || !company) return true;
    const fromModel = companyFromModel(model);
    return !fromModel || fromModel === company;
  }

  // { company, initial, version, label, title } — the pure model behind the
  // HTML. Initial and version are kept apart because they are painted apart:
  // the letter gets its own weight and colour (🎯T302).
  function modelPrefix(agent) {
    const a = agent && typeof agent === 'object' ? agent : {};
    const provider = String(a.provider || '');
    const model = String(a.model || '');
    const company = companyFor(provider, model);
    if (!company) return { company: '', initial: '', version: '', label: '', title: '' };
    // 🎯T323 fail-closed: foreign model under this company's mark → mark only.
    const matched = modelMatchesCompany(company, model);
    const initial = matched ? familyInitial(model) : '';
    const version = matched ? versionOf(model) : '';
    // Tooltip: never advertise a foreign model id beside the wrong mark.
    const shown = matched && model ? model : provider;
    const title = (COMPANY_LABEL[company] || company) + (shown ? ' · ' + shown : '');
    return {
      company: company, initial: initial, version: version,
      label: initial + version, title: title,
    };
  }

  // Prefix chrome painted before the bare agent name. Empty string when the
  // company is unknown, so unwired rows are untouched.
  function modelPrefixHtml(agent) {
    const p = modelPrefix(agent);
    if (!p.company) return '';
    // The initial is its own element so the CSS can weight and colour it
    // apart from the digits — that separation is what stops 'O5' reading as
    // '05' now the letter is back (🎯T302).
    const sub = (p.initial ? '<span class="model-family">' + escHtml(p.initial) + '</span>' : '')
      + escHtml(p.version);
    const name = String(agent && agent.name || '').trim();
    const aria = 'Select provider and model' + (name ? ' for ' + name : '')
      + '. Current: ' + p.title;
    return '<button type="button" class="model-badge" data-company="' + escHtml(p.company)
      + '" title="' + escHtml(p.title) + '" aria-label="' + escHtml(aria) + '">'
      + companyIconHtml(p.company)
      + (sub ? '<sub>' + sub + '</sub>' : '')
      + '</button>';
  }

  return {
    companyFor: companyFor,
    companyFromModel: companyFromModel,
    modelMatchesCompany: modelMatchesCompany,
    familyOf: familyOf,
    familyInitial: familyInitial,
    versionOf: versionOf,
    condenseModel: condenseModel,
    companyIconHtml: companyIconHtml,
    modelPrefix: modelPrefix,
    modelPrefixHtml: modelPrefixHtml,
    COMPANY_LABEL: COMPANY_LABEL,
  };
}));
