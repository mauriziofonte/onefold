<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { Folder, CopyX, ScanSearch, Play, Trash2, RotateCcw, CircleAlert, Wand, SlidersHorizontal, ListChecks } from '@lucide/svelte';
  import { onMount } from 'svelte';
  import { namingParams } from '../lib/regime.js';
  import { openWizard } from '../lib/wizard.js';
  import { settings } from '../lib/settings.svelte.js';
  import { store, start, act, flash } from '../lib/store.svelte.js';
  import { t } from '../lib/i18n.svelte.js';
  import { num, bytes } from '../lib/format.js';
  import { confirmDialog } from '../lib/dialogs.svelte.js';
  import { join } from '../lib/paths.js';
  import { applyExclusions, excludeOne } from '../lib/plan.js';
  import PathField from '../components/PathField.svelte';
  import NamingCard from '../components/NamingCard.svelte';
  import FiltersCard from '../components/FiltersCard.svelte';
  import ResultTable from '../components/ResultTable.svelte';
  import SettingsActions from '../components/SettingsActions.svelte';
  import Funnel from '../components/Funnel.svelte';
  import PackagesBanner from '../components/PackagesBanner.svelte';
  import Stat from '../components/Stat.svelte';

  const cfg = settings.rearrange;
  const noFamily = $derived(!Object.values(cfg.fams).some(Boolean));
  // the guided setup opens this view with its answers: preview at once
  onMount(() => {
    if (cfg.autoPreview) {
      cfg.autoPreview = false;
      if (!busy) preview();
    }
  });
  const job = $derived(cfg.jobId ? store.jobs[cfg.jobId] : null);
  const s = $derived(job?.summary || {});
  const busy = $derived(!!store.active);
  const hasPreview = $derived(job && s.moves !== undefined);
  const executed = $derived(job && job.stage === 'execute' && (s.moved !== undefined));
  const canExclude = $derived(job?.status === 'previewed' && job.stage === 'preview');
  let pkgs = $state([]); // folders that look like driver/installer packages

  function params() {
    return {
      root: cfg.root.trim(), ...namingParams(cfg), reportDupes: cfg.dupes === 'report', keepEmptyDirs: cfg.keepEmptyDirs,
      filters: { maxSize: cfg.filters.maxSize, exclude: cfg.filters.exclude, rules: { ...cfg.filters.rules },
        excludePaths: [...(cfg.filters.excludePaths || [])], excludeGlobs: [...(cfg.filters.excludeGlobs || [])] },
    };
  }

  const reason = (n) => {
    const [rule, dir] = n.split('/');
    return t('crule.' + rule) + (dir ? ' · ' + t('merge.dir') : '');
  };
  // the lists of a rearrange hold paths relative to the root
  const root = $derived(s.root || cfg.root.trim());
  const abs = (p) => (/^([A-Za-z]:)?[\\/]/.test(p) ? p : join(root, p));
  const onexclude = (v) => excludeOne(cfg, job, v);

  // a preview or an execute of this view shows the Plan tab
  async function preview() {
    cfg.tab = 'plan';
    try {
      const j = await start('rearrange', params());
      cfg.jobId = j.id;
      cfg.stale = false;
    } catch (e) {
      flash(e.message);
    }
  }

  async function execute() {
    let body = t('confirm.rearrangeBody', { n: num(s.moves), root: s.root || cfg.root });
    if (s.deletes) body += t('confirm.rearrangeDeletes', { n: num(s.deletes), size: bytes(s.deleteBytes) });
    if (!(await confirmDialog(t('confirm.rearrangeTitle'), body, s.deletes || 0))) return;
    cfg.tab = 'plan';
    try {
      await act(job.id, 'execute', { confirm: s.deletes || 0 });
    } catch (e) {
      flash(e.message);
    }
  }

  const tabs = $derived([
    { key: 'moves', label: t('rearrange.movesTitle'), count: s.moves || 0, cols: [t('rearrange.now'), t('rearrange.after')], exclude: canExclude, abs },
    ...(s.deletes ? [{ key: 'deletes', label: t('rearrange.deletes'), count: s.deletes, tone: 'rose', cols: [t('rearrange.deletedCol'), t('rearrange.keptCol')],
      exclude: canExclude, content: true, abs }] : []),
    { key: 'excluded', label: t('merge.tab.excluded'), count: s.excluded || 0, cols: [t('common.source'), t('merge.col.reason')], note: reason },
  ]);
</script>

<section class="mx-auto max-w-6xl space-y-5">
  <div class="flex items-start gap-4">
    <div class="flex-1">
      <h1 class="text-xl font-semibold text-slate-900">{t('rearrange.title')}</h1>
      <p class="text-slate-500">{t('rearrange.desc')}</p>
    </div>
    <button class="btn-ghost shrink-0" onclick={() => openWizard('rearrange')} data-testid="open-wizard"><Wand size={14} />{t('wizard.open')}</button>
  </div>

  <div class="flex gap-1 border-b border-slate-200" role="tablist" data-testid="mode-tabs">
    <button class="tab flex items-center gap-1.5" class:tab-on={cfg.tab !== 'plan'} onclick={() => (cfg.tab = 'settings')} data-testid="tab-settings"><SlidersHorizontal size={14} />{t('plan.tabSettings')}</button>
    <button class="tab flex items-center gap-1.5" class:tab-on={cfg.tab === 'plan'} onclick={() => (cfg.tab = 'plan')} data-testid="tab-plan"><ListChecks size={14} />{t('plan.tabPlan')}{#if hasPreview}<span class="tab-n">{num(s.moves || 0)}</span>{/if}</button>
  </div>

  {#if cfg.tab !== 'plan'}
    <div class="grid grid-cols-1 gap-5 lg:grid-cols-3">
      <div class="card space-y-3 lg:col-span-2">
        <div class="card-h"><Folder size={16} />{t('rearrange.folder')}</div>
        <PathField bind:value={cfg.root} testid="rearrange-root" />
      </div>
      <div class="card space-y-2">
        <div class="card-h"><CopyX size={16} />{t('rearrange.dupes')}</div>
        <label class="chk"><input type="radio" bind:group={cfg.dupes} value="delete" />{t('rearrange.dupesDelete')}</label>
        <label class="chk"><input type="radio" bind:group={cfg.dupes} value="report" />{t('rearrange.dupesReport')}</label>
        <div class="border-t border-slate-100 pt-2"><label class="chk"><input type="checkbox" bind:checked={cfg.keepEmptyDirs} />{t('rearrange.keepEmpty')}</label></div>
      </div>
    </div>

    <NamingCard {cfg} dirs={[cfg.root]} />
    <FiltersCard f={cfg.filters} rest={cfg.fams.rest} onexclusions={(paths, globs) => applyExclusions(cfg, job, { paths, globs })} />
  {:else}
    {#if cfg.stale}
      <div class="banner border-amber-200 bg-amber-50 text-amber-800" data-testid="plan-stale"><CircleAlert size={16} class="shrink-0" /><span class="flex-1">{t('plan.stale')}</span></div>
    {/if}
    {#if job && (job.status === 'failed' || job.status === 'interrupted' || job.status === 'stopped')}
      <div class="banner {job.status === 'failed' ? 'border-rose-200 bg-rose-50 text-rose-800' : 'border-amber-200 bg-amber-50 text-amber-800'}">
        <CircleAlert size={16} class="shrink-0" />
        <span class="flex-1">{job.status === 'failed' ? job.error : t('job.' + job.status)}</span>
        <button class="btn-primary btn-sm" disabled={busy} onclick={() => act(job.id, 'continue').catch((e) => flash(e.message))}><RotateCcw size={12} />{t('job.continue')}</button>
      </div>
    {/if}

    {#if executed}
      <div class="grid grid-cols-2 gap-3 md:grid-cols-4" data-testid="rearrange-executed">
        <Stat label={t('rearrange.moved')} value={num(s.moved)} tone="text-emerald-700" ring="ring-emerald-200" />
        <Stat label={t('rearrange.deleted')} value={num(s.deleted)} />
        <Stat label={t('rearrange.dirs')} value={num(s.dirsRemoved)} />
        <Stat label={t('merge.failed')} value={num(s.failed)} tone={s.failed ? 'text-rose-600' : ''} />
      </div>
    {/if}

    {#if hasPreview}
      <Funnel {s} kind="rearrange" />
      <div class="grid grid-cols-2 gap-3 md:grid-cols-5" data-testid="rearrange-result">
        <Stat label={t('rearrange.files')} value={num(s.files)} />
        <Stat label={t('rearrange.stay')} value={num(s.stay)} />
        <Stat label={t('rearrange.moves')} value={num(s.moves)} tone="text-brand-700" ring="ring-brand-200" />
        <Stat testid="rearrange-deletes" label={t('rearrange.deletes')} value={num(s.deletes)} sub={t('rearrange.freed', { size: bytes(s.deleteBytes) })} tone={s.deletes ? 'text-rose-600' : ''} ring={s.deletes ? 'ring-rose-200' : ''} />
        <Stat label={t('merge.excluded')} value={num(s.excluded || 0)} sub={t('merge.excludedHint')} />
      </div>
      {#if s.deletes}
        <div class="banner border-rose-200 bg-rose-50 text-rose-800"><Trash2 size={16} />{t('rearrange.deletesBanner', { n: num(s.deletes) })}</div>
      {/if}
      <PackagesBanner {job} {cfg} {canExclude} bind:pkgs />
      <ResultTable jobId={job.id} {tabs} version={job.status + job.updated} rooms={[root]} onexclude={canExclude ? onexclude : null} {pkgs} />
    {:else if !job}
      <p class="py-8 text-center text-sm text-slate-400">{t('plan.empty')}</p>
    {/if}
  {/if}

  <div class="sticky -bottom-6 z-10 -mx-6 flex items-center gap-3 border-t border-slate-200 bg-slate-50/90 px-6 py-3 backdrop-blur" data-testid="actionbar">
    <button class="btn-primary" disabled={busy || !cfg.root.trim() || noFamily} onclick={preview} data-testid="rearrange-preview"><ScanSearch size={14} />{t('common.preview')}</button>
    <button class="btn-danger" disabled={busy || job?.status !== 'previewed' || cfg.stale} onclick={execute} data-testid="rearrange-execute"><Play size={14} />{t('common.execute')}</button>
    <span class="text-xs text-slate-500">{busy ? t('common.running') : !cfg.root.trim() ? t('rearrange.noFolder') : t('common.previewFirst')}</span>
    <SettingsActions kind="rearrange" {params} />
  </div>
</section>
