// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Path helpers for the plan lists: paths come from the server with the
// separator of its OS (store.info.sep).
import { store } from './store.svelte.js';

export const sep = () => store.info?.sep || '/';
const fold = (s) => (sep() === '\\' ? s.toLowerCase() : s);

// within: p is inside dir (component boundary).
export function within(p, dir) {
  const d = dir.endsWith(sep()) ? dir : dir + sep();
  return fold(p).startsWith(fold(d));
}

// roomOf: the longest room holding p ('' if none) and its index in rooms.
export function roomOf(p, rooms = []) {
  let best = '', idx = -1;
  rooms.forEach((r, i) => {
    if (r && within(p, r) && r.length > best.length) { best = r; idx = i; }
  });
  return { room: best, idx };
}

// relTo: p relative to dir (p itself when not inside).
export function relTo(p, dir) {
  return dir && within(p, dir) ? p.slice(dir.length).replace(/^[\\/]+/, '') : p;
}

// join: dir + sep + rel (rel may use either separator).
export function join(dir, rel) {
  return dir.replace(/[\\/]+$/, '') + sep() + rel.replace(/[\\/]+/g, sep());
}

// ancestors: the folders above p, nearest first, down to (and without) stop.
export function ancestors(p, stop = '') {
  const out = [];
  let cur = p;
  for (;;) {
    const i = Math.max(cur.lastIndexOf('\\'), cur.lastIndexOf('/'));
    if (i <= 0) break;
    cur = cur.slice(0, i);
    if (stop && !within(cur, stop)) break;
    if (/^[A-Za-z]:$/.test(cur)) break;
    out.push(cur);
  }
  return out;
}

// breakable: a path with a zero-width break after every separator, so a
// long path wraps between folders, not inside a name.
export function breakable(p) {
  return p.replace(/([\\/])/g, '$1\u200b');
}
