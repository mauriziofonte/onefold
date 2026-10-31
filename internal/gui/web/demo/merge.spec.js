// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// README demo (playwright.demo.config.js, scripts/gui-demo.sh): merge two made-up folders into one, preview, execute.
// Nothing personal: the sandbox is written here (DEMO_ROOT, must not exist).
import { test, expect } from '@playwright/test';
import { spawn } from 'node:child_process';
import { mkdirSync, readFileSync, writeFileSync, existsSync, createWriteStream, utimesSync } from 'node:fs';
import { join } from 'node:path';

const bin = process.env.ONEFOLD_BIN;
const root = process.env.DEMO_ROOT || '/tmp/onefold-demo';
const state = join(root, 'state');
const port = 47191;
let server;

const files = {
  'Old laptop/Desktop/Budget 2023.csv': 'month,amount\njan,120\nfeb,95\n',
  'Old laptop/Desktop/Trip notes.txt': 'Lisbon, May 2023: tram 28, pastéis, the river at sunset.\n',
  'Old laptop/Desktop/Trip notes (1).txt': 'Lisbon, May 2023: tram 28, pastéis, the river at sunset.\n',
  'Old laptop/Documents/Recipes/Bread.md': '# Bread\n500 g flour, 350 g water, 10 g salt, 4 g yeast.\n',
  'Old laptop/Documents/Letter.rtf': '{\\rtf1\\ansi A letter.}',
  'USB stick/Backup/Trip notes.txt': 'Lisbon, May 2023: tram 28, pastéis, the river at sunset.\n',
  'USB stick/Backup/Recipes/Bread.md': '# Bread\n500 g flour, 350 g water, 10 g salt, 4 g yeast.\n',
  'USB stick/Backup/Recipes/Soup.md': '# Soup\nLeeks, potatoes, stock.\n',
  'USB stick/Music/playlist.m3u': '#EXTM3U\nsong one.mp3\nsong two.mp3\n',
};

test.beforeAll(() => {
  if (existsSync(root)) throw new Error(`${root} exists: remove it first`);
  for (const [p, body] of Object.entries(files)) {
    mkdirSync(join(root, p, '..'), { recursive: true });
    writeFileSync(join(root, p), body);
    const when = new Date(Date.UTC(2023, 4, 14 + p.length % 9, 10, 0, 0)); // made-up dates in 2023
    utimesSync(join(root, p), when, when);
  }
  mkdirSync(join(root, 'Archive'), { recursive: true });
  server = spawn(bin, ['gui', '--no-open', '--keep', '--port', String(port), '--state', state],
    { stdio: ['ignore', 'ignore', 'pipe'], env: { ...process.env, ONEFOLD_STATE_DIR: state } });
  server.stderr.pipe(createWriteStream(join(root, 'server.log'), { flags: 'a' }));
});
test.afterAll(() => server?.kill('SIGTERM'));

async function sessionURL() {
  for (let i = 0; i < 100; i++) {
    try {
      const l = JSON.parse(readFileSync(join(state, 'gui.json'), 'utf8'));
      const r = await fetch(l.url, { redirect: 'manual' });
      if (r.status === 303) return l.url;
    } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error('server did not start');
}

test('merge two folders', async ({ browser }) => {
  const ctx = await browser.newContext({ extraHTTPHeaders: { 'Accept-Language': 'en-US,en;q=0.9' } });
  const page = await ctx.newPage();
  const pause = (ms) => page.waitForTimeout(ms);
  // frames for the GIF (scripts/gui-demo.sh assembles them with ffmpeg): no
  // Playwright video, which would need a browser-side ffmpeg download
  const frames = join(process.env.DEMO_OUT || 'demo-results', 'frames');
  mkdirSync(frames, { recursive: true });
  let recording = true;
  const recorder = (async () => {
    for (let n = 0; recording; n++) {
      await page.screenshot({ path: join(frames, `f${String(n).padStart(5, '0')}.png`) }).catch(() => {});
      await new Promise((r) => setTimeout(r, 150));
    }
  })();
  await page.goto(await sessionURL());
  await pause(1200);
  await page.getByTestId('nav-merge').click();
  await pause(800);
  for (const p of ['Old laptop', 'USB stick']) {
    await page.getByTestId('merge-inputs-input').pressSequentially(join(root, p), { delay: 25 });
    await page.getByTestId('merge-inputs-input').press('Enter');
    await pause(500);
  }
  await page.getByTestId('merge-output').pressSequentially(join(root, 'Archive'), { delay: 25 });
  await pause(1200);
  await page.getByTestId('merge-preview').click();
  await expect(page.getByTestId('jobbar-status')).toHaveText('preview ready');
  await pause(2500);
  await page.getByTestId('merge-execute').click();
  await pause(1200);
  await page.getByTestId('confirm-reviewed').check();
  await pause(600);
  await page.getByTestId('confirm-go').click();
  await expect(page.getByTestId('jobbar-status')).toHaveText('done');
  await pause(2500);
  recording = false;
  await recorder;
  await ctx.close();
});
