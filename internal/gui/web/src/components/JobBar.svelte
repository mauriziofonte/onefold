<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { Check, LoaderCircle, Circle, CircleCheck, CircleAlert, Pause, Play, Square, RotateCcw, X } from '@lucide/svelte';
  import { store, act, jobsSorted } from '../lib/store.svelte.js';
  import { t } from '../lib/i18n.svelte.js';
  import { settings } from '../lib/settings.svelte.js';
  import { confirmDialog } from '../lib/dialogs.svelte.js';
  import { amount, rate, duration, num } from '../lib/format.js';

  let dismissed = $state('');
  let { onopen } = $props();

  const job = $derived(store.active ? store.jobs[store.active] : jobsSorted().find((j) => j.id !== dismissed && Date.now() - new Date(j.updated) < 6 * 3600e3));
  const live = $derived(job && (job.status === 'running' || job.status === 'paused'));
  const p = $derived(job?.progress);
  const pct = $derived(p && p.total > 0 ? Math.min(100, Math.round((p.done / p.total) * 100)) : null);
  const idx = $derived(job ? job.phases.indexOf(job.phase) : -1);

  function phaseState(i) {
    if (!live) return job.status === 'done' || job.status === 'previewed' ? 'done' : i < idx ? 'done' : i === idx ? 'cur' : 'todo';
    return i < idx ? 'done' : i === idx ? 'cur' : 'todo';
  }

  async function stop() {
    if (await confirmDialog(t('job.stopTitle'), t('job.stopConfirm'), 0, { simple: true, go: t('job.stop') })) act(job.id, 'stop');
  }
  function errorsLabel(n) {
    return n === 1 ? t('job.error1') : t('job.errors', { n: num(n) });
  }
  function openErrors() {
    settings.drawerOpen = true;
    settings.logFilter = 'error';
  }
</script>

<section class="shrink-0 border-b border-slate-200 bg-white px-4 py-2.5" data-testid="jobbar">
  {#if !job}
    <div class="flex items-center gap-3 text-slate-500"><span class="h-2 w-2 rounded-full bg-slate-300"></span><b class="text-slate-700">{t('job.idle')}</b><span>{t('job.noJob')}</span></div>
  {:else}
    <div class="flex flex-wrap items-center gap-3">
      {#if job.status === 'running'}
        <span class="h-2 w-2 animate-pulse rounded-full bg-indigo-500"></span>
      {:else if job.status === 'paused'}
        <Pause size={16} class="text-amber-500" />
      {:else if job.status === 'done' || job.status === 'previewed'}
        <CircleCheck size={16} class={job.errors ? 'text-amber-500' : 'text-emerald-600'} />
      {:else}
        <CircleAlert size={16} class={job.status === 'failed' ? 'text-rose-600' : 'text-amber-500'} />
      {/if}
      <button class="font-semibold text-slate-800 hover:underline" onclick={() => onopen?.(job)} data-testid="jobbar-title">
        {t('kind.' + job.kind)} · {t('stage.' + job.stage)}
      </button>
      <span class="pill {job.status === 'failed' ? 'bg-rose-100 text-rose-700' : job.status === 'paused' || job.status === 'stopped' || job.status === 'interrupted' ? 'bg-amber-100 text-amber-800' : job.status === 'running' ? 'bg-indigo-50 text-indigo-700' : 'bg-emerald-50 text-emerald-700'}" data-testid="jobbar-status">{t('status.' + job.status)}</span>
      <div class="flex flex-wrap items-center gap-1">
        {#each job.phases as ph, i (ph)}
          {@const st = phaseState(i)}
          {#if i > 0}<span class="text-slate-300">›</span>{/if}
          <span class="phase {st === 'done' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : st === 'cur' ? 'border-indigo-300 bg-indigo-50 font-medium text-indigo-700' : 'border-slate-200 text-slate-400'}">
            {#if st === 'done'}<Check size={12} />{:else if st === 'cur' && job.status === 'running'}<LoaderCircle size={12} class="animate-spin" />{:else}<Circle size={12} />{/if}
            {t('phase.' + ph)}
          </span>
        {/each}
      </div>
      <div class="ml-auto flex items-center gap-4 text-xs text-slate-500 tabular-nums">
        {#if live && p}
          <span data-testid="jobbar-count"><b class="text-slate-700">{amount(p.done, p.unit)}</b>{#if p.total > 0}&nbsp;/&nbsp;{amount(p.total, p.unit)}{#if !p.final}+{/if}{/if}</span>
          {#if pct !== null}<span class="font-semibold text-slate-700" data-testid="jobbar-pct">{pct}%</span>{/if}
          {#if p.rate > 0}<span>{rate(p.rate, p.unit)}</span>{/if}
          {#if p.final && p.eta >= 0}<span>{t('job.eta')} <b class="text-slate-700">{duration(p.eta)}</b></span>{/if}
        {:else if job.status === 'failed'}
          <span class="max-w-md truncate text-rose-700" title={job.error}>{job.error}</span>
        {:else if job.status === 'stopped'}
          <span>{t('job.stopped')}</span>
        {:else if job.status === 'interrupted'}
          <span>{t('job.interrupted')}</span>
        {/if}
        <button class="pill {job.errors ? 'bg-rose-100 text-rose-700' : 'bg-slate-100 text-slate-500'}" onclick={openErrors} data-testid="jobbar-errors">{errorsLabel(job.errors)}</button>
        {#if job.status === 'running'}
          <button class="btn-ghost btn-sm" onclick={() => act(job.id, 'pause')} data-testid="job-pause"><Pause size={12} />{t('job.pause')}</button>
          <button class="btn-ghost btn-sm" onclick={stop} data-testid="job-stop"><Square size={12} />{t('job.stop')}</button>
        {:else if job.status === 'paused'}
          <button class="btn-primary btn-sm" onclick={() => act(job.id, 'resume')} data-testid="job-resume"><Play size={12} />{t('job.resume')}</button>
          <button class="btn-ghost btn-sm" onclick={stop} data-testid="job-stop"><Square size={12} />{t('job.stop')}</button>
        {:else if job.status === 'interrupted' || job.status === 'stopped'}
          <button class="btn-primary btn-sm" disabled={!!store.active} onclick={() => act(job.id, 'continue')} data-testid="job-continue"><RotateCcw size={12} />{t('job.continue')}</button>
          <button class="icon-btn" onclick={() => (dismissed = job.id)} title={t('job.dismiss')}><X size={14} /></button>
        {:else}
          <button class="icon-btn" onclick={() => (dismissed = job.id)} title={t('job.dismiss')}><X size={14} /></button>
        {/if}
      </div>
    </div>
    <div class="mt-2 h-2 overflow-hidden rounded-full bg-slate-100">
      {#if live && pct === null}
        <div class="indeterminate h-full w-1/3 rounded-full {job.status === 'paused' ? 'bg-amber-400' : 'bg-indigo-500'}"></div>
      {:else}
        <div class="h-full transition-all {job.status === 'paused' ? 'bg-amber-400' : live ? 'stripes bg-indigo-600' : job.status === 'failed' ? 'bg-rose-500' : job.errors || job.status !== 'done' && job.status !== 'previewed' ? 'bg-amber-500' : 'bg-emerald-500'}" style="width:{live ? pct : 100}%" data-testid="jobbar-bar"></div>
      {/if}
    </div>
  {/if}
</section>
