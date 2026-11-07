// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

import { api, events } from './api.js';
import { settings } from './settings.svelte.js';
import { ended, jobEnded } from './notify.js';

// Global reactive state shared by every view.
export const store = $state({
  info: null,
  jobs: {}, // id -> job
  active: '',
  log: [], // live entries of this session
  jobLog: null, // { id, entries } when the drawer shows a past run
  connected: false,
  toast: '',
  toastOk: false, // a confirmation, not an error
  tools: null, // downloadable tool packages + download progress (/api/tools)
  config: null, // user config + defaults (/api/config)
  naming: null, // presets, families, stem modes, skip rules (/api/naming)
});

export async function refreshConfig() {
  store.config = await api.get('/api/config');
}

// saveConfig stores the user config on the server (validated there).
export async function saveConfig(c) {
  store.config = await api.post('/api/config', c);
}

const MAX_LOG = 3000;

function upsert(j) {
  const old = store.jobs[j.id];
  // an HTTP response can arrive after a newer SSE snapshot: keep the newest
  if (old && old.updated > j.updated) return;
  store.jobs[j.id] = j;
  if (settings.notify && ended(old, j)) jobEnded(j);
  if (j.status === 'running' || j.status === 'paused') store.active = j.id;
  else if (store.active === j.id) store.active = '';
}

export async function refreshJobs() {
  const r = await api.get('/api/jobs');
  const map = {};
  for (const j of r.jobs) map[j.id] = j;
  store.jobs = map;
  store.active = r.active || '';
}

// onTools applies a tools snapshot; when a download batch ends the resolved
// tool paths in info change: reload them.
async function onTools(snap) {
  // an HTTP answer can arrive after a newer SSE snapshot: keep the newest
  if (store.tools && snap.seq < store.tools.seq) return;
  const ended = store.tools?.running && !snap.running;
  store.tools = snap;
  if (ended) store.info = await api.get('/api/info');
}

export async function refreshTools() {
  onTools(await api.get('/api/tools'));
}

export async function installTools(ids) {
  onTools(await api.post('/api/tools/install', { ids }));
}

export async function cancelTools() {
  onTools(await api.post('/api/tools/cancel'));
}

export async function init() {
  store.info = await api.get('/api/info');
  await refreshConfig();
  await refreshTools();
  await refreshJobs();
  const l = await api.get('/api/log');
  store.log = l.entries || [];
  events({
    open: () => { store.connected = true; },
    error: () => { store.connected = false; },
    hello: () => { refreshJobs(); refreshTools(); },
    job: upsert,
    tools: onTools,
    log: (e) => {
      store.log.push(e);
      if (store.log.length > MAX_LOG) store.log.splice(0, store.log.length - MAX_LOG);
    },
  });
}

export function jobsSorted() {
  return Object.values(store.jobs).sort((a, b) => (a.created < b.created ? 1 : -1));
}

export async function start(kind, params) {
  const j = await api.post('/api/jobs', { kind, params });
  upsert(j);
  return j;
}

export async function act(id, action, body) {
  const j = await api.post(`/api/jobs/${id}/${action}`, body);
  if (j) upsert(j);
  return j;
}

export async function removeJob(id) {
  await api.del(`/api/jobs/${id}`);
  delete store.jobs[id];
}

export function flash(msg, ok = false) {
  store.toast = msg;
  store.toastOk = ok;
  setTimeout(() => { if (store.toast === msg) store.toast = ''; }, 6000);
}
