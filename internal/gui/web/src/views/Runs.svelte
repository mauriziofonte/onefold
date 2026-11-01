<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { RotateCcw, Trash2, ScrollText, Eye } from '@lucide/svelte';
  import { store, act, removeJob, jobsSorted, flash } from '../lib/store.svelte.js';
  import { api } from '../lib/api.js';
  import { t } from '../lib/i18n.svelte.js';
  import { num, bytes, when } from '../lib/format.js';
  import { settings } from '../lib/settings.svelte.js';

  let { onopen } = $props();
  const jobs = $derived(jobsSorted());

  function summary(j) {
    const s = j.summary || {};
    switch (j.kind) {
      case 'merge':
        return s.copied !== undefined ? `${num(s.copied)} ${t('merge.copied').toLowerCase()} · ${bytes(s.copiedBytes)}` : s.toCopy !== undefined ? `${num(s.toCopy)} ${t('merge.toCopy').toLowerCase()} · ${bytes(s.copyBytes)}` : '';
      case 'rearrange':
        return s.moved !== undefined ? `${num(s.moved)} ${t('rearrange.moved').toLowerCase()} · ${num(s.deleted)} ${t('rearrange.deleted').toLowerCase()}` : s.moves !== undefined ? `${num(s.moves)} ${t('rearrange.moves').toLowerCase()} · ${num(s.deletes)} ${t('rearrange.deletes').toLowerCase()}` : '';
      case 'recover':
        return `${num(s.moved)} ${t('rearrange.moved').toLowerCase()} · ${num(s.deleted)} ${t('rearrange.deleted').toLowerCase()}`;
      case 'index':
        return s.assets !== undefined ? `${num(s.assets)} ${t('golden.assets').toLowerCase()}` : '';
      case 'plan':
        return s.golden !== undefined ? `${num(s.golden)} ${t('golden.goldenN')}` : '';
      case 'stats':
        return s.files !== undefined ? `${num(s.files)} ${t('stats.files')} · ${bytes(s.bytes)}` + (s.wasted ? ` · ${bytes(s.wasted)} ${t('stats.wasted').toLowerCase()}` : '') : '';
      case 'apply':
        return s.written !== undefined ? `${num(s.written)} ${t('golden.written').toLowerCase()}` : s.toWrite !== undefined ? `${num(s.toWrite)} ${t('golden.toWrite').toLowerCase()}` : '';
    }
    return '';
  }
  async function showLog(j) {
    const r = await api.get(`/api/log?job=${j.id}&limit=5000`);
    store.jobLog = { id: j.id.slice(0, 15), entries: r.entries || [] };
    settings.drawerOpen = true;
  }
  const tone = { failed: 'tag-rose', interrupted: 'tag-amber', stopped: 'tag-amber', paused: 'tag-amber', running: 'tag-brand', done: 'tag-emerald', previewed: 'tag-emerald' };
</script>

<section class="mx-auto max-w-6xl space-y-5">
  <div>
    <h1 class="text-xl font-semibold text-slate-900">{t('runs.title')}</h1>
    <p class="text-slate-500">{t('runs.desc')}</p>
  </div>
  <div class="card p-0">
    <table class="tbl" data-testid="runs-table">
      <thead><tr><th>{t('runs.when')}</th><th>{t('runs.kind')}</th><th>{t('runs.status')}</th><th>{t('runs.summary')}</th><th></th></tr></thead>
      <tbody>
        {#each jobs as j (j.id)}
          <tr class:bg-amber-50={j.status === 'interrupted' || j.status === 'stopped'}>
            <td class="text-xs whitespace-nowrap">{when(j.created)}</td>
            <td>{t('kind.' + j.kind)} {#if j.kind !== 'stats'}<span class="text-xs text-slate-400">· {t('stage.' + j.stage)}</span>{/if}</td>
            <td><span class={tone[j.status] || 'tag-slate'}>{t('status.' + j.status)}</span>{#if j.errors}<span class="ml-1 tag-rose">{num(j.errors)} ✕</span>{/if}</td>
            <td class="text-xs text-slate-600">{summary(j)}{#if j.status === 'failed'}<div class="text-rose-700">{j.error}</div>{/if}</td>
            <td class="text-right whitespace-nowrap">
              {#if j.status === 'interrupted' || j.status === 'stopped' || (j.status === 'failed' && j.stage === 'execute')}
                <button class="btn-primary btn-sm" disabled={!!store.active} onclick={() => act(j.id, 'continue').catch((e) => flash(e.message))}><RotateCcw size={12} />{t('job.continue')}</button>
              {/if}
              <button class="icon-btn" title={t('runs.show')} onclick={() => onopen?.(j)}><Eye size={14} /></button>
              <button class="icon-btn" title={t('runs.log')} onclick={() => showLog(j)}><ScrollText size={14} /></button>
              <button class="icon-btn" title={t('common.delete')} disabled={j.id === store.active} onclick={() => removeJob(j.id).catch((e) => flash(e.message))}><Trash2 size={14} /></button>
            </td>
          </tr>
        {:else}
          <tr><td colspan="5" class="py-4 text-center text-xs text-slate-400">{t('runs.empty')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
</section>
