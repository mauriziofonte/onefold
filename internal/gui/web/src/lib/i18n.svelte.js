// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// The strings of every language, one JSON file each (translated on Weblate,
// see TRANSLATING.md); en.json is the source and the fallback of a missing key.
import en from './locales/en.json';
import it from './locales/it.json';
import es from './locales/es.json';
import fr from './locales/fr.json';
import de from './locales/de.json';
import ptBR from './locales/pt-BR.json';

const dicts = { en, it, es, fr, de, 'pt-BR': ptBR };
export const LANGS = ['en', 'it', 'es', 'fr', 'de', 'pt-BR'];

// the language names in the switch, each in its own language
export const LANG_NAMES = { en: 'English', it: 'Italiano', es: 'Español', fr: 'Français', de: 'Deutsch', 'pt-BR': 'Português (Brasil)' };
const FORMATS = { it: 'it-IT', es: 'es-ES', fr: 'fr-FR', de: 'de-DE', 'pt-BR': 'pt-BR' };

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

// locale formats dates and numbers: the region of each translation, or for
// English the browser's own variant (en-GB, en-IN...), US English otherwise.
export function locale() {
  if (FORMATS[lang.current]) return FORMATS[lang.current];
  const nav = typeof navigator !== 'undefined' ? navigator.language || '' : '';
  return /^en(-|$)/i.test(nav) ? nav : 'en-US';
}
