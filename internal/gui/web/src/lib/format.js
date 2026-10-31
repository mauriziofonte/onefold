// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

import { locale } from './i18n.svelte.js';

export function num(n) {
  return new Intl.NumberFormat(locale()).format(n ?? 0);
}

export function bytes(n) {
  n = n ?? 0;
  const u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return new Intl.NumberFormat(locale(), { maximumFractionDigits: i === 0 ? 0 : n < 10 ? 2 : 1 }).format(n) + ' ' + u[i];
}

export function amount(v, unit) {
  return unit === 'bytes' ? bytes(v) : num(v);
}

export function rate(v, unit) {
  if (!v || v < 0) return '';
  return unit === 'bytes' ? bytes(v) + '/s' : num(Math.round(v)) + '/s';
}

export function duration(sec) {
  if (sec == null || sec < 0 || !isFinite(sec)) return '–';
  sec = Math.round(sec);
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60;
  if (h) return `${h}h ${String(m).padStart(2, '0')}m`;
  if (m) return `${m}m ${String(s).padStart(2, '0')}s`;
  return `${s}s`;
}

export function when(iso) {
  if (!iso || iso.startsWith('0001')) return '';
  const d = new Date(iso);
  return new Intl.DateTimeFormat(locale(), { dateStyle: 'short', timeStyle: 'short' }).format(d);
}

export function clock(iso) {
  const d = new Date(iso);
  return d.toTimeString().slice(0, 8);
}
