// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// User settings persisted in localStorage: last view, the options of every
// mode, the last run of each mode, the guided setup in progress, language, log
// drawer.
import { allFams, uniformChoice, migrate, migrateRules, RULES } from './regime.js';

const KEY = 'onefold.settings.v1';

// filters of a mode: size cap, exclusions and the skip rules by name
// (catalog.Rules; a missing rule is on).
// excludePaths / excludeGlobs: the user's exclusions.
const filters = () => ({ maxSize: '50G', exclude: '', rules: {}, excludePaths: [], excludeGlobs: [] });

// naming of merge/rearrange: file families, per family a preset
// valid for it, a stem mode (norm, clean, raw, prefix) and the folder level
// of the prefix; or a free template (expert) that keeps the families' scope.
const namingCfg = () => ({
  fams: allFams(true), choice: uniformChoice('ext-year'), stem: uniformChoice('norm'), level: uniformChoice(1), free: false, template: '',
});

export const wizardDefaults = () => ({
  step: 0, goal: '', inputs: [], output: '', root: '', rooms: [], host: '', target: '',
  fams: { ...allFams(false), img: true, video: true }, famsAuto: true, choice: null, clean: false,
  dupes: 'delete', keepEmpty: false, videoTier: false, verifyFull: true,
  rules: Object.fromEntries(RULES.map((r) => [r, true])), exclude: '', recipe: '',
});

export const defaults = () => ({
  lang: '',
  view: 'wizard',
  drawerOpen: false,
  logFilter: 'all',
  pickerLast: '',
  notice: '', // a one-time notice key (settings migrated)
  wizard: wizardDefaults(),
  merge: {
    inputs: [], output: '', ...namingCfg(), filters: filters(),
    safety: { verify: true, verifyCopy: true, fsync: true, cache: true, clone: false, workers: 0 }, jobId: '', autoPreview: false,
    purgeJobId: '', trash: false, tab: 'settings', stale: false,
  },
  rearrange: { root: '', ...namingCfg(), dupes: 'delete', keepEmptyDirs: false, filters: filters(), jobId: '', autoPreview: false,
    tab: 'settings', stale: false },
  // plan lists: rows per page (or 'all'), order, list or tree
  list: { limit: '100', sort: '', view: 'list' },
  golden: {
    rooms: [], host: '', filters: filters(), manifests: [], verifyFull: true, videoTier: false,
    plan: '', target: '', onlyHost: '', allowUnstamped: false, indexJobId: '', planJobId: '', applyJobId: '', autoIndex: false,
  },
  journals: { root: '' },
  stats: { roots: [], dups: 'fast', allVolumes: false, jobId: '', dedupeJobId: '',
    clean: { keep: 'oldest', keepIn: '', action: 'trash', scriptDir: '', ps: null } },
});

// fill completes saved settings with the defaults (new versions add keys).
// An empty default object is an open map (filter rules): kept as saved.
function fill(saved, def) {
  if (Array.isArray(def)) return Array.isArray(saved) ? saved : def;
  if (def === null) return saved ?? null;
  if (def && typeof def === 'object' && Object.keys(def).length === 0) {
    return saved && typeof saved === 'object' && !Array.isArray(saved) ? saved : def;
  }
  if (def && typeof def === 'object') {
    const out = {};
    for (const k of Object.keys(def)) out[k] = fill(saved?.[k], def[k]);
    return out;
  }
  return saved === undefined || saved === null || typeof saved !== typeof def ? def : saved;
}

function load() {
  try {
    const stored = localStorage.getItem(KEY);
    const raw = JSON.parse(stored || '{}');
    // settings of older versions (C7): preset media and golden tree presets
    // leave merge/rearrange before the defaults fill the new keys
    let notice = '';
    for (const m of ['merge', 'rearrange']) {
      if (raw[m] && !raw[m].fams) notice = migrate(raw[m]) || notice;
      migrateRules(raw[m]);
    }
    migrateRules(raw.golden);
    const s = fill(raw, defaults());
    if (notice) s.notice = notice;
    return s;
  } catch {
    return defaults();
  }
}

export const settings = $state(load());

export function save() {
  try {
    localStorage.setItem(KEY, JSON.stringify($state.snapshot(settings)));
  } catch {
    /* storage full or disabled: settings stay for this session */
  }
}
