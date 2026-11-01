// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Colors and layout helpers of the statistics charts.

// One calm, distinct color per file family (kinds.Families), the same in
// every chart.
export const FAMILY_COLORS = {
  photos: '#4e79a7',
  videos: '#e15759',
  audio: '#b07aa1',
  documents: '#f28e2b',
  archives: '#9c755f',
  code: '#59a14f',
  apps: '#76b7b2',
  design: '#edc948',
  games: '#ff9da7',
  data: '#86bcb6',
  other: '#bab0ac',
};
export const FAMILIES = Object.keys(FAMILY_COLORS);

export const famColor = (f) => FAMILY_COLORS[f] || FAMILY_COLORS.other;

// dominant returns the family with the most bytes of a {family: bytes} map.
export function dominant(fams) {
  let best = 'other', max = -1;
  for (const [f, b] of Object.entries(fams || {})) if (b > max) { best = f; max = b; }
  return best;
}

// squarify lays items ({value}) out in the rectangle x, y, w, h with the
// squarified treemap algorithm; it returns the items with x, y, w, h.
export function squarify(items, x, y, w, h) {
  const total = items.reduce((s, i) => s + i.value, 0);
  if (!total || w <= 0 || h <= 0) return [];
  const scale = (w * h) / total;
  const rest = items.filter((i) => i.value > 0).map((i) => ({ ...i, area: i.value * scale }));
  const out = [];
  let row = [];
  const worst = (r, side) => {
    const s = r.reduce((a, i) => a + i.area, 0);
    let max = 0, min = Infinity;
    for (const i of r) { max = Math.max(max, i.area); min = Math.min(min, i.area); }
    return Math.max((side * side * max) / (s * s), (s * s) / (side * side * min));
  };
  const place = (r) => {
    const s = r.reduce((a, i) => a + i.area, 0);
    if (w >= h) {
      const cw = s / h;
      let cy = y;
      for (const i of r) { const ih = i.area / cw; out.push({ ...i, x, y: cy, w: cw, h: ih }); cy += ih; }
      x += cw; w -= cw;
    } else {
      const ch = s / w;
      let cx = x;
      for (const i of r) { const iw = i.area / ch; out.push({ ...i, x: cx, y, w: iw, h: ch }); cx += iw; }
      y += ch; h -= ch;
    }
  };
  while (rest.length) {
    const side = Math.min(w, h);
    const next = rest[0];
    if (!row.length || worst([...row, next], side) <= worst(row, side)) {
      row.push(next);
      rest.shift();
    } else {
      place(row);
      row = [];
    }
  }
  if (row.length) place(row);
  return out;
}

// pct formats a share (0..1).
export function pct(a, b) {
  if (!b) return '–';
  const p = (100 * a) / b;
  return (p > 0 && p < 1 ? '<1' : Math.round(p)) + '%';
}
