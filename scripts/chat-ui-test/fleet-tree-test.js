// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic oracle for RHS fleet who-started-whom tree (🎯T68) and list
// completeness wiring (🎯T72.1 client side). Stubs /api/agents, drives
// refreshAgents/renderTree, asserts hierarchy + stable sibling name order.
//
//   node scripts/chat-ui-test/fleet-tree-test.js [--headed]

'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const playwrightRoot = path.join(__dirname, '..', 'browser-loop-test', 'node_modules', 'playwright');
const { chromium } = require(playwrightRoot);

const HEADED = process.argv.includes('--headed');

function contentType(p) {
  if (p.endsWith('.html')) return 'text/html; charset=utf-8';
  if (p.endsWith('.js')) return 'application/javascript; charset=utf-8';
  if (p.endsWith('.css')) return 'text/css; charset=utf-8';
  return 'application/octet-stream';
}

function startStaticServer(agentsPayload) {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/api/agents') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify(agentsPayload()));
        return;
      }
      if (u.pathname === '/api/migrate/options') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ providers: [
          { provider: 'grok', band: 'ok', eligible: true, reason: 'Grok weekly on pace', models: ['grok-4.5', 'grok-4'] },
          { provider: 'claude', band: 'ok', eligible: true, reason: 'Claude weekly on pace', models: ['claude-fable-5', 'claude-opus-5'] },
        ] }));
        return;
      }
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 'fleet-tree-test', ok: true }));
        return;
      }
      const rel = u.pathname === '/' ? '/index.html' : u.pathname;
      const file = path.normalize(path.join(webRoot, rel));
      if (!file.startsWith(webRoot)) { res.writeHead(403); res.end(); return; }
      fs.readFile(file, (err, data) => {
        if (err) { res.writeHead(404); res.end('not found'); return; }
        res.writeHead(200, { 'Content-Type': contentType(file) });
        res.end(data);
      });
    });
    srv.listen(0, '127.0.0.1', () => resolve({ srv, base: `http://127.0.0.1:${srv.address().port}` }));
    srv.on('error', reject);
  });
}

(async () => {
  const failures = [];
  // Deliberately unsorted feed — UI must still show stable name order under each parent.
  // Fan-out workers share PO workdir (typical); zeta keeps a distinct path.
  const poRepo = '/Users/x/work/github.com/org/po-repo';
  let agents = [
    { name: 'zeta-worker', workdir: '/Users/x/work/github.com/org/other', parent: 'po', status: 'running', progress: 'working · xcodebuild' },
    { name: 'alpha-worker', workdir: poRepo, parent: 'po', status: 'stopped', progress: 'stopped' },
    { name: 'po', workdir: poRepo, parent: 'jevons', status: 'running', provider: 'grok', model: 'grok-4.5' },
    // 🎯T115: root overseer state-dir home must not render as path chrome.
    { name: 'jevons', workdir: '/Users/x/.jevons/jevons', parent: '', status: 'running', purpose: 'overseer', provider: 'grok', model: 'grok-4.5' },
    // 🎯T118: same-workdir leaf under po → progress secondary, not path.
    // 🎯T287: Anthropic worker — company icon + version subscript. 🎯T299 cut
    // the family initial; 🎯T302 restores it, so this row reads O4.8 again.
    { name: 'mid-worker', workdir: poRepo, parent: 'po', status: 'running', phase: 'working', step: 'Bash: go test', progress: 'working · Bash: go test', provider: 'claude', model: 'claude-opus-4-8' },
    // Aside-purpose row: 💡 title, no path element (description must not bleed).
    { name: 'att-billing', description: 'billing nit', purpose: 'aside', workdir: '/Users/x/.jevons/threads/att-billing', parent: 'jevons', status: 'running' },
    // 🎯T365: target-filing aside — same purpose, 🎯 chrome from aside_kind.
    { name: 'att-filing', description: 'safe mode', purpose: 'aside', aside_kind: 'target', workdir: '/Users/x/.jevons/asides/att-filing', parent: 'jevons', status: 'running' },
  ];
  const { srv, base } = await startStaticServer(() => agents);
  const browser = await chromium.launch({ headless: !HEADED });
  const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => typeof window.refreshAgents === 'function' || typeof refreshAgents === 'function', null, { timeout: 10000 });
    await page.evaluate(() => {
      try { refreshAgents(); } catch (_) { window.refreshAgents && window.refreshAgents(); }
    });
    await page.waitForTimeout(200);

    const tree = await page.evaluate(() => {
      const nodes = [...document.querySelectorAll('#agents .agent-node')];
      return nodes.map(n => ({
        name: n.dataset.agent,
        parent: n.dataset.parent || '',
        depth: (() => {
          let d = 0, el = n.parentElement;
          while (el) {
            if (el.classList && el.classList.contains('agent-children')) d++;
            el = el.parentElement;
          }
          return d;
        })(),
        indent: n.closest('.agent-children') ? true : false,
      }));
    });

    if (tree.length !== 7) {
      failures.push(`expected 7 agent nodes (completeness), got ${tree.length}: ${JSON.stringify(tree)}`);
    }
    const names = tree.map(t => t.name);
    if (!names.includes('zeta-worker') || !names.includes('alpha-worker') || !names.includes('jevons') || !names.includes('att-billing')) {
      failures.push(`missing agents in panel: ${names.join(',')}`);
    }
    // Top-level roots first: only jevons has empty parent in this fixture.
    const roots = tree.filter(t => t.parent === '');
    if (roots.length !== 1 || roots[0].name !== 'jevons') {
      failures.push(`roots = ${JSON.stringify(roots)}, want single jevons`);
    }
    // Under po: alpha, mid, zeta in locale name order.
    const underPo = tree.filter(t => t.parent === 'po').map(t => t.name);
    const wantSiblings = ['alpha-worker', 'mid-worker', 'zeta-worker'];
    if (JSON.stringify(underPo) !== JSON.stringify(wantSiblings)) {
      failures.push(`sibling order under po = ${JSON.stringify(underPo)}, want ${JSON.stringify(wantSiblings)}`);
    }
    // po must nest under jevons (parent attr).
    const po = tree.find(t => t.name === 'po');
    if (!po || po.parent !== 'jevons') {
      failures.push(`po parent=${po && po.parent}, want jevons`);
    }

    // 🎯T115: overseer omits ~/.jevons/jevons path; aside has 💡 and no path.
    const chrome = await page.evaluate(() => {
      const nodes = [...document.querySelectorAll('#agents .agent-node')];
      function row(name) {
        const n = nodes.find(el => el.dataset.agent === name);
        if (!n) return null;
        const dir = n.querySelector('.agent-dir');
        return {
          nameText: (n.querySelector('.agent-name') || {}).textContent || '',
          hasDir: !!dir,
          dirText: dir ? (dir.textContent || '') : '',
          isAsideClass: n.classList.contains('agent-aside'),
          asideKind: n.dataset.asideKind || '',
        };
      }
      return {
        jevons: row('jevons'),
        aside: row('att-billing'),
        filing: row('att-filing'),
        po: row('po'),
      };
    });
    if (!chrome.jevons) {
      failures.push('T115: jevons row missing');
    } else {
      if (chrome.jevons.hasDir || /\.jevons|jevons\/jevons/i.test(chrome.jevons.dirText)) {
        failures.push('T115: overseer must omit state-dir path: ' + JSON.stringify(chrome.jevons));
      }
      if (chrome.jevons.nameText !== 'jevons') {
        failures.push('T115: overseer title: ' + JSON.stringify(chrome.jevons.nameText));
      }
    }
    if (!chrome.aside) {
      failures.push('T115: aside row missing');
    } else {
      if (!/^💡\s*billing nit$/.test(chrome.aside.nameText)) {
        failures.push('T115: aside title wants 💡 billing nit: ' + JSON.stringify(chrome.aside.nameText));
      }
      if (chrome.aside.hasDir || chrome.aside.dirText) {
        failures.push('T115: aside must have no path element: ' + JSON.stringify(chrome.aside));
      }
      if (!chrome.aside.isAsideClass) {
        failures.push('T115: aside missing agent-aside class');
      }
    }
    // 🎯T365: same purpose=aside, different kind — filings paint 🎯, ideas 💡.
    if (!chrome.filing) {
      failures.push('T365: target-filing aside row missing');
    } else {
      if (!/^🎯\s*safe mode$/.test(chrome.filing.nameText)) {
        failures.push('T365: filing wants 🎯 safe mode: ' + JSON.stringify(chrome.filing.nameText));
      }
      if (/💡/.test(chrome.filing.nameText)) {
        failures.push('T365: filing must not carry the light bulb: ' + JSON.stringify(chrome.filing.nameText));
      }
      if (chrome.filing.asideKind !== 'target') {
        failures.push('T365: filing data-aside-kind=' + JSON.stringify(chrome.filing.asideKind));
      }
      if (!chrome.filing.isAsideClass) {
        failures.push('T365: filing still needs aside row chrome (class)');
      }
      if (chrome.filing.hasDir || chrome.filing.dirText) {
        failures.push('T365: filing must have no path element: ' + JSON.stringify(chrome.filing));
      }
    }
    if (chrome.aside && /🎯/.test(chrome.aside.nameText)) {
      failures.push('T365: idea aside must keep 💡, not 🎯: ' + JSON.stringify(chrome.aside.nameText));
    }
    if (!chrome.po || !chrome.po.hasDir || !/org\/po-repo/.test(chrome.po.dirText)) {
      failures.push('T115: work agent (PO) must keep path chrome: ' + JSON.stringify(chrome.po));
    }

    // 🎯T118: same-workdir worker → non-path progress secondary; distinct workdir keeps path.
    const t118 = await page.evaluate(() => {
      const nodes = [...document.querySelectorAll('#agents .agent-node')];
      function row(name) {
        const n = nodes.find(el => el.dataset.agent === name);
        if (!n) return null;
        const dir = n.querySelector('.agent-dir');
        return {
          secondary: n.dataset.secondary || '',
          hasDir: !!dir,
          dirText: dir ? (dir.textContent || '') : '',
          dirClass: dir ? dir.className : '',
        };
      }
      return { mid: row('mid-worker'), alpha: row('alpha-worker'), zeta: row('zeta-worker'), po: row('po') };
    });
    if (!t118.mid) {
      failures.push('T118: mid-worker row missing');
    } else {
      if (t118.mid.secondary !== 'progress') {
        failures.push('T118: mid-worker secondaryKind want progress: ' + JSON.stringify(t118.mid));
      }
      if (!/working/.test(t118.mid.dirText) || !/Bash/.test(t118.mid.dirText)) {
        failures.push('T118: mid-worker progress text: ' + JSON.stringify(t118.mid));
      }
      if (/po-repo|github\.com/.test(t118.mid.dirText)) {
        failures.push('T118: mid-worker must not show path as secondary: ' + JSON.stringify(t118.mid));
      }
      if (!/agent-progress/.test(t118.mid.dirClass)) {
        failures.push('T118: mid-worker missing agent-progress class: ' + JSON.stringify(t118.mid));
      }
    }
    if (!t118.alpha || t118.alpha.secondary !== 'progress' && t118.alpha.secondary !== 'status') {
      // stopped + progress field → progress kind
      if (!t118.alpha || !/stopped/i.test(t118.alpha.dirText) || /po-repo/.test(t118.alpha.dirText || '')) {
        failures.push('T118: alpha-worker should show non-path status/progress: ' + JSON.stringify(t118.alpha));
      }
    }
    if (!t118.zeta || t118.zeta.secondary !== 'path' || !/org\/other/.test(t118.zeta.dirText || '')) {
      failures.push('T118: distinct-workdir worker keeps path: ' + JSON.stringify(t118.zeta));
    }
    if (!t118.po || t118.po.secondary !== 'path') {
      failures.push('T118: PO with children keeps path secondary: ' + JSON.stringify(t118.po));
    }

    // 🎯T287: company icon + condensed model subscript before the bare name.
    const badgeOf = (name) => page.evaluate((n) => {
      const node = [...document.querySelectorAll('#agents .agent-node')]
        .find(el => el.dataset.agent === n);
      if (!node) return null;
      const badge = node.querySelector('.model-badge');
      const nameEl = node.querySelector('.agent-name');
      return {
        company: badge ? badge.dataset.company : '',
        sub: badge && badge.querySelector('sub') ? badge.querySelector('sub').textContent : '',
        hasIcon: !!(badge && badge.querySelector('svg.model-icon')),
        title: badge ? badge.getAttribute('title') : '',
        // Prefix must precede the name in document order.
        beforeName: !!(badge && nameEl &&
          (badge.compareDocumentPosition(nameEl) & Node.DOCUMENT_POSITION_FOLLOWING)),
      };
    }, name);

    const anth = await badgeOf('mid-worker');
    if (!anth || anth.company !== 'anthropic' || anth.sub !== 'O4.8' || !anth.hasIcon || !anth.beforeName) {
      failures.push('T302: Anthropic Opus prefix: ' + JSON.stringify(anth));
    }

    // 🎯T302: the restored initial only survives if it does not read as a
    // digit, and the mark only reads as Claude if it carries the brand orange.
    // Both are CSS claims, so both are measured off the rendered row rather
    // than off the rule text: the letter must be heavier than the digits
    // beside it and painted in the accent the frontier table gives target ids,
    // and the splat must be orange over transparent ground — no plate behind
    // it, which is the shape the owner rejected.
    const paint = await page.evaluate(() => {
      const node = [...document.querySelectorAll('.agent-node')]
        .find(n => (n.querySelector('.agent-name') || {}).textContent === 'mid-worker');
      const badge = node && node.querySelector('.model-badge');
      const fam = badge && badge.querySelector('sub .model-family');
      const sub = badge && badge.querySelector('sub');
      const icon = badge && badge.querySelector('.model-icon');
      if (!fam || !sub || !icon) return null;
      const accentSwatch = document.createElement('span');
      accentSwatch.style.color = 'var(--accent)';
      document.body.appendChild(accentSwatch);
      const accent = getComputedStyle(accentSwatch).color;
      accentSwatch.remove();
      return {
        letter: fam.textContent,
        famColor: getComputedStyle(fam).color,
        accent: accent,
        famWeight: Number(getComputedStyle(fam).fontWeight),
        subWeight: Number(getComputedStyle(sub).fontWeight),
        iconColor: getComputedStyle(icon).color,
        iconOpacity: Number(getComputedStyle(icon).opacity),
        // Anything painted behind the mark would show up here.
        iconBg: getComputedStyle(icon).backgroundColor,
        badgeBg: getComputedStyle(badge).backgroundColor,
        svgChildren: [...icon.children].map(c => c.tagName.toLowerCase()),
        fills: [...icon.querySelectorAll('[fill]')].map(p => p.getAttribute('fill')),
      };
    });
    if (!paint) {
      failures.push('T302: no rendered family letter on the Anthropic row');
    } else {
      if (paint.letter !== 'O') {
        failures.push('T302: family letter is not O: ' + JSON.stringify(paint));
      }
      // Same colour the frontier table paints target ids — resolved, so a
      // hardcoded hex that happens to match one palette still fails the other.
      if (paint.famColor !== paint.accent) {
        failures.push('T302: family letter is not the accent token: ' + JSON.stringify(paint));
      }
      if (!(paint.famWeight >= 700) || !(paint.famWeight > paint.subWeight)) {
        failures.push('T302: family letter is not bolder than the digits: ' + JSON.stringify(paint));
      }
      // Faded orange on the strokes: the glyph colour is the brand orange and
      // the mark is drawn under 1 opacity, exactly as Grok's neutral mark is.
      if (paint.iconColor !== 'rgb(217, 119, 87)') {
        failures.push('T302: Claude mark is not brand orange D97757: ' + JSON.stringify(paint));
      }
      if (!(paint.iconOpacity < 1)) {
        failures.push('T302: Claude mark is not faded: ' + JSON.stringify(paint));
      }
      // No plate: nothing but the glyph path inside the svg, no painted ground
      // on the svg or the badge, and no literal fill knocked out of a tile.
      const transparent = c => c === 'rgba(0, 0, 0, 0)' || c === 'transparent';
      if (!paint.svgChildren.every(t => t === 'path')
          || !paint.fills.every(f => f === 'currentColor')
          || !transparent(paint.iconBg) || !transparent(paint.badgeBg)) {
        failures.push('T302: Claude mark sits on a plate: ' + JSON.stringify(paint));
      }
    }
    // 🎯T299: the version is a *subscript* — its foot sits below the mark's,
    // not level with it and not beside its middle. Measured, because the CSS
    // that carries it (cross-axis alignment plus a relative offset) only shows
    // its effect once both boxes are laid out.
    const drop = await page.evaluate(() => {
      const node = [...document.querySelectorAll('#agents .agent-node')]
        .find(el => el.dataset.agent === 'mid-worker');
      const icon = node && node.querySelector('.model-badge svg.model-icon');
      const sub = node && node.querySelector('.model-badge sub');
      if (!icon || !sub) return null;
      const i = icon.getBoundingClientRect();
      const s = sub.getBoundingClientRect();
      return { below: s.bottom - i.bottom, iconH: i.height, subH: s.height };
    });
    if (!drop || !(drop.below > 1) || !(drop.below < drop.iconH / 3)) {
      failures.push('T299: subscript must hang just below the mark: ' + JSON.stringify(drop));
    }
    const xai = await badgeOf('po');
    if (!xai || xai.company !== 'xai' || xai.sub !== '4.5' || !xai.hasIcon || !xai.beforeName) {
      failures.push('T287: Grok prefix (no leading G): ' + JSON.stringify(xai));
    }
    // 🎯T506: the owner-reported root badge is a visible native control with
    // readable model text and an effective pointer target. Measure the real
    // rendered boxes and centre hit-test; CSS source assertions alone do not
    // establish any of these claims.
    await page.locator('#agents .agent-node[data-agent="po"] .agent-name').click();
    const rootBadge = page.locator('#agents .agent-node[data-agent="jevons"] .model-badge');
    const rootGeometry = await rootBadge.evaluate((badge) => {
      const rect = badge.getBoundingClientRect();
      const sub = badge.querySelector('sub');
      const centre = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2);
      const rgb = (value) => {
        const m = String(value).match(/[\d.]+/g);
        return m && m.length >= 3 ? m.slice(0, 3).map(Number) : [0, 0, 0];
      };
      const luminance = (colour) => {
        const channels = rgb(colour).map((v) => {
          const s = v / 255;
          return s <= 0.04045 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
        });
        return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
      };
      let ground = badge;
      let background = 'rgb(255, 255, 255)';
      while (ground) {
        const candidate = getComputedStyle(ground).backgroundColor;
        if (!/^rgba\([^)]*,\s*0(?:\.0+)?\)$/.test(candidate) && candidate !== 'transparent') {
          background = candidate;
          break;
        }
        ground = ground.parentElement;
      }
      const foreground = sub ? getComputedStyle(sub).color : 'rgb(0, 0, 0)';
      const high = Math.max(luminance(foreground), luminance(background));
      const low = Math.min(luminance(foreground), luminance(background));
      return {
        tag: badge.tagName,
        aria: badge.getAttribute('aria-label') || '',
        width: rect.width,
        height: rect.height,
        fontSize: sub ? parseFloat(getComputedStyle(sub).fontSize) : 0,
        contrast: (high + 0.05) / (low + 0.05),
        visible: typeof badge.checkVisibility === 'function'
          ? badge.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })
          : rect.width > 0 && rect.height > 0,
        centreHitsBadge: centre === badge || badge.contains(centre),
      };
    });
    if (rootGeometry.tag !== 'BUTTON' || !/Select provider and model for jevons/.test(rootGeometry.aria)
        || rootGeometry.width < 32 || rootGeometry.height < 32 || rootGeometry.fontSize < 12
        || rootGeometry.contrast < 4.5
        || !rootGeometry.visible || !rootGeometry.centreHitsBadge) {
      failures.push('T506: root model selector geometry/semantics: ' + JSON.stringify(rootGeometry));
    }

    // Pointer and keyboard activation both open the actual provider/model
    // table, and neither route may steal selection from the agent row.
    await rootBadge.click();
    await page.waitForSelector('#prov-menu', { state: 'visible' });
    let selected = await page.locator('#agents .agent-node.selected').getAttribute('data-agent');
    if (selected !== 'po') failures.push('T506: pointer activation selected ' + JSON.stringify(selected));
    await page.keyboard.press('Escape');
    await rootBadge.focus();
    await rootBadge.press('Enter');
    await page.waitForSelector('#prov-menu', { state: 'visible' });
    selected = await page.locator('#agents .agent-node.selected').getAttribute('data-agent');
    if (selected !== 'po') failures.push('T506: keyboard activation selected ' + JSON.stringify(selected));
    const menuRows = await page.locator('#prov-menu .prov-menu-row').count();
    if (menuRows < 2) failures.push('T506: provider/model menu has ' + menuRows + ' rows');
    const artifactDir = path.join(__dirname, 'artifacts');
    fs.mkdirSync(artifactDir, { recursive: true });
    await page.screenshot({ path: path.join(artifactDir, 't506-model-selector.png') });
    await page.keyboard.press('Escape');
    // No provider at all → no prefix chrome (row unchanged).
    const none = await badgeOf('alpha-worker');
    if (!none || none.company !== '' || none.hasIcon) {
      failures.push('T287: unknown company must paint nothing: ' + JSON.stringify(none));
    }

    // Prefix follows a migrate: same agent, new provider/model → repaint.
    agents = agents.map(a => a.name === 'po'
      ? { ...a, provider: 'claude', model: 'claude-sonnet-4-5-20250929' }
      : a);
    await page.evaluate(() => {
      try { refreshAgents(); } catch (_) { window.refreshAgents && window.refreshAgents(); }
    });
    await page.waitForTimeout(400);
    // 🎯T302: the migrate is legible in the subscript again — Grok 4.5 and
    // Sonnet 4.5 read the same under 🎯T299, but the restored initial tells
    // them apart, so the letter is part of what proves the repaint.
    const migrated = await badgeOf('po');
    if (!migrated || migrated.company !== 'anthropic' || migrated.sub !== 'S4.5'
        || migrated.title !== 'Anthropic · claude-sonnet-4-5-20250929') {
      failures.push('T302: prefix did not follow the migrate: ' + JSON.stringify(migrated));
    }

    // 🎯T71 thin: working indicator picks up tool progress.
    await page.evaluate(() => {
      setWorking(true);
      addTurnItem('tool-use', 'jevons_agent_start: name=worker');
    });
    await page.waitForTimeout(50);
    const work = await page.evaluate(() => {
      const el = document.querySelector('.working-indicator');
      return el ? el.textContent : '';
    });
    if (!/step/i.test(work) || !/jevons_agent_start/i.test(work)) {
      failures.push(`T71 working progress not shown: ${JSON.stringify(work)}`);
    }

    // 🎯T94: idle stuck-watchdog — progress resets idle clock; pure idle fires recover.
    // Stub transport open/close: static hermetic server has no /ws/chat, and
    // onClose would call setWorking(false) and onOpen wipes #messages.
    await page.evaluate(() => {
      if (typeof transport !== 'undefined' && transport) {
        transport.onClose = function () {};
        transport.onOpen = function () {};
      }
      WORKING_STUCK_MS = 150;
      setWorking(true);
    });
    // Mid-turn progress must keep working alive past the bound.
    for (let i = 0; i < 5; i++) {
      await page.waitForTimeout(60);
      await page.evaluate((n) => { updateWorkingProgress('step ' + n + ' · tool_x'); }, i);
    }
    await page.waitForTimeout(50);
    const midHealthy = await page.evaluate(() => !!document.querySelector('.working-indicator'));
    if (!midHealthy) {
      failures.push('T94 false-recovered while progress was still arriving');
    }
    // Stop progress; idle bound should clear working + status note.
    await page.waitForTimeout(250);
    const afterStuck = await page.evaluate(() => ({
      working: !!document.querySelector('.working-indicator'),
      status: [...document.querySelectorAll('.msg.status')].map(e => e.textContent).join(' | '),
    }));
    if (afterStuck.working) {
      failures.push('T94 stuck-watchdog left working indicator on after idle bound');
    }
    if (!/stuck|Recovered|idle/i.test(afterStuck.status)) {
      failures.push('T94 stuck-watchdog status note missing: ' + JSON.stringify(afterStuck.status));
    }
  } catch (e) {
    failures.push(String(e && e.stack || e));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL fleet-tree-test:');
    for (const f of failures) console.error('  -', f);
    process.exit(1);
  }
  console.log('ok - fleet tree hierarchy; completeness; model prefix; T506 model-selector geometry + pointer/keyboard menu activation');
})();
