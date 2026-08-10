// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T374 — one missing cockpit module degrades one feature, not the page.
//
// THE FAULT. web/index.html is one inline <script> spanning ~9000 lines: 170
// top-level `let`/`const`, 265 top-level function declarations, 78 top-level
// imperative statements. A single throw at its top level aborts everything
// below it. The bindings below stay permanently in the temporal dead zone
// while the listeners and intervals registered above keep firing against
// them, so one transient fault becomes a recurring window.onerror storm —
// once per fleet poll, forever. That is exactly what a 404 on
// scripts/pending_turns.js did: composer_persist.js captures root.PendingTurns
// at its own load time (line 28), so `ComposerPersist.loadPending()` at
// index.html:7209 threw, and workingEl (8146), agentInspectInput (8783) and
// rhsBottomTab (8879) never initialised.
//
// WHY THE EXISTING GUARDS DO NOT COVER IT. index.html already carries 267
// `typeof X !== 'undefined'` guards, and one of them wraps that very call.
// They are all DIRECT-dependency guards and none survives a TRANSITIVE
// absence: ComposerPersist IS defined; its dependency is not. 🎯T292's embed
// ratchet and 🎯T374's serve-time banner both decide whether the FILE is on
// the serving path — necessary, and they now detect this class before the
// page loads, but detection is not containment. Whatever the cause, once the
// browser has a document with a module missing from it, something has to keep
// the rest of the page alive.
//
// WHAT WAS REJECTED, AND WHY.
//   - A blanket try/catch around the inline script. It buys nothing: the
//     throw still ends the try block, so every declaration below it is still
//     dead. It converts loud total failure into SILENT total failure, and it
//     swallows genuine errors from healthy modules on the way. The browser
//     oracle's M2 mutant is exactly this shape, and it must fail.
//   - Splitting the inline script into N <script> blocks. Top-level let/const
//     share one global lexical environment, so a throw would abort only its
//     own segment — attractive. But FUNCTION declarations hoist per script,
//     not across scripts. A top-level call in an early segment to a function
//     declared in a later one works today by hoisting and would silently stop
//     working; with 267 `typeof`-shaped guards those become silently skipped
//     features. That trades a loud abort for silent partial death.
//
// THE MECHANISM: contain at the MODULE boundary, not the statement boundary.
// Two independent detection signals, one containment.
//   (a) A capture-phase 'error' listener on window sees resource-load
//       failures for <script src="scripts/…"> elements. Those events do not
//       bubble and never reach window.onerror, so nothing else in the page
//       observes them. Crucially the browser fires them WHILE it is still
//       ordering blocking classic scripts — the error for script N is
//       dispatched before script N+1 executes — so the stand-in installed
//       here is already in place when the next module's factory runs. That
//       is why this contains rather than merely announces: composer_persist
//       closes over a live stand-in instead of undefined.
//   (b) seal(), called once after the last module tag and before the inline
//       script, walks the document's own script tags and stands in for any
//       expected global that was never assigned. This catches "loaded but its
//       factory threw", which (a) cannot see, and it runs early enough to
//       contain that case for the inline script too.
//
// NOT A SWALLOW. Only names on the gate's table, and only ones proven absent,
// get a stand-in. Errors raised by modules that ARE present propagate
// untouched to window.onerror and on to jlog.js and the daemon eventlog. Each
// absence produces a banner naming the module and a jLog line: degraded and
// loud, never quiet.
//
// ANTI-DRIFT. The gated SET is read from the document at runtime, so a module
// added tomorrow is gated with no edit here. Only the NAME rule needs
// maintenance (snake_case → PascalCase plus a measured exception set), and
// module_gate_test.js recomputes it against every file in web/scripts/ and
// fails on disagreement — a mismatched name is a red test, not a silent hole.
//
// DECLARED RESIDUAL, not fudged:
//   - A module that loads but whose factory throws is only detected at seal
//     time, so a PEER that captured it at its own load time (the two
//     root.PendingTurns adopters) still holds undefined. Contained for the
//     inline script, not for that peer. Strictly narrower than today.
//   - A module publishing a SECOND global (layout_probe.js also sets
//     JevonsProbe) gets a stand-in only for its primary name.
//   - A stand-in handed to a real DOM API (appendChild) still throws; the
//     browser type-checks the object, and nothing user-space can prevent it.
//
// DOM-free decision logic (globalNameFor / makeStandIn / plan) so Node
// hermetic tests require() the real thing rather than a test-only twin.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.ModuleGate = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  // Marks the injected banner. Tests assert on the literal, so it is part of
  // the contract. Deliberately distinct from 🎯T375's boot-sentinel banner
  // (data-jevons-boot-error) and the serve-time one
  // (data-jevons-asset-error): three different questions, three answers the
  // owner can tell apart — "a module is missing", "the boot never finished",
  // "the tree you were served was incomplete".
  var BANNER_ATTR = 'data-jevons-module-error';

  // Only scripts under this prefix are gated. CDN tags are somebody else's
  // problem and must not receive stand-ins.
  var LOCAL_PREFIX = 'scripts/';

  // Modules whose published global does not follow snake_case → PascalCase.
  // Measured from web/scripts/, not guessed; module_gate_test.js re-derives
  // this against the sources and fails if a module drifts out of the rule
  // without an entry here.
  var NAME_EXCEPTIONS = {
    jlog: 'jLog',
    owner_ux: 'OwnerUX',
    rsi_dispositions: 'RSIDispositions',
    smd: 'smd',
    transport: 'transport',
  };

  // A valid JS identifier. Names are derived from filenames in the served
  // document, so validate before they reach an indirect eval below.
  var IDENT_RE = /^[A-Za-z_$][A-Za-z0-9_$]*$/;

  // baseName('scripts/pending_turns.js') === 'pending_turns'
  function baseName(src) {
    var s = String(src == null ? '' : src);
    var q = s.indexOf('?');
    if (q >= 0) s = s.slice(0, q);
    var cut = s.lastIndexOf('/');
    var file = cut < 0 ? s : s.slice(cut + 1);
    return file.replace(/\.js$/i, '');
  }

  // globalNameFor maps a script src to the global its module publishes.
  function globalNameFor(src) {
    var base = baseName(src);
    if (!base) return '';
    if (Object.prototype.hasOwnProperty.call(NAME_EXCEPTIONS, base)) {
      return NAME_EXCEPTIONS[base];
    }
    var parts = base.split('_');
    var out = '';
    for (var i = 0; i < parts.length; i++) {
      var p = parts[i];
      if (!p) continue;
      out += p.charAt(0).toUpperCase() + p.slice(1);
    }
    return out;
  }

  // makeStandIn builds the inert value published under a missing module's
  // name. It has to answer every shape a caller might reach for — property
  // access, invocation, construction, iteration, string and number coercion,
  // key enumeration — without throwing, because the entire point is that the
  // statement which touches it continues instead of aborting the page.
  //
  // Nothing here pretends the module works. Coercing a stand-in to a string
  // yields a marker naming the module, so if one does reach the DOM the owner
  // reads the cause off the screen rather than filing a mystery.
  function makeStandIn(name, src) {
    var marker = '[jevons: ' + (src || name) + ' failed to load]';
    if (typeof Proxy !== 'function') {
      // Pre-Proxy engines get a plain callable. Property reads yield
      // undefined rather than a stand-in, so containment is partial — but a
      // partial floor beats none, and every browser the cockpit supports has
      // had Proxy for a decade.
      var flat = function () { return undefined; };
      flat.toString = function () { return marker; };
      return flat;
    }

    var target = function () {};
    var proxy = new Proxy(target, {
      get: function (t, prop) {
        // Proxy invariant: a non-configurable, non-writable own property
        // must be reported as itself. Function targets have none by default,
        // but honour it rather than rely on that.
        var d = Object.getOwnPropertyDescriptor(t, prop);
        if (d && d.configurable === false && d.writable === false) {
          return t[prop];
        }
        // NOT thenable. If `then` returned a stand-in, `await X` would call
        // it, get another stand-in back, and hang forever — swapping a throw
        // for a deadlock, which is worse.
        if (prop === 'then') return undefined;
        if (prop === 'toJSON') return undefined;
        if (prop === 'length') return 0;
        if (prop === 'toString' || prop === 'valueOf') {
          return function () { return marker; };
        }
        if (typeof Symbol !== 'undefined') {
          if (prop === Symbol.toPrimitive) return function () { return marker; };
          if (prop === Symbol.toStringTag) return 'JevonsMissingModule';
          // Empty iteration: `[...X]` is [], `for (const a of X)` runs zero
          // times. A caller iterating a missing module's output should see
          // nothing, not an exception.
          if (prop === Symbol.iterator) {
            return function () {
              return { next: function () { return { value: undefined, done: true }; } };
            };
          }
        }
        if (prop === '__jevonsMissingModule') return src || name;
        return makeStandIn(name, src);
      },
      // Accept writes and deletes silently. Product code that stashes state
      // on a module object must not throw on the way past.
      set: function () { return true; },
      deleteProperty: function () { return true; },
      has: function () { return true; },
      // Reflect, not []: ownKeys must list the target's non-configurable own
      // keys (a function's `prototype`) or the Proxy throws on enumeration.
      // Those keys are non-enumerable, so Object.keys() and for..in still
      // see nothing.
      ownKeys: function (t) { return Reflect.ownKeys(t); },
      apply: function () { return makeStandIn(name, src); },
      construct: function () { return makeStandIn(name, src); },
    });
    return proxy;
  }

  // isStandIn answers whether a value came out of this gate. Used by the
  // browser oracle, and by product code that wants to decline work rather
  // than push markers into the DOM.
  function isStandIn(v) {
    if (v == null) return false;
    try {
      return typeof v.__jevonsMissingModule === 'string';
    } catch (_) {
      return false;
    }
  }

  // globalBindingExists decides presence for one name.
  //
  // `typeof win[name]` is not sufficient. transport.js ends with a top-level
  // `const transport = …`, which lands in the GLOBAL LEXICAL environment and
  // is invisible on the window object — so a window-only check would call a
  // present module absent and stand in over it, which is the one direction
  // that actively breaks a working page (the oracle's M3 mutant).
  //
  // An indirect eval runs in global scope and can see that binding. If eval
  // is unavailable — a future CSP without 'unsafe-eval' — the answer is
  // UNKNOWN, and unknown resolves to PRESENT. Under-reach costs signal (b)
  // for that module while (a) still covers the 404 case; over-reach would
  // break a healthy cockpit.
  function globalBindingExists(win, name) {
    if (!name || !IDENT_RE.test(name)) return true;
    if (win && typeof win[name] !== 'undefined') return true;
    try {
      return (0, eval)('typeof ' + name) !== 'undefined';
    } catch (_) {
      return true; // unknown ⇒ leave it alone
    }
  }

  // plan is the seal-time decision, split out DOM-free so it can be tested
  // without a document: given the script srcs the page loads and a presence
  // oracle, which modules need a stand-in.
  //
  // `already` names modules signal (a) has handled, so seal does not report
  // one absence twice.
  function plan(srcs, present, already) {
    var seen = already || {};
    var out = [];
    for (var i = 0; i < (srcs || []).length; i++) {
      var src = String(srcs[i] || '');
      if (src.indexOf(LOCAL_PREFIX) !== 0) continue;
      var name = globalNameFor(src);
      if (!name || seen[name]) continue;
      if (present(name)) continue;
      seen[name] = true;
      out.push({ src: src, global: name });
    }
    return out;
  }

  // The handle from the most recent install(). index.html seals through the
  // module namespace (`ModuleGate.seal()`) rather than holding the handle,
  // because the two calls are 45 script tags apart and a variable carried
  // across them would have to live in the very inline script this protects.
  var installed = null;

  function install(win, doc, opts) {
    var o = opts || {};
    var failures = [];
    var handled = Object.create(null);
    var sealed = false;

    function log(level, msg, fields) {
      // jlog.js is itself gated, so it may be a stand-in or absent. Either
      // way this must not throw while reporting that something failed.
      try {
        if (typeof win.jLog === 'function') win.jLog(level, msg, fields);
      } catch (_) { /* reporting is best-effort */ }
      try {
        if (win.console && typeof win.console.error === 'function') {
          win.console.error('[module-gate] ' + msg, fields);
        }
      } catch (_) { /* ditto */ }
    }

    // record installs the stand-in and remembers the absence. Returns false
    // if this module was already handled.
    function record(src, reason) {
      var name = globalNameFor(src);
      if (!name || handled[name]) return false;
      handled[name] = true;
      failures.push({ src: src, global: name, reason: reason });
      try {
        win[name] = makeStandIn(name, src);
      } catch (_) { /* frozen window: the report below still lands */ }
      log('error', 'module gate: cockpit module unavailable', {
        script: src, global: name, reason: reason,
      });
      return true;
    }

    // Signal (a). Resource-load errors do not bubble; a capture-phase
    // listener on window is the only place to see them. Registered before
    // any other module loads, and it neither preventDefault()s nor stops
    // propagation — swallowing is the failure mode this exists to avoid.
    function onResourceError(e) {
      var t = e && e.target;
      if (!t || t === win || !t.tagName) return;
      if (String(t.tagName).toLowerCase() !== 'script') return;
      var src = (t.getAttribute && t.getAttribute('src')) || '';
      if (src.indexOf(LOCAL_PREFIX) !== 0) return;
      if (record(src, 'script did not load (404 or network error)')) {
        renderBanner();
      }
    }
    win.addEventListener('error', onResourceError, true);

    function bannerEl() {
      return doc.querySelector ? doc.querySelector('[' + BANNER_ATTR + ']') : null;
    }

    // One banner listing every failed module, rewritten as more arrive.
    function renderBanner() {
      if (!failures.length) return null;
      var host = doc.body || doc.documentElement;
      if (!host) return null; // too early; seal() renders once body exists
      var el = bannerEl();
      if (!el) {
        el = doc.createElement('div');
        el.setAttribute(BANNER_ATTR, '1');
        el.setAttribute('role', 'alert');
        el.style.cssText = 'position:fixed;top:0;left:0;right:0;z-index:2147483646;'
          + 'background:#78350f;color:#fff;'
          + 'font:13px/1.45 -apple-system,system-ui,sans-serif;'
          + 'padding:10px 14px;border-bottom:2px solid #f59e0b';
        host.appendChild(el);
      }
      var names = [];
      for (var i = 0; i < failures.length; i++) names.push(failures[i].src);
      // textContent throughout: a degraded cockpit announcing its own damage
      // must not become an injection surface while doing it.
      el.textContent = '';
      var strong = doc.createElement('strong');
      strong.textContent = failures.length === 1
        ? 'Cockpit module unavailable — the features that use it are disabled.'
        : 'Cockpit modules unavailable — the features that use them are disabled.';
      var detail = doc.createElement('div');
      detail.textContent = names.join(', ') + ' — the rest of the cockpit is live.';
      var btn = doc.createElement('button');
      btn.type = 'button';
      btn.textContent = 'Reload';
      btn.style.cssText = 'margin-top:6px;padding:3px 12px;border-radius:4px;'
        + 'border:1px solid #fcd34d;background:#fff;color:#78350f;cursor:pointer';
      btn.addEventListener('click', function () { win.location.reload(); });
      el.appendChild(strong);
      el.appendChild(detail);
      el.appendChild(btn);
      return el;
    }

    // Signal (b). Called once from index.html after the last module tag and
    // before the inline script, so a factory that threw is contained for the
    // inline script as well as for everything async that follows.
    function seal() {
      if (sealed) return failures.slice();
      sealed = true;
      var srcs = [];
      var tags = doc.querySelectorAll ? doc.querySelectorAll('script[src]') : [];
      for (var i = 0; i < tags.length; i++) {
        var s = tags[i].getAttribute('src') || '';
        if (s.indexOf(LOCAL_PREFIX) === 0) srcs.push(s);
      }
      var needed = plan(srcs, function (name) {
        return globalBindingExists(win, name);
      }, {});
      for (var j = 0; j < needed.length; j++) {
        record(needed[j].src, 'script loaded but published no global (its factory threw)');
      }
      renderBanner();
      return failures.slice();
    }

    var handle = {
      seal: seal,
      failures: function () { return failures.slice(); },
      sealed: function () { return sealed; },
    };
    win.__jevonsModuleGate = handle;
    installed = handle;
    if (o.autoSeal) seal();
    return handle;
  }

  return {
    BANNER_ATTR: BANNER_ATTR,
    LOCAL_PREFIX: LOCAL_PREFIX,
    NAME_EXCEPTIONS: NAME_EXCEPTIONS,
    baseName: baseName,
    globalNameFor: globalNameFor,
    makeStandIn: makeStandIn,
    isStandIn: isStandIn,
    plan: plan,
    install: install,
    // Seals the installed gate. A no-op when install() never ran, so a page
    // that loaded this module but not its installer cannot be broken by the
    // seal call itself — the one thing a containment module must never do is
    // become the fault.
    seal: function () { return installed ? installed.seal() : []; },
  };
}));
