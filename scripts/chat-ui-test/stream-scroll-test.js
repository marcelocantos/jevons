// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Hermetic oracle for follow-scroll during long in-flight assistant streams
// (🎯T30.2). Serves the static web/ UI, grows a streaming bubble past the
// viewport, and asserts:
//   * auto-follow keeps the viewport within FOLLOW slack of true bottom
//     after each growth step (no freeze at a mid-bubble offset)
//   * intentional scroll-up disarms follow (autoScroll false) and is not
//     bounced back by the next stream chunk
//   * after seal, free scroll reaches the true bottom
//
//   node scripts/chat-ui-test/stream-scroll-test.js [--headed]

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

function startStaticServer() {
  const webRoot = path.join(__dirname, '..', '..', 'web');
  return new Promise((resolve, reject) => {
    const srv = http.createServer((req, res) => {
      const u = new URL(req.url, 'http://127.0.0.1');
      if (u.pathname === '/health') {
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ version: 'stream-scroll-test', ok: true }));
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

function slackOf(page) {
  return page.evaluate(() => {
    const m = document.getElementById('messages');
    const dist = m.scrollHeight - m.clientHeight - m.scrollTop;
    return {
      dist,
      autoScroll: window.autoScroll === true || (typeof autoScroll !== 'undefined' && autoScroll === true),
      scrollTop: m.scrollTop,
      scrollHeight: m.scrollHeight,
      clientHeight: m.clientHeight,
      streamItems: document.querySelectorAll('#messages .msg.jevons li').length,
      hasEnd: !!document.getElementById('messages-end'),
    };
  });
}

(async () => {
  const failures = [];
  const { srv, base } = await startStaticServer();
  const browser = await chromium.launch({ headless: !HEADED });
  // Short viewport so a long stream exceeds one screen.
  const page = await browser.newPage({ viewport: { width: 900, height: 500 } });

  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(
      () => typeof window.appendOrAddJevons === 'function' && typeof window.sealAssistantStream === 'function',
      null,
      { timeout: 10000 },
    );

    // Pad history so the stream starts near the bottom of a long transcript.
    // Include the working indicator (live path always has it mid-turn).
    await page.evaluate(() => {
      document.getElementById('messages').innerHTML = '';
      for (let i = 0; i < 12; i++) {
        window.addMsg('user', 'prior user turn ' + i);
        window.addMsg('jevons', 'prior assistant turn ' + i + ' — short.');
      }
      if (typeof setWorking === 'function') setWorking(true, 'streaming');
      // Force follow on and pin (same as send()).
      autoScroll = true;
      const m = document.getElementById('messages');
      m.scrollTop = m.scrollHeight;
    });
    await page.waitForTimeout(80);

    // Grow a streaming reply in steps that each add many lines.
    // Assert scrollHeight is monotonic and follow lag stays within slack —
    // regression for "pins to mid-bubble while tokens still arrive".
    const steps = 10;
    let prevHeight = 0;
    for (let s = 0; s < steps; s++) {
      await page.evaluate((step) => {
        // Mix markdown so marked re-layout is non-trivial (lists + headings +
        // a short table), matching real incremental replies.
        const block = [
          `### stream_step_${step}`,
          ...Array.from({ length: 20 }, (_, i) => `- stream_step_${step}_line_${i}`),
          '',
          '| a | b |',
          '| --- | --- |',
          `| ${step} | ${step * 2} |`,
          '',
        ].join('\n');
        window.appendOrAddJevons((step === 0 ? '### long stream\n' : '\n') + block);
      }, s);
      // rAF render + double-rAF pin + ResizeObserver / IO settle.
      await page.waitForTimeout(150);
      const snap = await slackOf(page);
      // FOLLOW_SLACK_PX is 120; allow a little layout jitter.
      if (snap.dist > 140) {
        failures.push(
          `step ${s}: follow lag ${snap.dist}px (scrollTop=${snap.scrollTop}, ` +
          `height=${snap.scrollHeight}, client=${snap.clientHeight}, items=${snap.streamItems})`,
        );
      }
      if (snap.scrollHeight + 1 < prevHeight) {
        failures.push(
          `step ${s}: scrollHeight shrank mid-stream (${prevHeight} → ${snap.scrollHeight})`,
        );
      }
      prevHeight = Math.max(prevHeight, snap.scrollHeight);
      if (snap.streamItems < (s + 1) * 15) {
        failures.push(`step ${s}: only ${snap.streamItems} list items rendered mid-stream (want ~full growth)`);
      }
    }

    if (!failures.length) {
      // Intentional scroll-up must disarm follow and survive the next chunk.
      await page.evaluate(() => {
        const m = document.getElementById('messages');
        m.scrollTop = Math.max(0, m.scrollTop - 400);
      });
      await page.waitForTimeout(50);
      const afterUp = await slackOf(page);
      // autoScroll is a let in page scope — classic script binds it on window in browsers.
      const armed = await page.evaluate(() => {
        try { return autoScroll; } catch (_) { return null; }
      });
      if (armed !== false) {
        failures.push(`scroll-up did not disarm follow (autoScroll=${JSON.stringify(armed)}, dist=${afterUp.dist})`);
      }
      const topBefore = afterUp.scrollTop;
      await page.evaluate(() => {
        window.appendOrAddJevons('\n- stream_after_user_scroll_up');
      });
      await page.waitForTimeout(120);
      const afterChunk = await slackOf(page);
      // Must not bounce back to bottom (would jump scrollTop up by hundreds).
      if (afterChunk.scrollTop > topBefore + 80) {
        failures.push(
          `next stream chunk bounced scroll after user scroll-up ` +
          `(was ${topBefore}, now ${afterChunk.scrollTop})`,
        );
      }
    }

    // Seal, re-enable follow, pin, then free-scroll must reach true bottom.
    await page.evaluate(() => {
      window.sealAssistantStream();
      autoScroll = true;
      const m = document.getElementById('messages');
      m.scrollTop = m.scrollHeight;
    });
    await page.waitForTimeout(150);
    const sealed = await slackOf(page);
    if (sealed.dist > 140) {
      failures.push(`after seal+pin, still ${sealed.dist}px from bottom`);
    }

    // History browse: small wheel-up must NOT snap back to bottom (regression
    // from end-sentinel IntersectionObserver re-pin while autoScroll true).
    await page.evaluate(() => {
      autoScroll = true;
      const m = document.getElementById('messages');
      m.scrollTop = m.scrollHeight;
    });
    await page.waitForTimeout(40);
    await page.locator('#messages').hover({ position: { x: 40, y: 40 } });
    await page.mouse.wheel(0, -300);
    await page.waitForTimeout(120);
    const afterWheel = await slackOf(page);
    if (afterWheel.dist < 80) {
      failures.push(
        `history wheel-up snapped back (dist=${afterWheel.dist}, want well above bottom)`,
      );
    }
    const wheelArmed = await page.evaluate(() => {
      try { return autoScroll; } catch (_) { return null; }
    });
    if (wheelArmed !== false) {
      failures.push(`history wheel-up did not disarm follow (autoScroll=${JSON.stringify(wheelArmed)})`);
    }
    // Stay put — no delayed snap after wheel (wait a beat).
    const topAfterWheel = afterWheel.scrollTop;
    await page.waitForTimeout(200);
    const afterWait = await slackOf(page);
    if (Math.abs(afterWait.scrollTop - topAfterWheel) > 40) {
      failures.push(
        `scroll drifted after wheel-up without user input ` +
        `(was ${topAfterWheel}, now ${afterWait.scrollTop})`,
      );
    }

    // Scroll up then down manually — should reach bottom without bounce fight.
    await page.evaluate(() => {
      autoScroll = false;
      const m = document.getElementById('messages');
      m.scrollTop = 0;
    });
    await page.waitForTimeout(40);
    await page.evaluate(() => {
      const m = document.getElementById('messages');
      m.scrollTop = m.scrollHeight;
    });
    await page.waitForTimeout(40);
    const free = await slackOf(page);
    if (free.dist > 5) {
      failures.push(`after seal, free scroll cannot reach bottom (dist=${free.dist})`);
    }
    if (!free.hasEnd) {
      failures.push('messages-end sentinel missing after stream');
    }
  } catch (e) {
    failures.push(String(e && e.stack || e));
  } finally {
    await browser.close();
    srv.close();
  }

  if (failures.length) {
    console.error('FAIL stream-scroll-test:');
    for (const f of failures) console.error('  -', f);
    process.exit(1);
  }
  console.log('ok - stream follow-scroll stays at true bottom; user scroll-up disarms; seal free-scroll works (🎯T30.2)');
})();
