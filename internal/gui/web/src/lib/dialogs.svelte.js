// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Promise-based modal dialogs rendered once by App.svelte.
export const dialogs = $state({
  picker: null, // { start, title, resolve }
  confirm: null, // { title, body, count, simple, go, resolve }
});

export function pickFolder(start = '', title = '') {
  return new Promise((resolve) => { dialogs.picker = { start, title, resolve }; });
}

// confirmDialog resolves true when the user confirms; count > 0 requires
// typing that number (deletions). opts.simple drops the "I reviewed the
// preview" check (actions without a preview); opts.go labels the button.
export function confirmDialog(title, body, count = 0, opts = {}) {
  return new Promise((resolve) => { dialogs.confirm = { title, body, count, simple: !!opts.simple, go: opts.go || '', resolve }; });
}
