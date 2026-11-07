// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// A notification of the system when a job ends while onefold is not the
// window in front: the browser's Notification API, so nothing goes through a
// server and nothing leaves the computer. Off until turned on in Config,
// which asks the browser for the permission (once per browser).
import { t } from './i18n.svelte.js';

export const supported = () => typeof window !== 'undefined' && 'Notification' in window;

// permission: 'granted', 'denied', 'default' (not asked yet) or 'unsupported'.
export const permission = () => (supported() ? Notification.permission : 'unsupported');

export async function ask() {
  if (!supported()) return 'unsupported';
  if (Notification.permission === 'default') return Notification.requestPermission();
  return Notification.permission;
}

const WORKING = new Set(['running', 'paused']);
const ENDS = new Set(['done', 'previewed', 'failed', 'stopped']);

// ended reports whether a job went from working to an end state.
export const ended = (old, j) => !!old && WORKING.has(old.status) && ENDS.has(j.status);

// jobEnded shows the notification of a job that just ended, when the window
// is hidden or not focused (in front, the job bar already says it).
export function jobEnded(j) {
  if (permission() !== 'granted' || (!document.hidden && document.hasFocus())) return;
  const kind = t('kind.' + j.kind);
  const body = j.status === 'done' && j.errors > 0 ? t('notify.body.errors', { n: j.errors }) : t('notify.body.' + j.status);
  try {
    const n = new Notification(t('notify.title.' + j.status, { kind }), { body, tag: 'onefold-' + j.id });
    n.onclick = () => {
      window.focus();
      n.close();
    };
  } catch {
    /* a browser that shows notifications only from a service worker: none */
  }
}
