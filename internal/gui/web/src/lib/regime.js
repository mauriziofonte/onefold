// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Naming regimes on the client: the file families of merge and
// rearrange and the naming presets valid for each (display data). The server
// composes the naming of the families (naming.Compose) and applies the rules
// again (catalog.CheckTemplateScope, naming.CheckTemplate): preset media and
// the golden tree presets (library*) never reach merge or rearrange.

// FAMILIES: catalog preset, naming selector (the selector of the preset that
// targets the family), presets specific to the family, the gallery of the
// guided setup (first = recommended).
export const FAMILIES = [
  { key: 'img', preset: 'image', sel: 'photo,image', specific: ['photo-timestamp', 'photo-month', 'devices'],
    chips: ['photo-timestamp', 'photo-month', 'devices', 'date-tree', 'ext-year'] },
  { key: 'video', preset: 'video', sel: 'video', specific: ['video-tier'], chips: ['video-tier', 'date-tree', 'ext-year'] },
  { key: 'doc', preset: 'document', sel: 'PDF,DOC,XLS,PPT,TXT,EBOOK,EMAIL,CONTACT', specific: ['documents'],
    chips: ['documents', 'date-tree', 'ext-year'] },
  { key: 'music', preset: 'audio', sel: 'audio', specific: ['music'], chips: ['music', 'ext-year'] },
  { key: 'arch', preset: 'archive', sel: 'archive', specific: [], chips: ['ext-year', 'ext-size', 'ext-flat'] },
  { key: 'rest', preset: null, sel: '*', specific: [], chips: ['ext-year', 'ext-size', 'ext-flat'] },
];
export const FAM_KEYS = FAMILIES.map((f) => f.key);

// GENERIC presets (selector *) are valid for every family.
export const GENERIC = ['ext-year', 'date-tree', 'ext-flat', 'ext-size', 'ext-era-year', 'ext-era-year-month',
  'ext-era-year-size-month', 'ext-era-year-origin', 'ext-era-year-origin-mtime'];

// RULES: exclusion rules of the wizard (server wizard.go wizardRules) and the
// catalog rule each one sets (catalog.Rules).
export const RULES = ['system', 'cloud', 'dev', 'junk', 'big', 'transient', 'shortcuts'];
export const RULE_OF = { system: 'system-dirs', cloud: 'cloud-dirs', dev: 'dev-dirs', junk: 'junk', big: 'big', transient: 'transient', shortcuts: 'shortcuts' };

// STEMS: what a family does with the file name (naming.StemModes).
export const STEMS = ['norm', 'clean', 'raw', 'prefix'];
export const MAX_LEVEL = 9;

export const fam = (k) => FAMILIES.find((f) => f.key === k);
export const recommended = (k) => fam(k).specific[0] || 'ext-year';
export const optionsFor = (k) => [...fam(k).specific, ...GENERIC];
export const allFams = (on) => Object.fromEntries(FAM_KEYS.map((k) => [k, on]));
export const recommendedChoice = () => Object.fromEntries(FAM_KEYS.map((k) => [k, recommended(k)]));
export const uniformChoice = (p) => Object.fromEntries(FAM_KEYS.map((k) => [k, p]));

// isGoldenTree mirrors naming.GoldenTree (resolved spec) and the -layout aliases.
const LAYOUT_ALIASES = ['dedupe', 'timeline', 'document', 'device'];
export function isGoldenTree(spec) {
  const s = (spec || '').trim().replace(/-clean$/, '');
  return s === 'library' || s.startsWith('library-') || LAYOUT_ALIASES.includes(s);
}

// catalogPreset: the CSV union of the families, "all" with "everything else"
// (display; the server derives it again, naming.CatalogPreset).
export function catalogPreset(fams) {
  if (fams.rest) return 'all';
  return FAMILIES.filter((f) => f.preset && fams[f.key]).map((f) => f.preset).join(',');
}

// familiesOf: the naming of the selected families as the server takes it
// (settings.Naming.Families): { key: { preset, stem, level } }.
export function familiesOf(c) {
  const out = {};
  for (const f of FAMILIES) {
    if (!c.fams[f.key]) continue;
    const stem = c.stem?.[f.key] || 'norm';
    out[f.key] = { preset: c.choice[f.key], stem, ...(stem === 'prefix' ? { level: Number(c.level?.[f.key]) || 1 } : {}) };
  }
  return out;
}

// namingParams: the naming a merge/rearrange job receives from its settings
// { fams, choice, stem, level, free, template }: the families compose on the
// server; a free template replaces their naming and keeps their scope.
export function namingParams(c) {
  return { families: familiesOf(c), template: c.free ? c.template : '' };
}

// Selector -> family of a free template, for the "rule without effect" warning.
const KIND_FAM = { photo: 'img', image: 'img', video: 'video', audio: 'music', archive: 'arch', document: 'doc', text: 'doc', other: 'rest' };
const GROUP_FAM = { PDF: 'doc', DOC: 'doc', XLS: 'doc', PPT: 'doc', TXT: 'doc', EBOOK: 'doc', EMAIL: 'doc', CONTACT: 'doc',
  IMAGE: 'img', RAW: 'img', VECTOR: 'img', DESIGN: 'img', VIDEO: 'video', AUDIO: 'music', PLAYLIST: 'music', ARCHIVE: 'arch', DISK: 'arch' };

// idleSelectors lists the selectors of a free template that touch no selected family.
export function idleSelectors(template, fams) {
  if (fams.rest) return [];
  const out = [];
  for (const part of (template || '').split(';')) {
    const i = part.indexOf('=');
    if (i <= 0) continue;
    for (const s of part.slice(0, i).split(',').map((x) => x.trim())) {
      if (!s || s === '*' || s.startsWith('.')) continue;
      const f = KIND_FAM[s] || GROUP_FAM[s] || (s === s.toUpperCase() ? 'rest' : null);
      if (f && !fams[f]) out.push(s);
    }
  }
  return out;
}

// migrate converts the naming/preset of settings saved by older versions
// (C7): preset media -> photos and videos, golden tree presets -> ext-year.
// Returns a notice key when something was changed.
export function migrate(cfg) {
  if (cfg.fams) return '';
  let notice = '';
  const presets = (cfg.filters?.preset || 'all').split(',').map((p) => p.trim());
  const fams = allFams(false);
  for (const p of presets) {
    if (p === 'media') { fams.img = true; fams.video = true; notice = 'regime.migrated'; continue; }
    const f = FAMILIES.find((x) => x.preset === p);
    if (f) fams[f.key] = true;
    else fams.rest = true; // all, common, personal, visual, code: the whole scope
  }
  cfg.fams = fams;
  let n = cfg.naming || 'ext-year';
  cfg.choice = uniformChoice('ext-year');
  cfg.free = false;
  if (n === 'custom') {
    cfg.free = true;
  } else if (isGoldenTree(n)) {
    notice = 'regime.migrated';
  } else if (GENERIC.includes(n)) {
    cfg.choice = uniformChoice(n);
  } else {
    const f = FAMILIES.find((x) => x.specific.includes(n));
    if (f) cfg.choice[f.key] = n;
  }
  delete cfg.naming;
  if (cfg.filters) delete cfg.filters.preset;
  return notice;
}

// migrateRules turns the skip fields of older filter settings into the rule
// map (catalog.Rules names) and the global "clean" into per-family stems.
export function migrateRules(cfg) {
  const f = cfg?.filters;
  if (f && !f.rules) {
    f.rules = {};
    const map = { skipSystemDirs: 'system-dirs', skipCloudDirs: 'cloud-dirs', skipDevDirs: 'dev-dirs', skipNoExt: 'noext' };
    for (const [k, r] of Object.entries(map)) {
      if (f[k] === false) f.rules[r] = false;
      delete f[k];
    }
    if (f.keepJunk) f.rules.junk = false;
    delete f.keepJunk;
  }
  if (cfg && cfg.fams && !cfg.stem) {
    cfg.stem = Object.fromEntries(FAM_KEYS.map((k) => [k, cfg.clean ? 'clean' : 'norm']));
    delete cfg.clean;
  }
}
