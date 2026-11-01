// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Writes dist/.source-hash: the hash of the frontend sources the dist was built
// from. The Go test TestWebDistInSync recomputes it (same algorithm, CR bytes
// dropped so a CRLF checkout hashes the same) and fails when dist is stale.
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { join, relative, sep } from 'node:path';

const root = new URL('..', import.meta.url).pathname;
const fixed = ['index.html', 'package.json', 'package-lock.json', 'vite.config.js'];
const files = [];
const walk = (d) => {
  for (const n of readdirSync(d)) {
    const p = join(d, n);
    if (statSync(p).isDirectory()) walk(p);
    else files.push(relative(root, p).split(sep).join('/'));
  }
};
walk(join(root, 'src'));
for (const f of fixed) files.push(f);
files.sort();
const h = createHash('sha256');
for (const f of files) {
  const body = readFileSync(join(root, f)).filter((b) => b !== 13);
  h.update(f + '\n' + createHash('sha256').update(body).digest('hex') + '\n');
}
writeFileSync(join(root, 'dist', '.source-hash'), h.digest('hex') + '\n');
