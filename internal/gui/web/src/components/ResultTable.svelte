<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { List, FolderTree } from '@lucide/svelte';
  import { api } from '../lib/api.js';
  import { t } from '../lib/i18n.svelte.js';
  import { num, bytes } from '../lib/format.js';
  import { settings } from '../lib/settings.svelte.js';
  import { roomOf, relTo, breakable, within } from '../lib/paths.js';
  import ExcludeMenu from './ExcludeMenu.svelte';
  import TreeView from './TreeView.svelte';

  // Plan lists. tabs: [{ key, label, count, cols: [srcLabel, dstLabel],
  // tone, note: (note) => text, exclude: bool (row actions), content: bool
  // ("this content everywhere"), abs: (src) => absolute path }]. rooms: the
  // folders of the run, labels: their badges (sources shown relative to their
  // room). onexclude({ path } | { contentOf }) when rows can exclude.
  // pkgs: package folders: their rows get a tag.
  let { jobId, tabs, version = '', rooms = [], labels = [], onexclude = null, pkgs = [] } = $props();
  const view = settings.list;
  let current = $state('');
  let q = $state('');
  let ext = $state('');
  let offset = $state(0);
  let rows = $state([]);
  let total = $state(0);
  let exts = $state([]);
  let sentinel = $state();
  let loading = false;
  const SIZES = ['50', '100', '250', '500', 'all'];
  const CHUNK = 500;

  const tab = $derived(tabs.find((x) => x.key === current) || tabs[0]);
  const limit = $derived(view.limit === 'all' ? CHUNK : Number(view.limit) || 100);

  $effect(() => {
    if (!tabs.find((x) => x.key === current)) current = tabs[0]?.key || '';
  });

  function url(from, n) {
    const p = new URLSearchParams({ list: tab.key, q, ext, sort: view.sort || '', offset: String(from), limit: String(n) });
    return `/api/jobs/${jobId}/items?${p}`;
  }

  async function load(append = false) {
    if (!jobId || !tab || loading) return;
    loading = true;
    try {
      const r = await api.get(url(append ? rows.length : offset, limit));
      rows = append ? [...rows, ...(r.items || [])] : r.items || [];
      total = r.total || 0;
    } catch {
      if (!append) { rows = []; total = 0; }
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    jobId; tab?.key; q; ext; offset; version; view.limit; view.sort;
    load();
  });

  $effect(() => {
    jobId; tab?.key; version;
    if (!jobId || !tab) return;
    api.get(`/api/jobs/${jobId}/exts?list=${tab.key}`).then((r) => {
      exts = Object.entries(r.exts || {}).sort((a, b) => b[1] - a[1]);
    }).catch(() => { exts = []; });
  });

  // "all": the next chunk when the end of the list comes into view
  $effect(() => {
    if (view.limit !== 'all' || !sentinel) return;
    const io = new IntersectionObserver((es) => {
      if (es.some((e) => e.isIntersecting) && rows.length < total) load(true);
    });
    io.observe(sentinel);
    return () => io.disconnect();
  });

  function setTab(k) { current = k; offset = 0; ext = ''; }
  const abs = (src) => (tab?.abs ? tab.abs(src) : src);
  const where = (src) => roomOf(abs(src), rooms);
  const pkgOf = (src) => pkgs.find((p) => within(abs(src), p.src));
</script>

<div class="card p-0" data-testid="result-table">
  <div class="flex flex-wrap items-center gap-1 border-b border-slate-200 px-3 pt-2">
    {#each tabs as x (x.key)}
      <button class="tab" class:tab-on={x.key === tab?.key} onclick={() => setTab(x.key)}>
        {x.label} <span class="tab-n" class:text-rose-600={x.tone === 'rose' && x.count > 0}>{num(x.count)}</span>
      </button>
    {/each}
  </div>
  <div class="flex flex-wrap items-center gap-2 border-b border-slate-100 px-3 py-2 text-xs">
    <div class="flex overflow-hidden rounded-lg border border-slate-300" role="group" aria-label={t('plan.view')}>
      <button class="flex items-center gap-1 px-2 py-1 {view.view !== 'tree' ? 'bg-slate-800 text-white' : 'bg-white text-slate-600'}" onclick={() => (view.view = 'list')} data-testid="view-list"><List size={12} />{t('plan.viewList')}</button>
      <button class="flex items-center gap-1 px-2 py-1 {view.view === 'tree' ? 'bg-slate-800 text-white' : 'bg-white text-slate-600'}" onclick={() => (view.view = 'tree')} data-testid="view-tree"><FolderTree size={12} />{t('plan.viewTree')}</button>
    </div>
    <select class="input w-auto py-1 text-xs" bind:value={ext} onchange={() => (offset = 0)} data-testid="ext-filter">
      <option value="">{t('plan.allExt')}</option>
      {#each exts as [e, n] (e)}<option value={e}>{e ? '.' + e : t('plan.noExt')} ({num(n)})</option>{/each}
    </select>
    {#if view.view !== 'tree'}
      <select class="input w-auto py-1 text-xs" bind:value={view.sort} onchange={() => (offset = 0)} data-testid="sort">
        <option value="">{t('plan.sortPlan')}</option>
        <option value="src">{t('plan.sortSrc')}</option>
        <option value="dst">{t('plan.sortDst')}</option>
        <option value="-size">{t('plan.sortSizeDesc')}</option>
        <option value="size">{t('plan.sortSize')}</option>
      </select>
      <label class="flex items-center gap-1 text-slate-500">{t('plan.perPage')}
        <select class="input w-auto py-1 text-xs" bind:value={view.limit} onchange={() => (offset = 0)} data-testid="page-size">
          {#each SIZES as n (n)}<option value={n}>{n === 'all' ? t('plan.all') : n}</option>{/each}
        </select>
      </label>
    {/if}
    <input class="input ml-auto w-64 py-1" placeholder={t('common.search')} bind:value={q} oninput={() => (offset = 0)} />
  </div>

  {#if view.view === 'tree'}
    <TreeView {jobId} list={tab?.key} {q} {ext} {version} {rooms} {labels} exclude={!!(tab?.exclude && onexclude)} {onexclude} note={tab?.note} abs={tab?.abs} />
  {:else}
    <table class="tbl">
      <thead class="sticky -top-6 z-10"><tr><th>{tab?.cols?.[0] || t('common.source')}</th><th>{tab?.cols?.[1] || t('common.target')}</th><th class="text-right">{t('common.size')}</th>{#if tab?.exclude && onexclude}<th></th>{/if}</tr></thead>
      <tbody>
        {#each rows as r, i (i + ':' + r.src)}
          {@const w = where(r.src)}
          <tr class:bg-amber-50={r.note === 'renamed'}>
            <td class="mono" class:text-rose-700={tab?.tone === 'rose'} title={abs(r.src)} style="word-break: normal; overflow-wrap: anywhere">
              {#if w.idx >= 0 && labels[w.idx]}<span class="tag-slate mr-1">{labels[w.idx]}</span>{/if}{breakable(w.room ? relTo(abs(r.src), w.room) : r.src)}{#if pkgs.length && pkgOf(r.src)}<span class="tag-amber ml-1 font-sans">{t('packages.' + pkgOf(r.src).note)}</span>{/if}
            </td>
            <td class="mono" style="word-break: normal; overflow-wrap: anywhere">{breakable(r.dst || '')}{#if r.note === 'renamed'} <span class="tag-amber">_NN</span>{:else if r.note && tab?.note}<span class="font-sans text-slate-600">{tab.note(r.note)}</span>{:else if r.note}<span class="text-rose-700"> {r.note}</span>{/if}</td>
            <td class="num">{r.size ? bytes(r.size) : ''}</td>
            {#if tab?.exclude && onexclude}<td class="w-8 text-right"><ExcludeMenu path={abs(r.src)} room={w.room} content={!!tab.content} {onexclude} /></td>{/if}
          </tr>
        {:else}
          <tr><td colspan="4" class="py-4 text-center text-xs text-slate-400">{t('common.empty')}</td></tr>
        {/each}
      </tbody>
    </table>
    {#if view.limit === 'all'}
      <div bind:this={sentinel} class="px-3 py-2 text-xs text-slate-500">{t('common.showing', { a: num(rows.length ? 1 : 0), b: num(rows.length), n: num(total) })}</div>
    {:else}
      <div class="flex items-center gap-2 border-t border-slate-100 px-3 py-2 text-xs text-slate-500">
        {#if total > 0}{t('common.showing', { a: num(offset + 1), b: num(Math.min(offset + limit, total)), n: num(total) })}{/if}
        <div class="ml-auto flex gap-1">
          <button class="btn-ghost btn-sm" disabled={offset === 0} onclick={() => (offset = Math.max(0, offset - limit))}>{t('common.prev')}</button>
          <button class="btn-ghost btn-sm" disabled={offset + limit >= total} onclick={() => (offset += limit)}>{t('common.next')}</button>
        </div>
      </div>
    {/if}
  {/if}
</div>
