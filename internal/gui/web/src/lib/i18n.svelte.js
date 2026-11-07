// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// The strings of every language, one JSON file each; en.json is the source
// and the fallback of a missing key.
import en from './locales/en.json';
import it from './locales/it.json';

const dicts = { en, it };
export const LANGS = ['en', 'it'];

// the language names in the switch, each in its own language
export const LANG_NAMES = { en: 'English', it: 'Italiano' };

// lang.current is reactive: components re-render when the language changes.
export const lang = $state({ current: 'en' });

export function setLang(l) {
  lang.current = dicts[l] ? l : 'en';
  document.documentElement.lang = lang.current;
}

// t looks a key up in the current language (English fallback) and fills {name}.
export function t(key, args) {
  const d = dicts[lang.current] || en;
  let s = d[key] ?? en[key] ?? key;
  if (args) {
    for (const [k, v] of Object.entries(args)) s = s.replaceAll('{' + k + '}', String(v));
  }
  return s;
}

// locale formats dates and numbers: Italian, or the browser's own English
// variant (en-GB, en-IN...), US English otherwise.
export function locale() {
  if (lang.current === 'it') return 'it-IT';
  const nav = typeof navigator !== 'undefined' ? navigator.language || '' : '';
  return /^en(-|$)/i.test(nav) ? nav : 'en-US';
}
