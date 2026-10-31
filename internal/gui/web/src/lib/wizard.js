// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Guided setup: open it from a mode, and turn its answers into the form
// of the right mode. The wizard never runs a job itself: the form previews.
import { settings, wizardDefaults } from './settings.svelte.js';
import { recommendedChoice, uniformChoice, RULE_OF } from './regime.js';

// openWizard starts a guided setup for a mode, with that mode's folders.
export function openWizard(mode) {
  const w = wizardDefaults();
  w.goal = mode;
  w.step = 1;
  w.choice = recommendedChoice();
  if (mode === 'merge') {
    w.inputs = [...settings.merge.inputs];
    w.output = settings.merge.output;
  } else if (mode === 'rearrange') {
    w.root = settings.rearrange.root;
  } else if (mode === 'golden') {
    w.rooms = [...settings.golden.rooms];
    w.host = settings.golden.host;
    w.target = settings.golden.target;
  }
  settings.wizard = w;
  settings.view = 'wizard';
}

// filtersOf maps the wizard rules onto the filter settings of a form.
function filtersOf(w, f) {
  const rules = { ...(f.rules || {}) };
  for (const [r, rule] of Object.entries(RULE_OF)) {
    if (rule !== 'big') rules[rule] = w.rules[r] !== false;
  }
  return { ...f, rules, maxSize: w.rules.big !== false ? '50G' : '0', exclude: w.exclude };
}

// namingOfWizard: the naming answers as form settings (the wizard's "clean
// names" applies to every family).
export function namingOfWizard(w) {
  return { fams: { ...w.fams }, choice: { ...w.choice }, stem: uniformChoice(w.clean ? 'clean' : 'norm'), level: uniformChoice(1), free: false, template: '' };
}

// wizardParams: the job settings the form will run (merge or rearrange), for
// the equivalent command line.
export function wizardParams(w) {
  const naming = namingOfWizard(w);
  const families = {};
  for (const [k, on] of Object.entries(naming.fams)) if (on) families[k] = { preset: naming.choice[k], stem: naming.stem[k] };
  const filters = filtersOf(w, { rules: {} });
  if (w.goal === 'rearrange' || (w.goal === 'dupes' && w.inputs.length === 1)) {
    const root = w.goal === 'rearrange' ? w.root : w.inputs[0];
    return { kind: 'rearrange', params: { root, families, filters, reportDupes: w.goal === 'dupes' || w.dupes === 'report', keepEmptyDirs: w.goal === 'rearrange' ? w.keepEmpty : true } };
  }
  return { kind: 'merge', params: { inputs: [...w.inputs], output: w.goal === 'dupes' ? w.inputs[0] : w.output, families, filters } };
}

// folders the wizard works on, by goal
export function wizardDirs(w) {
  if (w.goal === 'rearrange') return w.root ? [w.root] : [];
  if (w.goal === 'golden') return w.rooms;
  return w.inputs;
}

// finish fills the form of the goal's mode with the answers and opens it; the
// mode starts its preview (golden: indexes this computer).
export function finish(w) {
  const naming = namingOfWizard(w);
  switch (w.goal) {
    case 'merge':
      Object.assign(settings.merge, naming, { inputs: [...w.inputs], output: w.output, filters: filtersOf(w, settings.merge.filters), autoPreview: true });
      settings.view = 'merge';
      break;
    case 'rearrange':
      Object.assign(settings.rearrange, naming, { root: w.root, dupes: w.dupes, keepEmptyDirs: w.keepEmpty, filters: filtersOf(w, settings.rearrange.filters), autoPreview: true });
      settings.view = 'rearrange';
      break;
    case 'dupes':
      // one folder: rearrange preview that only reports; several: merge preview into the first
      if (w.inputs.length === 1) {
        Object.assign(settings.rearrange, naming, { root: w.inputs[0], dupes: 'report', keepEmptyDirs: true, filters: filtersOf(w, settings.rearrange.filters), autoPreview: true });
        settings.view = 'rearrange';
      } else {
        Object.assign(settings.merge, naming, { inputs: [...w.inputs], output: w.inputs[0], filters: filtersOf(w, settings.merge.filters), autoPreview: true });
        settings.view = 'merge';
      }
      break;
    case 'golden':
      Object.assign(settings.golden, { rooms: [...w.rooms], host: w.host, target: w.target, verifyFull: w.verifyFull, videoTier: w.videoTier,
        filters: filtersOf(w, settings.golden.filters), autoIndex: true });
      settings.view = 'golden';
      break;
  }
}

