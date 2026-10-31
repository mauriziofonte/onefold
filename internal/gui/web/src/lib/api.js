// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// JSON API of the local server (same origin, session cookie).
async function call(method, url, body) {
  const r = await fetch(url, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  });
  let data = null;
  try { data = await r.json(); } catch { /* empty body */ }
  if (!r.ok) {
    const e = new Error(data?.error || `${r.status} ${r.statusText}`);
    e.data = data; // e.g. { needsPreview: true }
    throw e;
  }
  return data;
}

export const api = {
  get: (url) => call('GET', url),
  post: (url, body) => call('POST', url, body ?? {}),
  del: (url) => call('DELETE', url),
};

// events opens the server-sent event stream and reconnects on failure.
export function events(handlers) {
  let es;
  let closed = false;
  const open = () => {
    es = new EventSource('/api/events');
    es.onopen = () => handlers.open?.();
    es.onerror = () => {
      handlers.error?.();
      es.close();
      if (!closed) setTimeout(open, 1500);
    };
    for (const name of ['hello', 'job', 'log', 'tools']) {
      es.addEventListener(name, (e) => handlers[name]?.(JSON.parse(e.data)));
    }
  };
  open();
  return () => { closed = true; es?.close(); };
}
