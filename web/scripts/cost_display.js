// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T137: honest cost-ticker formatting. DOM-free for Node tests.
// list_price → billable $; subscription → labeled estimate; disabled → hide.

(function (root, factory) {
  if (typeof module === 'object' && module.exports) {
    module.exports = factory();
  } else {
    root.CostDisplay = factory();
  }
}(typeof self !== 'undefined' ? self : this, function () {
  'use strict';

  /**
   * @param {object|null|undefined} c - GET /api/cost JSON body
   * @returns {{visible:boolean, text?:string, className?:string, title?:string}}
   */
  function formatCostTicker(c) {
    if (!c || c.error || c.disabled || c.accounting === 'disabled') {
      return { visible: false };
    }

    const g = Number(c.global_usd_per_hour || 0).toFixed(2);
    const f = Number(c.fleet_usd_per_hour || 0).toFixed(2);
    const alerts = Array.isArray(c.alerts) ? c.alerts : [];
    const billable = c.billable === true ||
      (c.billable !== false && c.accounting !== 'subscription');
    const kill = billable && alerts.some(function (a) { return a.level === 'kill'; });
    const warn = alerts.length > 0;

    var text;
    if (!billable || c.accounting === 'subscription') {
      text = 'est $' + g + '/hr global · $' + f + '/hr fleet (not billed)';
    } else {
      text = '🔥 $' + g + '/hr global · $' + f + '/hr fleet';
    }
    if (alerts.length) {
      text += ' · ⚠ ' + alerts.length;
    }

    var title;
    if (c.currency_note) {
      title = c.currency_note;
    } else if (!billable) {
      title = 'API-equivalent estimate — not billed under SuperGrok / flat subscription';
    } else {
      title = 'No runaway signals';
    }
    if (alerts.length) {
      title = alerts.map(function (a) {
        return '[' + a.level + '] ' + a.kind + ': ' + a.detail;
      }).join('\n');
      if (c.currency_note) {
        title = c.currency_note + '\n' + title;
      }
    }

    return {
      visible: true,
      text: text,
      className: kill ? 'cost-kill' : (warn ? 'cost-warn' : ''),
      title: title
    };
  }

  return {
    formatCostTicker: formatCostTicker
  };
}));
