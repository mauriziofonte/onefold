<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { FolderInput, FolderOutput, ShieldCheck, Shield, ChevronDown, ScanSearch, Play, RotateCcw, CircleAlert, Wand, Eraser, Trash2, SlidersHorizontal, ListChecks } from '@lucide/svelte';
  import { onMount } from 'svelte';
  import { namingParams } from '../lib/regime.js';
  import { openWizard } from '../lib/wizard.js';
  import { settings } from '../lib/settings.svelte.js';
  import { store, start, act, flash } from '../lib/store.svelte.js';
  import { t } from '../lib/i18n.svelte.js';
  import { num, bytes } from '../lib/format.js';
  import { confirmDialog } from '../lib/dialogs.svelte.js';
  import PathList from '../components/PathList.svelte';
  import PathField from '../components/PathField.svelte';
  import NamingCard from '../components/NamingCard.svelte';
  import FiltersCard from '../components/FiltersCard.svelte';
  import ResultTable from '../components/ResultTable.svelte';
  import SettingsActions from '../components/SettingsActions.svelte';
  import Funnel from '../components/Funnel.svelte';
  import PackagesBanner from '../components/PackagesBanner.svelte';
  import { applyExclusions, excludeOne } from '../lib/plan.js';
  import Stat from '../components/Stat.svelte';

  const cfg = settings.merge;
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
  const safe = $derived(cfg.safety.verify && cfg.safety.verifyCopy && cfg.safety.fsync && cfg.safety.cache);
  const hasPreview = $derived(job && s.toCopy !== undefined);
  const executed = $derived(job && job.stage === 'execute' && s.copied !== undefined);
  // source purge: a purge job of this merge
  const pjob = $derived(cfg.purgeJobId ? store.jobs[cfg.purgeJobId] : null);
  const ps = $derived(pjob?.summary || {});
  const purgeOfThis = $derived(!!pjob && pjob.params?.mergeJob === cfg.jobId);
  const canPurge = $derived(executed && job.status === 'done' && s.settled > 0);

  function params() {
    return {
      inputs: cfg.inputs, output: cfg.output.trim(), ...namingParams(cfg),
      filters: { maxSize: cfg.filters.maxSize, exclude: cfg.filters.exclude, rules: { ...cfg.filters.rules },
        excludePaths: [...(cfg.filters.excludePaths || [])], excludeGlobs: [...(cfg.filters.excludeGlobs || [])] },
      trustSparse: !cfg.safety.verify, noVerifyCopy: !cfg.safety.verifyCopy, noFsync: !cfg.safety.fsync,
      noCache: !cfg.safety.cache, workers: Number(cfg.safety.workers) || 0,
    };
  }

  // a preview or an execute of this view shows the Plan tab
  async function preview() {
    cfg.tab = 'plan';
    try {
      const j = await start('merge', params());
      cfg.jobId = j.id;
      cfg.purgeJobId = '';
      cfg.stale = false;
    } catch (e) {
      flash(e.message);
    }
  }

  async function execute() {
    let body = t('confirm.mergeBody', { n: num(s.toCopy), size: bytes(s.copyBytes), out: s.output || cfg.output });
    if (s.blocked) body += '<p>' + t('confirm.mergeBlocked', { n: num(s.blocked) }) + '</p>';
    if (!(await confirmDialog(t('confirm.mergeTitle'), body))) return;
    cfg.tab = 'plan';
    try {
      await act(job.id, 'execute', {});
    } catch (e) {
      flash(e.message);
    }
  }

  async function purgePreview() {
    try {
      const j = await start('purge', { mergeJob: job.id, trash: !!cfg.trash });
      cfg.purgeJobId = j.id;
    } catch (e) {
      flash(e.message);
    }
  }

  async function purgeExecute() {
    const how = pjob.params?.trash ? t('purge.toTrash') : t('purge.forever');
    const body = t('confirm.purgeBody', { n: num(ps.candidates), size: bytes(ps.bytes), how });
    if (!(await confirmDialog(t('confirm.purgeTitle'), body, ps.candidates || 0))) return;
    try {
      await act(pjob.id, 'execute', { confirm: ps.candidates || 0 });
    } catch (e) {
      flash(e.message);
    }
  }

  const reason = (n) => {
    const [rule, dir] = n.split('/');
    return t('crule.' + rule) + (dir ? ' · ' + t('merge.dir') : '');
  };
  // row exclusions only while the plan is a preview
  const canExclude = $derived(job?.status === 'previewed' && job.stage === 'preview');
  const onexclude = (v) => excludeOne(cfg, job, v);
  const rooms = $derived(s.rooms || []);
  let pkgs = $state([]); // folders that look like driver/installer packages
  const labels = $derived(rooms.map((_, i) => (i === 0 ? t('plan.outputBadge') : t('plan.inputBadge', { n: i }))));
  const tabs = $derived([
    { key: 'copies', label: t('merge.tab.copies'), count: s.toCopy || 0, exclude: canExclude, content: true },
    { key: 'renamed', label: t('merge.tab.renamed'), count: s.renamed || 0, exclude: canExclude, content: true },
    { key: 'blocked', label: t('merge.tab.blocked'), count: s.blocked || 0, tone: 'rose', exclude: canExclude },
    { key: 'inputDupes', label: t('merge.tab.inputDupes'), count: s.inputDupes || 0, cols: [t('common.source'), t('merge.col.kept')], exclude: canExclude, content: true },
    { key: 'outputDupes', label: t('merge.tab.outputDupes'), count: s.outputDupFiles || 0, cols: [t('common.source'), t('merge.col.kept')] },
    { key: 'excluded', label: t('merge.tab.excluded'), count: s.excluded || 0, cols: [t('common.source'), t('merge.col.reason')], note: reason },
    ...(s.failed ? [{ key: 'failed', label: t('merge.tab.failed'), count: s.failed, tone: 'rose' }] : []),
  ]);
  const purgeTabs = $derived([
    { key: 'purge', label: t('purge.tab.purge'), count: ps.candidates || 0, tone: 'rose', cols: [t('purge.col.deleted'), t('purge.col.copy')] },
    { key: 'refused', label: t('purge.tab.refused'), count: ps.refused || 0, cols: [t('common.source'), t('purge.col.why')] },
    ...(pjob?.stage === 'execute' && ps.failed ? [{ key: 'failed', label: t('purge.tab.failed'), count: ps.failed, tone: 'rose', cols: [t('common.source'), t('purge.col.why')] }] : []),
  ]);
</script>

<section class="mx-auto max-w-6xl space-y-5">
  <div class="flex items-start gap-4">
    <div class="flex-1">
      <h1 class="text-xl font-semibold text-slate-900">{t('merge.title')}</h1>
      <p class="text-slate-500">{t('merge.desc')}</p>
    </div>
    <button class="btn-ghost shrink-0" onclick={() => openWizard('merge')} data-testid="open-wizard"><Wand size={14} />{t('wizard.open')}</button>
  </div>

  <div class="flex gap-1 border-b border-slate-200" role="tablist" data-testid="mode-tabs">
    <button class="tab flex items-center gap-1.5" class:tab-on={cfg.tab !== 'plan'} onclick={() => (cfg.tab = 'settings')} data-testid="tab-settings"><SlidersHorizontal size={14} />{t('plan.tabSettings')}</button>
    <button class="tab flex items-center gap-1.5" class:tab-on={cfg.tab === 'plan'} onclick={() => (cfg.tab = 'plan')} data-testid="tab-plan"><ListChecks size={14} />{t('plan.tabPlan')}{#if hasPreview}<span class="tab-n">{num(s.toCopy || 0)}</span>{/if}</button>
  </div>

  {#if cfg.tab !== 'plan'}
  <div class="grid grid-cols-1 gap-5 lg:grid-cols-2">
    <div class="card">
      <div class="card-h"><FolderInput size={16} />{t('merge.inputs')} <span class="hint">{t('merge.inputsHint')}</span></div>
      <PathList bind:items={cfg.inputs} testid="merge-inputs" tag={(p) => (p === cfg.output.trim() ? t('merge.alsoOutput') : '')} />
    </div>
    <div class="card">
      <div class="card-h"><FolderOutput size={16} />{t('merge.output')}</div>
      <PathField bind:value={cfg.output} testid="merge-output" />
      <p class="mt-3 flex gap-1.5 text-xs text-slate-500"><ShieldCheck size={14} class="shrink-0 text-emerald-600" />{t('merge.outputHint')}</p>
    </div>
  </div>

  <NamingCard {cfg} dirs={cfg.inputs.filter((p) => p.trim() && p.trim() !== cfg.output.trim())} />

  <div class="grid grid-cols-1 gap-5 lg:grid-cols-2">
    <FiltersCard f={cfg.filters} rest={cfg.fams.rest} onexclusions={(paths, globs) => applyExclusions(cfg, job, { paths, globs })} />
    <details class="card group">
      <summary class="card-h mb-0 cursor-pointer list-none"><Shield size={16} />{t('safety.title')} <span class="hint">{safe ? t('safety.hint') : t('safety.hintOff')}</span><ChevronDown size={16} class="ml-auto transition group-open:rotate-180" /></summary>
      <div class="mt-3 space-y-2">
        <label class="chk"><input type="checkbox" bind:checked={cfg.safety.verify} />{t('safety.verify')}</label>
        <label class="chk"><input type="checkbox" bind:checked={cfg.safety.verifyCopy} />{t('safety.verifyCopy')}</label>
        <label class="chk"><input type="checkbox" bind:checked={cfg.safety.fsync} />{t('safety.fsync')}</label>
        <label class="chk"><input type="checkbox" bind:checked={cfg.safety.cache} />{t('safety.cache')}</label>
        <p class="rounded-md border border-amber-200 bg-amber-50 p-2 text-xs text-amber-700">{t('safety.warn')}</p>
        <label class="block"><span class="lbl">{t('safety.workers')}</span><input class="input mt-1 w-24" type="number" min="0" bind:value={cfg.safety.workers} /></label>
      </div>
    </details>
  </div>

  {:else}
  {#if cfg.stale}
    <div class="banner border-amber-200 bg-amber-50 text-amber-800" data-testid="plan-stale"><CircleAlert size={16} class="shrink-0" /><span class="flex-1">{t('plan.stale')}</span></div>
  {/if}
  {#if job && (job.status === 'failed' || job.status === 'interrupted' || job.status === 'stopped')}
    <div class="banner {job.status === 'failed' ? 'border-rose-200 bg-rose-50 text-rose-800' : 'border-amber-200 bg-amber-50 text-amber-800'}">
      <CircleAlert size={16} class="shrink-0" />
      <span class="flex-1">{job.status === 'failed' ? job.error : t('job.' + job.status)}</span>
      {#if job.status !== 'failed' || job.stage === 'execute'}
        <button class="btn-primary btn-sm" disabled={busy} onclick={() => act(job.id, 'continue').catch((e) => flash(e.message))}><RotateCcw size={12} />{t('job.continue')}</button>
      {/if}
    </div>
  {/if}

  {#if executed}
    <h2 class="text-base font-semibold text-slate-900">{t('merge.executed')}</h2>
    <div class="grid grid-cols-2 gap-3 md:grid-cols-4" data-testid="merge-executed">
      <Stat label={t('merge.copied')} value={num(s.copied)} sub={bytes(s.copiedBytes)} tone="text-emerald-700" ring="ring-emerald-200" />
      <Stat label={t('merge.failed')} value={num(s.failed)} tone={s.failed ? 'text-rose-600' : ''} />
      <Stat label={t('merge.pending')} value={num(s.pending)} />
      <Stat label={t('merge.missing')} value={num(s.missing)} tone={s.missing ? 'text-rose-600' : ''} />
    </div>
  {/if}

  {#if canPurge || (pjob && purgeOfThis)}
    <div class="card space-y-3 border-rose-200" data-testid="purge">
      <div class="card-h mb-0"><Eraser size={16} />{t('purge.title')} <span class="hint">{t('purge.hint')}</span></div>
      <p class="text-sm text-slate-600">{@html t('purge.desc', { n: num(s.settled || 0) })}</p>
      {#if store.naming?.trash}
        <label class="chk text-sm"><input type="checkbox" bind:checked={cfg.trash} data-testid="purge-trash" />{t('purge.trash')}</label>
      {/if}
      <div class="flex flex-wrap items-center gap-3">
        <button class="btn-ghost" disabled={busy || !canPurge} onclick={purgePreview} data-testid="purge-preview"><ScanSearch size={14} />{t('purge.preview')}</button>
        <button class="btn-danger" disabled={busy || pjob?.status !== 'previewed' || !purgeOfThis} onclick={purgeExecute} data-testid="purge-execute"><Trash2 size={14} />{t('purge.execute')}</button>
        {#if pjob && purgeOfThis && (pjob.status === 'failed' || pjob.status === 'stopped' || pjob.status === 'interrupted')}
          <span class="text-xs text-rose-700">{pjob.status === 'failed' ? pjob.error : t('job.' + pjob.status)}</span>
          <button class="btn-primary btn-sm" disabled={busy} onclick={() => act(pjob.id, 'continue').catch((e) => flash(e.message))}><RotateCcw size={12} />{t('job.continue')}</button>
        {/if}
      </div>
      {#if pjob && purgeOfThis && ps.candidates !== undefined}
        {#if pjob.stage === 'execute' && ps.deleted !== undefined}
          <div class="grid grid-cols-2 gap-3 md:grid-cols-4" data-testid="purge-executed">
            <Stat label={t('purge.deleted')} value={num(ps.deleted)} sub={bytes(ps.deletedBytes)} tone="text-emerald-700" ring="ring-emerald-200" />
            <Stat label={t('purge.dirs')} value={num(ps.dirsRemoved)} sub={t('purge.junk', { n: num(ps.junkRemoved || 0) })} />
            <Stat label={t('purge.gone')} value={num(ps.gone)} />
            <Stat label={t('merge.failed')} value={num(ps.failed)} tone={ps.failed ? 'text-rose-600' : ''} />
          </div>
        {:else}
          <div class="grid grid-cols-2 gap-3 md:grid-cols-3" data-testid="purge-result">
            <Stat testid="purge-candidates" label={t('purge.candidates')} value={num(ps.candidates)} sub={bytes(ps.bytes)} tone="text-rose-600" ring="ring-rose-200" />
            <Stat label={t('purge.refused')} value={num(ps.refused)} sub={t('purge.refusedHint')} />
            <Stat label={t('purge.how')} value={ps.trash ? t('purge.toTrash') : t('purge.forever')} />
          </div>
        {/if}
        <ResultTable jobId={pjob.id} tabs={purgeTabs} version={pjob.status + pjob.updated} />
      {/if}
    </div>
  {/if}

  {#if hasPreview}
    <h2 class="text-base font-semibold text-slate-900">{t('merge.result')}</h2>
    <Funnel {s} kind="merge" />
    <div class="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-7" data-testid="merge-result">
      <Stat label={t('merge.distinct')} value={num(s.distinct)} />
      <Stat label={t('merge.already')} value={num(s.alreadyPresent)} />
      <Stat label={t('merge.toCopy')} value={num(s.toCopy)} sub={bytes(s.copyBytes)} tone="text-indigo-700" ring="ring-indigo-200" testid="merge-tocopy" />
      <Stat label={t('merge.renamed')} value={num(s.renamed)} sub={t('merge.renamedHint')} />
      <Stat label={t('merge.blocked')} value={num(s.blocked)} sub={t('merge.blockedHint')} tone={s.blocked ? 'text-rose-600' : ''} />
      <Stat label={t('merge.outDupes')} value={num(s.outputDupFiles)} sub={t('merge.outDupesHint')} />
      <Stat label={t('merge.excluded')} value={num(s.excluded)} sub={t('merge.excludedHint')} testid="merge-excluded" />
    </div>
    <PackagesBanner {job} {cfg} {canExclude} bind:pkgs />
    <ResultTable jobId={job.id} {tabs} version={job.status + job.updated} {rooms} {labels} onexclude={canExclude ? onexclude : null} {pkgs} />
  {/if}
    {#if !job}<p class="py-8 text-center text-sm text-slate-400">{t('plan.empty')}</p>{/if}
  {/if}

  <div class="sticky -bottom-6 z-10 -mx-6 flex items-center gap-3 border-t border-slate-200 bg-slate-50/90 px-6 py-3 backdrop-blur" data-testid="actionbar">
    <button class="btn-primary" disabled={busy || !cfg.inputs.length || !cfg.output.trim() || noFamily} onclick={preview} data-testid="merge-preview"><ScanSearch size={14} />{t('common.preview')}</button>
    <button class="btn-danger" disabled={busy || job?.status !== 'previewed' || cfg.stale} onclick={execute} data-testid="merge-execute"><Play size={14} />{t('common.execute')}</button>
    <span class="text-xs text-slate-500">{busy ? t('common.running') : !cfg.inputs.length ? t('merge.noInputs') : t('common.previewFirst')}</span>
    <SettingsActions kind="merge" {params} />
  </div>

</section>
