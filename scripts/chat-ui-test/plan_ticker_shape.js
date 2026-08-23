// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Plan-ticker oracle (owner caveat 3): required matching on usage bars.
// Old paintPlanUsage is HTML spans (not <svg> rects) plus a company-mark
// SVG. Allowed to differ: fill width % and triangle left %. Pace class
// follows those numbers. Everything else (tags, remaining classes, data-*,
// icon SVG mark/viewBox/path, labels, presence of track/bar/fill/tri)
// must match. Sidebar transcript is out of scope for this file.

'use strict';

function dumpTicker(root) {
  if (!root) return null;
  const groups = Array.from(root.querySelectorAll(':scope > .plan-group')).map((g) => {
    const icon = g.querySelector('.plan-icon svg.model-icon');
    const path = icon && icon.querySelector('path');
    const wins = Array.from(g.querySelectorAll('.plan-win')).map((w) => {
      const fill = w.querySelector('.plan-bar-fill');
      const tri = w.querySelector('.plan-tri');
      const lab = w.querySelector('.plan-win-label');
      const bar = w.querySelector('.plan-bar');
      return {
        cls: String(w.className || '').replace(/\s+/g, ' ').trim(),
        window: w.getAttribute('data-window') || '',
        pace: w.getAttribute('data-pace') || '',
        label: lab ? String(lab.textContent || '') : '',
        barAria: bar ? bar.getAttribute('aria-hidden') : null,
        triAria: tri ? tri.getAttribute('aria-hidden') : null,
        fillTag: fill ? fill.tagName.toLowerCase() : '',
        triTag: tri ? tri.tagName.toLowerCase() : '',
        fillWidth: fill && fill.style ? fill.style.width : '',
        triLeft: tri && tri.style ? tri.style.left : '',
        hasFill: !!fill,
        hasTri: !!tri,
        hasTrack: !!w.querySelector('.plan-track'),
        hasBar: !!bar,
      };
    });
    return {
      cls: String(g.className || '').replace(/\s+/g, ' ').trim(),
      provider: g.getAttribute('data-provider') || '',
      company: g.getAttribute('data-company') || '',
      iconTag: icon ? icon.tagName.toLowerCase() : '',
      iconMark: icon ? icon.getAttribute('data-mark') || '' : '',
      iconViewBox: icon ? icon.getAttribute('viewBox') || '' : '',
      iconPath: path ? path.getAttribute('d') || '' : '',
      windows: wins,
    };
  });
  return { groupCount: groups.length, groups: groups };
}

function skeleton(dump) {
  if (!dump) return dump;
  return {
    groupCount: dump.groupCount,
    groups: (dump.groups || []).map((g) => ({
      cls: g.cls,
      provider: g.provider,
      company: g.company,
      iconTag: g.iconTag,
      iconMark: g.iconMark,
      iconViewBox: g.iconViewBox,
      iconPath: g.iconPath,
      windows: (g.windows || []).map((w) => ({
        cls: w.cls.replace(/\bplan-(ahead|hot|crit|low|under|locked|exhausted)\b/g, '').replace(/\s+/g, ' ').trim(),
        window: w.window,
        label: w.label,
        barAria: w.barAria,
        triAria: w.triAria,
        fillTag: w.fillTag,
        triTag: w.triTag,
        hasFill: w.hasFill,
        hasTri: w.hasTri,
        hasTrack: w.hasTrack,
        hasBar: w.hasBar,
        fillWidth: '*',
        triLeft: '*',
      })),
    })),
  };
}

function compareTickers(oldDump, newDump) {
  const a = JSON.stringify(skeleton(oldDump));
  const b = JSON.stringify(skeleton(newDump));
  if (a === b) {
    return { ok: true, oldGroups: (oldDump && oldDump.groupCount) || 0, newGroups: (newDump && newDump.groupCount) || 0 };
  }
  return { ok: false, old: skeleton(oldDump), neu: skeleton(newDump) };
}

module.exports = {
  dumpTicker: dumpTicker,
  skeleton: skeleton,
  compareTickers: compareTickers,
};
