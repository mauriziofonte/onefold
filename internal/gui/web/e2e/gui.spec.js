// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// GUI end-to-end: the real binary serves the embedded frontend; the browser
// drives merge, rearrange, journals, golden archive, pause/resume, language,
// persisted settings and the resume of a run killed mid-copy (power loss).
// Data: read-only system trees as sources, scratch copies under E2E_ROOT.
import { test, expect } from '@playwright/test';
import { spawn, execFileSync } from 'node:child_process';
import { mkdirSync, readFileSync, writeFileSync, chmodSync, existsSync, readdirSync, lstatSync, createWriteStream } from 'node:fs';
import { join } from 'node:path';

const repo = new URL('../../../../', import.meta.url).pathname;
const bin = process.env.ONEFOLD_BIN || join(repo, 'onefold');
const root = process.env.E2E_ROOT || '/tmp/onefold-gui-e2e';
const state = join(root, 'state');
const shots = new URL('./screens/', import.meta.url).pathname;
const port = 47190;

let server;
function startServer() {
  // ONEFOLD_STATE_DIR: the user config of this run lives in the scratch state, never in the real one;
  // ONEFOLD_TEST_COPY_DELAY: every merge copy waits 10ms, so on a fast machine the copy phase still
  // runs when the test pauses it or kills the server mid-copy
  server = spawn(bin, ['gui', '--no-open', '--keep', '--port', String(port), '--state', state],
    { stdio: ['ignore', 'ignore', 'pipe'], env: { ...process.env, ONEFOLD_STATE_DIR: state, ONEFOLD_TEST_COPY_DELAY: '10ms' } });
  // the server's stderr (progress, and a goroutine dump on SIGQUIT) stays in the scratch dir
  server.stderr.pipe(createWriteStream(join(root, 'server.log'), { flags: 'a' }));
}
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
function count(dir) {
  let n = 0;
  const walk = (d) => {
    for (const e of readdirSync(d)) {
      if (e === '.onefold') continue;
      const p = join(d, e);
      const st = lstatSync(p);
      if (st.isDirectory()) walk(p);
      else if (st.isFile()) n++;
    }
  };
  walk(dir);
  return n;
}
const shot = (page, name) => page.screenshot({ path: join(shots, name + '.png') });
// the Filters card is collapsible: open it whatever its state
const openFilters = (page) => page.getByTestId('filters-card').evaluate((d) => { d.open = true; });
// the sticky action bar sits flush on the bottom edge of the scroll area while
// the page is longer than it (the padding of <main> left a gap content showed
// through), and the page has one scroll: no document scroll, no inner scroll
// in the plan list
async function barFlush(page) {
  // the plan list loads its rows after the summary: wait for a long page
  await expect.poll(() => page.evaluate(() => {
    const m = document.querySelector('main');
    return m.scrollHeight > m.clientHeight;
  })).toBe(true);
  const r = await page.evaluate(() => {
    const main = document.querySelector('main');
    const bar = document.querySelector('[data-testid="actionbar"]');
    main.scrollTop = 0;
    const table = document.querySelector('[data-testid="result-table"]');
    const inner = table ? [...table.querySelectorAll('*')].some((e) => e.scrollHeight > e.clientHeight + 1 && /(auto|scroll)/.test(getComputedStyle(e).overflowY)) : false;
    const se = document.scrollingElement;
    se.scrollTop = 50; // the document itself never scrolls
    const doc = se.scrollTop;
    se.scrollTop = 0;
    return { gap: main.getBoundingClientRect().bottom - bar.getBoundingClientRect().bottom, long: main.scrollHeight > main.clientHeight, doc, inner };
  });
  expect(r.long).toBe(true);
  expect(Math.abs(r.gap)).toBeLessThan(1);
  expect(r.doc).toBeLessThanOrEqual(0);
  expect(r.inner).toBe(false);
}

test.beforeAll(() => {
  if (existsSync(root)) throw new Error(`${root} exists: remove it first (scratch dir of a previous run)`);
  mkdirSync(join(root, 'OUT'), { recursive: true });
  // the output already holds part of the content: "already present"
  execFileSync('cp', ['-r', '/usr/share/doc/bash', join(root, 'OUT', 'bash-docs')]);
  // a third input with an unreadable file (an error with its path in the log)
  mkdirSync(join(root, 'IN3', 'broken'), { recursive: true });
  writeFileSync(join(root, 'IN3', 'broken', 'locked.pdf'), '%PDF-1.4 unreadable on purpose');
  chmodSync(join(root, 'IN3', 'broken', 'locked.pdf'), 0o000);
  writeFileSync(join(root, 'IN3', 'note.txt'), 'a note');
  // rearrange: a scratch copy with duplicates under copy names
  // -L: on some systems most man pages are links to /etc/alternatives,
  // which onefold (rightly) never treats as content; a dangling link only
  // makes cp complain
  const copyTree = (src, dst) => {
    try { execFileSync('cp', ['-rL', src, dst], { stdio: 'ignore' }); } catch { /* dangling links */ }
  };
  const regular = (d) => readdirSync(d).filter((f) => lstatSync(join(d, f)).isFile());
  copyTree('/usr/share/man/man1', join(root, 'TREE'));
  const some = regular(join(root, 'TREE')).slice(0, 5);
  for (const f of some) execFileSync('cp', [join(root, 'TREE', f), join(root, 'TREE', f.replace(/\.gz$/, ' (1).gz'))]);
  // guided setup: another scratch tree with copies, untouched by the rearrange step
  copyTree('/usr/share/man/man5', join(root, 'TREE2'));
  for (const f of regular(join(root, 'TREE2')).slice(0, 3)) execFileSync('cp', [join(root, 'TREE2', f), join(root, 'TREE2', f.replace(/\.gz$/, ' (1).gz'))]);
  // a Desktop to archive: content, OS droppings, shortcuts, temp and lock
  // files, unknown and missing extensions; merged, then purged
  for (const [f, body] of Object.entries({
    'desktop.ini': '[.ShellClassInfo]', 'Thumbs.db': 'thumbs', 'Chrome.lnk': 'L', 'Sito.url': '[InternetShortcut]',
    '~$Relazione.docx': 'owner', 'x.tmp': 'tmp', 'y.crdownload': 'partial', 'Relazione.docx': 'PK report', 'nota.txt': 'a note',
    'Progetti/Alfa/spec.pdf': '%PDF-1.4 spec', 'Progetti/Alfa/desktop.ini': '[.ShellClassInfo]', 'foo.qqzz': 'unknown type', 'noext': 'no extension',
  })) {
    mkdirSync(join(root, 'Desktop', f, '..'), { recursive: true });
    writeFileSync(join(root, 'Desktop', f), body);
  }
  // a tree to review: a driver package, a big file, documents
  for (const [f, body] of Object.entries({
    'COMP1/DRIVERS/Display/nv.inf': '[Version]', 'COMP1/DRIVERS/Display/nv.sys': 'MZ driver', 'COMP1/DRIVERS/Display/license.txt': 'nvidia license',
    'COMP1/DRIVERS/Display/nvdata.data': 'nvidia data', 'COMP1/doc.pdf': '%PDF-1.4 doc', 'COMP1/a.txt': 'text a', 'b.txt': 'text b',
    'big.csv': 'x,y\n'.repeat(20000),
  })) {
    mkdirSync(join(root, 'REVIEW', f, '..'), { recursive: true });
    writeFileSync(join(root, 'REVIEW', f), body);
  }
  mkdirSync(shots, { recursive: true });
  startServer();
});
test.afterAll(() => server?.kill('SIGTERM'));

test('GUI end to end', async ({ browser }) => {
  const ctx = await browser.newContext({ extraHTTPHeaders: { 'Accept-Language': 'en-US,en;q=0.9' }, permissions: ['clipboard-read', 'clipboard-write'] });
  const page = await ctx.newPage();
  const nativeDialogs = []; // every confirmation is styled: a native one is a bug
  page.on('dialog', (d) => { nativeDialogs.push(d.message()); d.dismiss(); });
  await page.goto(await sessionURL());
  await expect(page.getByTestId('wizard')).toBeVisible(); // first launch: the guided setup
  await shot(page, '00-wizard-home');
  await page.getByTestId('nav-merge').click();
  await expect(page.getByText('Merge folders').first()).toBeVisible();
  await shot(page, '01-merge-empty');

  await test.step('merge: inputs, output, live naming preview', async () => {
    for (const p of ['/usr/share/doc', '/usr/share/icons', join(root, 'IN3')]) {
      await page.getByTestId('merge-inputs-input').fill(p);
      await page.getByTestId('merge-inputs-input').press('Enter');
    }
    await page.getByTestId('merge-output').fill(join(root, 'OUT'));
    await expect(page.locator('td', { hasText: '→' }).first()).toBeVisible();
    await shot(page, '02-merge-configured');
  });

  await test.step('merge: preview with live progress', async () => {
    await page.getByTestId('merge-preview').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText(/running|preview ready/);
    await shot(page, '03-merge-preview-running');
    await expect(page.getByTestId('jobbar-status')).toHaveText('preview ready', { timeout: 5 * 60 * 1000 });
    await expect(page.getByTestId('merge-result')).toBeVisible();
    await barFlush(page);
    await page.getByTestId('logdrawer').getByText('expand').click();
    await page.waitForTimeout(900);
    await shot(page, '04-merge-preview-result-log');
    expect(count(join(root, 'OUT'))).toBeLessThan(200); // preview wrote nothing
  });

  await test.step('merge: execute, pause, resume', async () => {
    await page.getByTestId('merge-execute').click();
    await shot(page, '05-merge-confirm');
    await page.getByTestId('confirm-reviewed').check();
    await page.getByTestId('confirm-go').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('running');
    await expect(page.locator('[data-testid="jobbar"] .phase', { hasText: 'Copy + verify' })).toHaveClass(/indigo/, { timeout: 5 * 60 * 1000 });
    await page.getByTestId('job-pause').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('paused');
    await page.waitForTimeout(1500);
    await shot(page, '06-merge-paused');
    const held = count(join(root, 'OUT'));
    await page.waitForTimeout(1500);
    expect(count(join(root, 'OUT'))).toBeLessThanOrEqual(held + 32); // only copies already in flight finish
    await page.getByTestId('job-resume').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText(/running|done/);
    await page.waitForTimeout(800);
    await shot(page, '07-merge-copying');
    await expect(page.getByTestId('jobbar-status')).toHaveText('done', { timeout: 10 * 60 * 1000 });
    await expect(page.getByTestId('merge-executed')).toBeVisible();
    await page.getByTestId('jobbar-errors').click();
    await page.waitForTimeout(900);
    await shot(page, '08-merge-done-errors');
  });

  await test.step('language: Italian, persisted across reloads', async () => {
    await page.getByTestId('lang-switch').getByText('IT').click();
    await expect(page.getByText('Unisci cartelle').first()).toBeVisible();
    await shot(page, '09-merge-italian');
    await page.reload();
    await expect(page.getByText('Unisci cartelle').first()).toBeVisible();
    await expect(page.getByTestId('tab-plan')).toHaveClass(/tab-on/); // the plan of the last run, kept across reloads
    await page.getByTestId('tab-settings').click();
    await expect(page.getByTestId('merge-output')).toHaveValue(join(root, 'OUT')); // settings kept in localStorage
    await page.getByTestId('lang-switch').getByText('EN').click();
  });

  await test.step('rearrange: deletions listed first, typed confirmation, journal', async () => {
    await page.getByTestId('nav-rearrange').click();
    await page.getByTestId('rearrange-root').fill(join(root, 'TREE'));
    await expect(page.locator('td', { hasText: '→' }).first()).toBeVisible();
    await page.getByTestId('rearrange-preview').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('preview ready', { timeout: 5 * 60 * 1000 });
    await expect(page.getByTestId('rearrange-result')).toBeVisible();
    await barFlush(page);
    await shot(page, '10-rearrange-preview');
    const deletes = (await page.getByTestId('rearrange-deletes').locator('.stat-v').innerText()).replace(/\D/g, '');
    expect(Number(deletes)).toBeGreaterThanOrEqual(5); // the 5 copies made above, plus identical man pages
    await page.getByTestId('rearrange-execute').click();
    await page.getByTestId('confirm-count').fill(deletes);
    await page.getByTestId('confirm-reviewed').check();
    await shot(page, '11-rearrange-confirm');
    await page.getByTestId('confirm-go').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('done', { timeout: 5 * 60 * 1000 });
    await expect(page.getByTestId('rearrange-executed')).toBeVisible();
    await page.getByTestId('nav-journals').click();
    await expect(page.getByTestId('journals-table').getByText('complete')).toBeVisible();
    await shot(page, '12-journals');
  });

  await test.step('journals: undo through the styled confirmation', async () => {
    await page.getByTestId('journals-table').getByRole('button', { name: 'Undo moves' }).first().click();
    await expect(page.getByTestId('confirm-reviewed')).toHaveCount(0); // no preview to review: plain confirmation
    await shot(page, '12b-journals-undo-confirm');
    await page.getByTestId('confirm-go').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('done', { timeout: 5 * 60 * 1000 });
    await expect(page.getByTestId('journals-table').getByText('complete')).toHaveCount(0);
  });

  await test.step('power loss mid-copy, restart, resume from Runs', async () => {
    await page.getByTestId('nav-merge').click();
    await page.getByTestId('tab-settings').click();
    // new merge into a fresh output
    while (await page.getByTestId('merge-inputs').getByTitle('Remove').count()) await page.getByTestId('merge-inputs').getByTitle('Remove').first().click();
    await page.getByTestId('merge-inputs-input').fill('/usr/share/man');
    await page.getByTestId('merge-inputs-input').press('Enter');
    await page.getByTestId('merge-output').fill(join(root, 'OUT2'));
    await page.getByTestId('merge-preview').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('preview ready', { timeout: 5 * 60 * 1000 });
    await page.getByTestId('merge-execute').click();
    await page.getByTestId('confirm-reviewed').check();
    await page.getByTestId('confirm-go').click();
    await expect(page.locator('[data-testid="jobbar"] .phase', { hasText: 'Copy + verify' })).toHaveClass(/indigo/, { timeout: 5 * 60 * 1000 });
    await page.waitForTimeout(400);
    server.kill('SIGKILL'); // the computer is switched off
    await page.waitForTimeout(1500);
    const partial = count(join(root, 'OUT2'));
    startServer();
    await page.goto(await sessionURL());
    await expect(page.getByTestId('unfinished-banner')).toBeVisible();
    await shot(page, '13-after-restart-banner');
    await page.getByTestId('nav-runs').click();
    await expect(page.getByTestId('runs-table').getByText('interrupted')).toBeVisible();
    await shot(page, '14-runs-interrupted');
    await page.getByTestId('runs-table').getByRole('button', { name: 'Resume run' }).first().click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('done', { timeout: 10 * 60 * 1000 });
    await shot(page, '15-runs-resumed-done');
    expect(count(join(root, 'OUT2'))).toBeGreaterThan(partial);
  });

  await test.step('config: owner, signature, language (no diagnostic log outside Windows)', async () => {
    await page.getByTestId('nav-config').click();
    await expect(page.getByTestId('config-diag')).toHaveCount(0);
    await expect(page.getByTestId('config-photo-date-fallback')).not.toBeChecked(); // off by default: golden names unchanged
    await page.getByTestId('config-artist').fill('E2E Owner');
    await page.getByTestId('config-language').fill('ENG');
    await page.getByTestId('config-save').click();
    await expect(page.getByTestId('config-saved')).toBeVisible();
    await page.getByTestId('config-language').fill('e1g'); // three characters (maxlength), not a code
    await page.getByTestId('config-save').click();
    await expect(page.getByTestId('toast')).toContainText('ISO 639-2');
    await page.getByTestId('toast').getByRole('button').click();
    await page.reload();
    await page.getByTestId('nav-config').click();
    await expect(page.getByTestId('config-artist')).toHaveValue('E2E Owner');
    await expect(page.getByTestId('config-language')).toHaveValue('eng');
    await shot(page, '15b-config');
    expect(JSON.parse(readFileSync(join(state, 'config.json'), 'utf8'))).toEqual({ artist: 'E2E Owner', language: 'eng' });
  });

  await test.step('golden archive: index, plan, check, apply', async () => {
    await page.getByTestId('nav-golden').click();
    await expect(page.getByTestId('golden-owner')).toContainText('E2E Owner');
    await expect(page.getByTestId('golden-tags')).toContainText('eng');
    // the golden scope is fixed: photos and videos (the PNG icons are photos)
    await expect(page.getByTestId('golden-scope')).toHaveText('photos and videos');
    await page.getByTestId('golden-rooms-input').fill('/usr/share/icons');
    await page.getByTestId('golden-rooms-input').press('Enter');
    await page.getByRole('button', { name: 'Index → manifest' }).click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('done', { timeout: 5 * 60 * 1000 });
    await page.getByRole('button', { name: 'Plan', exact: true }).click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('done', { timeout: 5 * 60 * 1000 });
    await page.locator('input[placeholder="Absolute folder path"]').last().fill(join(root, 'GOLDEN'));
    await page.getByRole('button', { name: 'Check target' }).click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('preview ready', { timeout: 5 * 60 * 1000 });
    await shot(page, '16-golden-checked');
    await expect(page.getByTestId('golden-blocked-tools')).toHaveCount(0); // exiftool and ffmpeg are installed: apply may start
    await page.getByRole('button', { name: 'Apply…' }).click();
    await page.getByTestId('confirm-reviewed').check();
    await page.getByTestId('confirm-go').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('done', { timeout: 5 * 60 * 1000 });
    await shot(page, '17-golden-applied');
    expect(count(join(root, 'GOLDEN'))).toBeGreaterThan(100);
  });

  await test.step('doctor', async () => {
    await page.getByTestId('nav-doctor').click();
    await shot(page, '18-doctor');
  });

  await test.step('guided setup: golden scope locked, real preview on the sample', async () => {
    await page.getByTestId('nav-wizard').click();
    await page.getByTestId('goal-golden').click();
    await page.getByTestId('wizard-next').click();
    await page.getByTestId('wizard-rooms-input').fill('/usr/share/icons');
    await page.getByTestId('wizard-rooms-input').press('Enter');
    await page.getByTestId('wizard-target').fill(join(root, 'GOLDEN2'));
    await page.getByTestId('wizard-scan').click();
    await expect(page.getByTestId('wizard-scanned')).toBeVisible();
    await expect(page.getByTestId('wizard-row').first()).toBeVisible();
    await expect(page.getByTestId('wizard-preview')).toContainText('PICTURES');
    await page.getByTestId('wizard-steps').getByText('Files').click(); // the locked step
    await expect(page.getByTestId('wizard-golden-locked')).toBeVisible();
    await shot(page, '19-wizard-golden-locked');
  });

  await test.step('guided setup: rearrange with duplicates, recipe, form previews', async () => {
    await page.getByTestId('wizard-steps').getByText('Goal').click();
    await page.getByTestId('goal-rearrange').click();
    await page.getByTestId('wizard-next').click();
    await page.getByTestId('wizard-root').fill(join(root, 'TREE2'));
    await page.getByTestId('wizard-scan').click();
    await expect(page.getByTestId('wizard-scanned')).toBeVisible();
    await page.getByTestId('wizard-next').click(); // files
    await page.getByTestId('wizard-next').click(); // layout
    await expect(page.getByTestId('wizard-naming')).not.toBeEmpty();
    await page.getByTestId('wizard-next').click(); // duplicates
    await page.getByTestId('wizard-next').click(); // exclusions
    await page.getByTestId('wizard-next').click(); // summary
    await expect(page.getByTestId('wizard-cli')).toContainText('onefold rearrange');
    await expect(page.getByTestId('wizard-preview')).toContainText('duplicate');
    await page.getByTestId('wizard-recipe-name').fill('E2E recipe');
    await page.getByTestId('wizard-recipe-save').click();
    await expect(page.getByTestId('toast')).toContainText('E2E recipe');
    await shot(page, '20-wizard-summary');
    await page.getByTestId('wizard-finish').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('preview ready', { timeout: 5 * 60 * 1000 });
    await expect(page.getByTestId('tab-plan')).toHaveClass(/tab-on/); // the form previews and shows its plan
    await page.getByTestId('tab-settings').click();
    await expect(page.getByTestId('rearrange-root')).toHaveValue(join(root, 'TREE2'));
    await page.getByTestId('tab-plan').click();
    await shot(page, '21-wizard-rearrange-preview');
    expect(readdirSync(join(root, 'TREE2')).some((f) => f.endsWith(' (1).gz'))).toBe(true); // preview only
    await page.getByTestId('nav-wizard').click();
    await page.getByTestId('wizard-steps').getByText('Goal').click();
    await expect(page.getByTestId('recipe')).toContainText('E2E recipe');
  });
  await test.step('plan review: tree, row and folder exclusions, patterns, instant re-plan, package warning', async () => {
    const rev = join(root, 'REVIEW');
    const toCopy = () => page.getByTestId('merge-tocopy').locator('.stat-v');
    await page.getByTestId('nav-merge').click();
    await page.getByTestId('tab-settings').click();
    while (await page.getByTestId('merge-inputs').getByTitle('Remove').count()) await page.getByTestId('merge-inputs').getByTitle('Remove').first().click();
    await page.getByTestId('merge-inputs-input').fill(rev);
    await page.getByTestId('merge-inputs-input').press('Enter');
    await page.getByTestId('merge-output').fill(join(root, 'REVIEWOUT'));
    await page.getByTestId('merge-preview').click();
    await expect(page.getByTestId('tab-plan')).toHaveClass(/tab-on/);
    await expect(page.getByTestId('jobbar-status')).toHaveText('preview ready', { timeout: 5 * 60 * 1000 });
    await expect(toCopy()).toHaveText('6');
    await expect(page.getByTestId('funnel')).toContainText('6 to copy');
    await expect(page.getByTestId('packages')).toContainText('1 folders look like driver');
    await shot(page, '22-plan-review');
    // the tree: open the input and COMP1, exclude the DRIVERS branch at once
    await page.getByTestId('view-tree').click();
    await page.getByTestId('tree-folder').filter({ hasText: 'REVIEW' }).getByRole('button').first().click();
    await page.getByTestId('tree-folder').filter({ hasText: 'COMP1' }).getByRole('button').first().click();
    await page.getByTestId('tree-folder').filter({ hasText: 'DRIVERS' }).getByRole('button').first().click();
    await shot(page, '23-plan-tree');
    await page.getByTestId('tree-folder').filter({ hasText: 'DRIVERS' }).getByTestId('tree-exclude').click();
    await expect(toCopy()).toHaveText('4');
    await expect(page.getByTestId('packages')).toHaveCount(0);
    // the list: largest first, exclude that one file from its row
    await page.getByTestId('view-list').click();
    await page.getByTestId('sort').selectOption('-size');
    await page.getByTestId('page-size').selectOption('50');
    await expect(page.getByTestId('result-table').locator('tbody tr').first()).toContainText('big.csv');
    await page.getByTestId('row-exclude').first().click();
    await shot(page, '24-plan-row-menu');
    await page.getByTestId('exclude-file').click();
    await expect(toCopy()).toHaveText('3');
    await page.getByRole('button', { name: /^Excluded/ }).click();
    await expect(page.locator('td', { hasText: 'Excluded by you' }).first()).toBeVisible();
    // a pattern from the Filters card, then removed again: both re-plan at once
    await page.getByTestId('tab-settings').click();
    await openFilters(page);
    await expect(page.getByTestId('exclusions').getByTestId('exclusion-remove')).toHaveCount(2);
    await page.getByTestId('glob-input').fill('*.txt');
    await page.getByTestId('glob-add').click();
    await page.getByTestId('tab-plan').click();
    await expect(toCopy()).toHaveText('1');
    await page.getByTestId('tab-settings').click();
    await expect(page.getByTestId('filters-card')).toHaveAttribute('open', ''); // exclusions set: the card opens by itself
    await page.getByTestId('exclusions').locator('.tag-indigo').getByTestId('exclusion-remove').click();
    await page.getByTestId('tab-plan').click();
    await expect(toCopy()).toHaveText('3');
    await shot(page, '25-plan-reviewed');
    // the execute runs the reviewed plan
    await page.getByTestId('merge-execute').click();
    await page.getByTestId('confirm-reviewed').check();
    await page.getByTestId('confirm-go').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('done', { timeout: 5 * 60 * 1000 });
    expect(count(join(root, 'REVIEWOUT'))).toBe(3);
  });

  await test.step('merge a desktop: [folder] prefix, exclusions, settings export, purge of the sources', async () => {
    const desk = join(root, 'Desktop');
    await page.getByTestId('nav-merge').click();
    await page.getByTestId('tab-settings').click();
    while (await page.getByTestId('merge-inputs').getByTitle('Remove').count()) await page.getByTestId('merge-inputs').getByTitle('Remove').first().click();
    await page.getByTestId('merge-inputs-input').fill(desk);
    await page.getByTestId('merge-inputs-input').press('Enter');
    await page.getByTestId('merge-output').fill(join(root, 'OUT3'));
    for (const f of ['img', 'video', 'doc', 'music', 'arch', 'rest']) {
      await page.getByTestId('fam-' + f).check();
      await page.getByTestId('choice-' + f).selectOption('ext-year');
      await page.getByTestId('stem-' + f).selectOption('prefix');
    }
    await expect(page.getByTestId('level-doc')).toHaveValue('1');
    await expect(page.getByTestId('naming-spec')).toContainText('origin-1');
    await expect(page.getByTestId('catalog-preset')).toHaveText('all');
    await expect(page.locator('td', { hasText: '[Desktop] ' }).first()).toBeVisible();
    await openFilters(page);
    await expect(page.getByTestId('rule-transient')).toBeChecked();
    await expect(page.getByTestId('rule-shortcuts')).toBeChecked();
    await expect(page.getByTestId('rule-system-ext')).toBeVisible(); // with "everything else"
    await shot(page, '22-merge-desktop-prefix');
    // the settings as a file for -settings, and the equivalent command
    const [dl] = await Promise.all([page.waitForEvent('download'), page.getByTestId('merge-export').click()]);
    const exported = JSON.parse(readFileSync(await dl.path(), 'utf8'));
    expect(exported.kind).toBe('merge');
    expect(exported.families.doc).toEqual({ preset: 'ext-year', stem: 'prefix', level: 1 });
    await page.getByTestId('merge-command').click();
    await expect(page.getByTestId('toast')).toContainText('Command copied');
    expect(await page.evaluate(() => navigator.clipboard.readText())).toContain('-name doc=ext-year+prefix');

    await page.getByTestId('merge-preview').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('preview ready', { timeout: 5 * 60 * 1000 });
    // droppings, shortcuts, temp and lock files are excluded; unknown and missing extensions are not
    await expect(page.getByTestId('merge-excluded').locator('.stat-v')).toHaveText('8');
    await expect(page.getByTestId('merge-tocopy').locator('.stat-v')).toHaveText('5');
    await page.getByRole('button', { name: /^Excluded/ }).click();
    await expect(page.locator('td', { hasText: 'Skip shortcuts' }).first()).toBeVisible();
    await shot(page, '23-merge-desktop-excluded');
    await page.getByTestId('merge-execute').click();
    await page.getByTestId('confirm-reviewed').check();
    await page.getByTestId('confirm-go').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('done', { timeout: 5 * 60 * 1000 });

    // purge: preview, typed count, the sources go and so do their emptied folders
    await expect(page.getByTestId('purge')).toBeVisible();
    await page.getByTestId('purge-preview').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('preview ready', { timeout: 5 * 60 * 1000 });
    await expect(page.getByTestId('purge-candidates').locator('.stat-v')).toHaveText('5');
    expect(existsSync(join(desk, 'nota.txt'))).toBe(true); // preview only
    await shot(page, '24-purge-preview');
    await page.getByTestId('purge-execute').click();
    await page.getByTestId('confirm-count').fill('5');
    await page.getByTestId('confirm-reviewed').check();
    await page.getByTestId('confirm-go').click();
    await expect(page.getByTestId('jobbar-status')).toHaveText('done', { timeout: 5 * 60 * 1000 });
    await expect(page.getByTestId('purge-executed')).toBeVisible();
    await shot(page, '25-purge-done');
    expect(readdirSync(desk).sort()).toEqual(['Chrome.lnk', 'Sito.url', 'Thumbs.db', 'desktop.ini', 'x.tmp', 'y.crdownload', '~$Relazione.docx'].sort());
    const out = [];
    const walk = (d) => { for (const e of readdirSync(d)) { if (e === '.onefold') continue; const p = join(d, e); if (lstatSync(p).isDirectory()) walk(p); else out.push(e); } };
    walk(join(root, 'OUT3'));
    expect(out.sort()).toEqual(['[Alfa] spec.pdf', '[Desktop] Relazione.docx', '[Desktop] foo.qqzz', '[Desktop] noext', '[Desktop] nota.txt']);
  });
  expect(nativeDialogs).toEqual([]);
  await ctx.close();
});
