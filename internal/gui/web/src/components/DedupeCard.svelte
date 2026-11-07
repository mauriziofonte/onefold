<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  // The duplicate cleanup of the Statistics view: one copy of each group
  // stays (a rule picks it), the others go to the Trash, become hard links
  // or clones of it, or are listed in a deletion script. A preview first;
  // the execute needs the typed count.
  import { Eraser, ScanSearch, Trash2, RotateCcw, FileCode } from '@lucide/svelte';
  import { settings } from '../lib/settings.svelte.js';
  import { store, start, act, flash } from '../lib/store.svelte.js';
  import { t } from '../lib/i18n.svelte.js';
  import { num, bytes } from '../lib/format.js';
  import { confirmDialog } from '../lib/dialogs.svelte.js';
  import PathField from './PathField.svelte';
  import ResultTable from './ResultTable.svelte';
  import Stat from './Stat.svelte';

  let { roots = [] } = $props();

  const cfg = settings.stats.clean;
  const busy = $derived(!!store.active);
  const job = $derived(settings.stats.dedupeJobId ? store.jobs[settings.stats.dedupeJobId] : null);
  const s = $derived(job?.summary || {});
  const trashOK = $derived(!!store.naming?.trash);
  const actions = $derived([...(trashOK ? ['trash'] : []), 'link', 'clone', 'script']);
  $effect(() => {
    if (!actions.includes(cfg.action)) cfg.action = actions[0];
  });
  const executed = $derived(job?.stage === 'execute' && s.done !== undefined);
  const ps = $derived(cfg.ps ?? navigator.userAgent.includes('Windows'));

  function scriptPath() {
    const dir = cfg.scriptDir.trim().replace(/[\\/]+$/, '');
    if (!dir) return '';
    const sep = dir.includes('\\') ? '\\' : '/';
    const stamp = new Date().toISOString().slice(0, 19).replace(/[-:]/g, '').replace('T', '-');
    return dir + sep + 'onefold-dedupe-' + stamp + (ps ? '.ps1' : '.sh');
  }

  async function preview() {
    const p = { roots, keep: cfg.keep, keepIn: cfg.keepIn.trim(), action: cfg.action };
    if (cfg.action === 'script') {
      p.script = scriptPath();
      p.ps = ps;
      if (!p.script) return flash(t('dedupe.scriptDirNeeded'));
    }
    try {
      const j = await start('dedupe', p);
      settings.stats.dedupeJobId = j.id;
    } catch (e) {
      flash(e.message);
    }
  }

  async function execute() {
    const a = job.params?.action;
    const body = t('confirm.dedupeBody.' + a, { n: num(s.items), size: bytes(s.bytes), script: job.params?.script || '' });
    const typed = a === 'script' ? 0 : s.items || 0;
    if (!(await confirmDialog(t('confirm.dedupeTitle'), body, typed))) return;
    try {
      await act(job.id, 'execute', { confirm: s.items || 0 });
    } catch (e) {
      flash(e.message);
    }
  }

  const tabs = $derived([
    { key: 'dedupe', label: t('dedupe.tab.items'), count: s.items || 0, tone: 'rose', cols: [t('dedupe.col.extra'), t('dedupe.col.kept')] },
    { key: 'skipped', label: t('dedupe.tab.skipped'), count: s.skipped || 0, cols: [t('common.source'), t('dedupe.col.why')] },
    ...(job?.stage === 'execute' && s.failed ? [{ key: 'failed', label: t('dedupe.tab.failed'), count: s.failed, tone: 'rose', cols: [t('common.source'), t('dedupe.col.why')] }] : []),
  ]);
</script>

<div class="space-y-3 rounded-lg border border-rose-200 p-4" data-testid="dedupe">
  <div class="card-h mb-0"><Eraser size={16} />{t('dedupe.title')} <span class="hint">{t('dedupe.hint')}</span></div>
  <p class="text-sm text-slate-600">{t('dedupe.desc')}</p>
  <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
    <label class="block">
      <span class="lbl">{t('dedupe.keep')}</span>
      <select class="input mt-1" bind:value={cfg.keep} data-testid="dedupe-keep">
        {#each ['oldest', 'newest', 'shortest', 'first'] as k (k)}<option value={k}>{t('dedupe.keep.' + k)}</option>{/each}
      </select>
    </label>
    <div>
      <span class="lbl">{t('dedupe.keepIn')}</span>
      <div class="mt-1"><PathField bind:value={cfg.keepIn} placeholder={t('dedupe.keepInPlaceholder')} testid="dedupe-keep-in" /></div>
    </div>
  </div>
  <fieldset>
    <legend class="lbl">{t('dedupe.action')}</legend>
    <div class="mt-1 space-y-1">
      {#each actions as a (a)}
        <label class="chk text-sm"><input type="radio" name="dedupe-action" value={a} bind:group={cfg.action} data-testid={'dedupe-action-' + a} />{t('dedupe.action.' + a)}</label>
        <p class="ml-6 text-xs text-slate-500">{t('dedupe.action.' + a + 'Hint')}</p>
      {/each}
    </div>
  </fieldset>
  {#if cfg.action === 'script'}
    <div>
      <span class="lbl">{t('dedupe.scriptDir')}</span>
      <div class="mt-1"><PathField bind:value={cfg.scriptDir} testid="dedupe-script-dir" /></div>
    </div>
  {/if}
  <div class="flex flex-wrap items-center gap-3">
    <button class="btn-ghost" disabled={busy || !roots.length} onclick={preview} data-testid="dedupe-preview"><ScanSearch size={14} />{t('dedupe.preview')}</button>
    <button class="btn-danger" disabled={busy || job?.status !== 'previewed' || !s.items} onclick={execute} data-testid="dedupe-execute">
      {#if job?.params?.action === 'script'}<FileCode size={14} />{t('dedupe.writeScript')}{:else}<Trash2 size={14} />{t('dedupe.execute')}{/if}
    </button>
    {#if job && (job.status === 'failed' || job.status === 'stopped' || job.status === 'interrupted')}
      <span class="text-xs text-rose-700">{job.status === 'failed' ? job.error : t('job.' + job.status)}</span>
      {#if job.stage === 'execute'}<button class="btn-primary btn-sm" disabled={busy} onclick={() => act(job.id, 'continue').catch((e) => flash(e.message))}><RotateCcw size={12} />{t('job.continue')}</button>{/if}
    {/if}
  </div>
  {#if job && s.items !== undefined}
    {#if executed}
      <div class="grid grid-cols-2 gap-3 md:grid-cols-3" data-testid="dedupe-executed">
        <Stat label={t('dedupe.done.' + job.params?.action)} value={num(s.done)} sub={bytes(s.doneBytes)} tone="text-emerald-700" ring="ring-emerald-200" />
        <Stat label={t('dedupe.failed')} value={num(s.failed)} tone={s.failed ? 'text-rose-600' : ''} />
        {#if s.script}<Stat label={t('dedupe.script')} value={s.script.split(/[\\/]/).pop()} sub={t('dedupe.scriptRun')} />{/if}
      </div>
    {:else}
      <div class="grid grid-cols-2 gap-3 md:grid-cols-3" data-testid="dedupe-result">
        <Stat testid="dedupe-items" label={t('dedupe.items')} value={num(s.items)} sub={t('dedupe.groupsN', { n: num(s.groups) })} tone="text-rose-600" ring="ring-rose-200" />
        <Stat label={t('dedupe.frees')} value={bytes(s.bytes)} />
        <Stat label={t('dedupe.skipped')} value={num(s.skipped)} />
      </div>
    {/if}
    <ResultTable jobId={job.id} {tabs} version={job.status + job.updated} />
  {/if}
</div>
