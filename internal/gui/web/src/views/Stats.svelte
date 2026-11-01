<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { ChartPie, HardDrive, Play, ChevronRight, ChevronDown, GitMerge, ScanSearch, TriangleAlert, Search, BadgeCheck, Cloud, Usb, Lock, Copy, Info } from '@lucide/svelte';
  import { onMount } from 'svelte';
  import { settings } from '../lib/settings.svelte.js';
  import { store, start, flash } from '../lib/store.svelte.js';
  import { api } from '../lib/api.js';
  import { t } from '../lib/i18n.svelte.js';
  import { num, bytes, duration } from '../lib/format.js';
  import { famColor, pct } from '../lib/charts.js';
  import PathList from '../components/PathList.svelte';
  import Stat from '../components/Stat.svelte';
  import Donut from '../components/charts/Donut.svelte';
  import Treemap from '../components/charts/Treemap.svelte';
  import YearBars from '../components/charts/YearBars.svelte';
  import HBars from '../components/charts/HBars.svelte';

  const cfg = settings.stats;
  const busy = $derived(!!store.active);
  const job = $derived(cfg.jobId ? store.jobs[cfg.jobId] : null);
  const running = $derived(job?.status === 'running' || job?.status === 'paused');

  let mounts = $state([]);
  let res = $state(null);
  let resId = '';
  let fam = $state(''); // selected family: highlights it in every chart
  let zoom = $state([]); // treemap path: the nodes opened below the top
  let measure = $state('bytes'); // donut by bytes or by files
  let extTab = $state('known');
  let extQ = $state('');
  let openGroup = $state(-1);
  let allGroups = $state(false);
  let allPairs = $state(false);

  onMount(async () => {
    try { mounts = (await api.get('/api/mounts')).mounts || []; } catch { mounts = []; }
  });

  async function load(id) {
    try {
      const r = await api.get(`/api/jobs/${id}/stats`);
      if (!r || id !== cfg.jobId) return;
      if (resId !== id) { zoom = []; fam = ''; openGroup = -1; allGroups = false; allPairs = false; resId = id; }
      res = r;
    } catch { /* no snapshot yet */ }
  }
  // the live snapshot while the job runs, the saved result after
  $effect(() => {
    const id = job?.id;
    const st = job?.status;
    if (!id) { res = null; return; }
    load(id);
    if (st === 'running' || st === 'paused') {
      const iv = setInterval(() => load(id), 1500);
      return () => clearInterval(iv);
    }
  });

  async function analyze(roots = cfg.roots) {
    try {
      const j = await start('stats', { roots, dups: cfg.dups, allVolumes: cfg.allVolumes });
      cfg.jobId = j.id;
    } catch (e) {
      flash(e.message);
    }
  }
  function toggleMount(p) {
    cfg.roots = cfg.roots.includes(p) ? cfg.roots.filter((x) => x !== p) : [...cfg.roots, p];
  }
  function mergeFolders(a, b) {
    settings.merge.inputs = [a, b];
    settings.merge.tab = 'settings';
    settings.view = 'merge';
  }
  function deeper(path) {
    cfg.roots = [path];
    analyze([path]);
  }

  // tree: one node per root; several roots get a common top
  const top = $derived.by(() => {
    const tr = res?.tree || [];
    if (tr.length === 1) return tr[0];
    const fams = {};
    for (const n of tr) for (const [f, b] of Object.entries(n.families || {})) fams[f] = (fams[f] || 0) + b;
    return { name: t('stats.allRoots'), path: '', files: res?.files || 0, bytes: tr.reduce((s, n) => s + n.bytes, 0), families: fams, children: tr };
  });
  const node = $derived(zoom.length ? zoom[zoom.length - 1] : top);
  function openNode(n) {
    if (n.children?.length) zoom = [...zoom, n];
    else if (n.deeper) flash(t('stats.deeperHint'), true);
  }

  const total = $derived(measure === 'bytes' ? res?.bytes || 0 : res?.files || 0);
  const fams = $derived((res?.families || []).filter((b) => b.files > 0));
  const slices = $derived(fams.map((b) => ({
    key: b.key, value: measure === 'bytes' ? b.bytes : b.files, color: famColor(b.key), label: t('fam.' + b.key),
    text: measure === 'bytes' ? bytes(b.bytes) : num(b.files), share: pct(measure === 'bytes' ? b.bytes : b.files, total),
  })));
  const famRow = $derived((res?.families || []).find((b) => b.key === fam));
  const cats = $derived((res?.categories || []).filter((c) => c.family === fam).slice(0, 8));
  const famExts = $derived((res?.exts || []).filter((e) => e.family === fam).slice(0, 8));
  const famFiles = $derived((res?.largestBy?.[fam] || []).slice(0, 8));
  const largest = $derived((fam ? famFiles : res?.largest || []).slice(0, 10));

  const d = $derived(res?.dups);
  const exts = $derived.by(() => {
    const list = extTab === 'known' ? res?.exts || [] : res?.unknown || [];
    const q = extQ.trim().toLowerCase().replace(/^\./, '');
    return list.filter((e) => !q || e.key.includes(q) || (e.category || '').toLowerCase().includes(q)).slice(0, 60);
  });
  const maxExt = $derived(Math.max(1, ...exts.map((e) => e.bytes)));
  const wastedShare = $derived(d && res?.bytes ? pct(d.wasted, res.bytes) : '–');
  const used = (m) => (m.total ? (m.total - m.free) / m.total : 0);
  const extName = (k) => (k ? '.' + k : t('stats.noExt'));
  const base = (p) => p.split(/[\\/]/).filter(Boolean).pop() || p;
  const short = (p) => p.split(/[\\/]/).filter(Boolean).slice(-2).join('/') || p;
  // rel shows a path from the measured folder that holds it (its name included)
  function rel(p) {
    for (const r of res?.roots || []) {
      if (p === r) return base(r);
      const pre = r.endsWith('/') || r.endsWith('\\') ? r : r + (r.includes('\\') ? '\\' : '/');
      if (p.startsWith(pre)) return base(r) + '/' + p.slice(pre.length).replaceAll('\\', '/');
    }
    return p;
  }
  // nested: one folder holds the other (not a merge of two inputs)
  const nested = (a, b) => { const x = a.replaceAll('\\', '/') + '/', y = b.replaceAll('\\', '/') + '/'; return x.startsWith(y) || y.startsWith(x); };
  const catName = (c) => (c ? c.charAt(0).toUpperCase() + c.slice(1).replaceAll('_', ' ') : '');
</script>

<section class="mx-auto max-w-6xl space-y-5" data-testid="stats">
  <div>
    <h1 class="text-xl font-semibold text-slate-900">{t('stats.title')}</h1>
    <p class="text-slate-500">{t('stats.desc')}</p>
  </div>

  <!-- what to measure -->
  <div class="card space-y-4">
    {#if mounts.length}
      <div>
        <span class="lbl">{t('stats.volumes')}</span>
        <div class="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3" data-testid="mounts">
          {#each mounts as m (m.path)}
            {@const on = cfg.roots.includes(m.path)}
            <button class="flex flex-col gap-1.5 rounded-lg border p-2.5 text-left transition-colors {on ? 'border-brand-400 bg-brand-50 ring-1 ring-brand-300' : 'border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50'}" onclick={() => toggleMount(m.path)}>
              <div class="flex items-center gap-2 text-sm">
                {#if m.remote}<Cloud size={15} class="text-slate-400" />{:else if m.removable}<Usb size={15} class="text-slate-400" />{:else}<HardDrive size={15} class="text-slate-400" />{/if}
                <b class="truncate text-slate-800">{m.label || m.path}</b>
                {#if m.readOnly}<Lock size={12} class="text-slate-400" />{/if}
                <span class="ml-auto shrink-0 text-[11px] text-slate-400">{m.fsType}</span>
              </div>
              {#if m.label && m.label !== m.path}<div class="truncate font-mono text-[11px] text-slate-400">{m.path}</div>{/if}
              <div class="h-1.5 overflow-hidden rounded-full bg-slate-100"><div class="h-full rounded-full {used(m) > 0.9 ? 'bg-rose-400' : 'bg-brand-500'}" style="width:{100 * used(m)}%"></div></div>
              <div class="text-[11px] text-slate-500 tabular-nums">{t('stats.free', { free: bytes(m.free), total: bytes(m.total) })}</div>
            </button>
          {/each}
        </div>
      </div>
    {/if}
    <div>
      <span class="lbl">{t('stats.folders')}</span>
      <PathList bind:items={cfg.roots} testid="stats-roots" />
    </div>
    <div class="flex flex-wrap items-end gap-4">
      <div>
        <span class="lbl">{t('stats.dups')}</span>
        <div class="mt-1 flex overflow-hidden rounded-lg border border-slate-300 text-sm" role="group">
          {#each ['off', 'fast', 'full'] as v (v)}
            <button class="px-3 py-1.5 {cfg.dups === v ? 'bg-slate-800 text-white' : 'bg-white text-slate-600 hover:bg-slate-50'}" onclick={() => (cfg.dups = v)} data-testid={'dups-' + v}>{t('stats.dups.' + v)}</button>
          {/each}
        </div>
      </div>
      <label class="flex items-center gap-2 pb-1.5 text-sm text-slate-600"><input type="checkbox" class="chk" bind:checked={cfg.allVolumes} />{t('stats.allVolumes')}</label>
      <button class="btn-primary ml-auto" disabled={busy || !cfg.roots.length} onclick={() => analyze()} data-testid="stats-run"><Play size={14} />{t('stats.run')}</button>
    </div>
    <p class="text-xs text-slate-500">{t('stats.dups.' + cfg.dups + 'Hint')} {t('stats.readOnly')}</p>
  </div>

  {#if res}
    {#if running}
      <div class="banner border-brand-200 bg-brand-50 text-brand-800" data-testid="stats-live">
        <ScanSearch size={16} class="animate-pulse" />{res.phase === 'dups' ? t('stats.liveDups') : t('stats.liveScan')}
      </div>
    {:else if res.stopped}
      <div class="banner border-amber-200 bg-amber-50 text-amber-800"><TriangleAlert size={16} />{t('stats.stopped')}</div>
    {/if}

    <!-- headline numbers -->
    <div class="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-7" data-testid="stats-kpis">
      <Stat label={t('stats.filesK')} value={num(res.files)} sub={t('stats.dirsN', { n: num(res.dirs) })} />
      <Stat label={t('common.size')} value={bytes(res.bytes)} sub={res.allocated ? t('stats.onDisk', { size: bytes(res.allocated) }) : ''} />
      <Stat label={t('stats.wasted')} value={d ? bytes(d.wasted) : '–'} sub={d ? t('stats.wastedSub', { share: wastedShare, n: num(d.extra) }) : t('stats.dups.off')} tone={d?.wasted ? 'text-amber-600' : ''} testid="stats-wasted" />
      <Stat label={t('stats.types')} value={num((res.exts?.length || 0) + (res.unknown?.length || 0))} sub={res.unknown?.length ? t('stats.unknownN', { n: num(res.unknown.length) }) : ''} />
      <Stat label={t('stats.empty')} value={num(res.empty)} sub={res.junk?.files ? t('stats.junkN', { n: num(res.junk.files) }) : ''} />
      <Stat label={t('stats.links')} value={num(res.symlinks + (res.hardLinks?.files || 0))} sub={res.hardLinks?.files ? t('stats.hardN', { n: num(res.hardLinks.files) }) : ''} />
      <Stat label={t('stats.problems')} value={num(res.errors)} tone={res.errors ? 'text-rose-600' : ''} sub={duration(res.elapsed)} />
    </div>

    <!-- families -->
    <div class="card">
      <div class="mb-3 flex items-center gap-2">
        <ChartPie size={16} class="text-slate-400" /><b>{t('stats.byFamily')}</b>
        <div class="ml-auto flex overflow-hidden rounded-lg border border-slate-300 text-xs">
          {#each ['bytes', 'files'] as v (v)}
            <button class="px-2 py-1 {measure === v ? 'bg-slate-800 text-white' : 'bg-white text-slate-600'}" onclick={() => (measure = v)}>{t('stats.by.' + v)}</button>
          {/each}
        </div>
      </div>
      <div class="grid grid-cols-1 items-start gap-6 lg:grid-cols-[auto_1fr]">
        <div class="flex flex-col items-center gap-3">
          <Donut {slices} selected={fam} onselect={(k) => (fam = k)} center={measure === 'bytes' ? bytes(res.bytes) : num(res.files)} sub={t('stats.total')} />
          <div class="flex max-w-60 flex-wrap justify-center gap-x-3 gap-y-1">
            {#each slices as s (s.key)}
              <button class="flex items-center gap-1 text-xs {fam && fam !== s.key ? 'text-slate-400' : 'text-slate-600'}" onclick={() => (fam = fam === s.key ? '' : s.key)}>
                <span class="h-2.5 w-2.5 rounded-sm" style="background:{s.color}"></span>{s.label}
              </button>
            {/each}
          </div>
        </div>
        {#if famRow}
          <div class="space-y-4" data-testid="family-detail">
            <div class="flex items-baseline gap-3">
              <span class="h-3 w-3 rounded-sm" style="background:{famColor(fam)}"></span>
              <b class="text-lg text-slate-900">{t('fam.' + fam)}</b>
              <span class="text-slate-500 tabular-nums">{bytes(famRow.bytes)} · {num(famRow.files)} {t('stats.files')} · {pct(famRow.bytes, res.bytes)}</span>
              <button class="link ml-auto text-xs" onclick={() => (fam = '')}>{t('stats.clear')}</button>
            </div>
            <div class="grid grid-cols-1 gap-5 md:grid-cols-2">
              <div><span class="lbl">{t('stats.categories')}</span>
                <div class="mt-2"><HBars rows={cats.map((c) => ({ key: c.key, label: catName(c.key), value: c.bytes, text: bytes(c.bytes), sub: num(c.files), color: famColor(fam) }))} /></div>
              </div>
              <div><span class="lbl">{t('stats.extensions')}</span>
                <div class="mt-2"><HBars mono rows={famExts.map((e) => ({ key: e.key, label: extName(e.key), value: e.bytes, text: bytes(e.bytes), sub: num(e.files), color: famColor(fam) }))} /></div>
              </div>
            </div>
          </div>
        {:else}
          <div class="space-y-1">
            <HBars rows={fams.map((b) => ({ key: b.key, label: t('fam.' + b.key), value: measure === 'bytes' ? b.bytes : b.files,
              text: measure === 'bytes' ? bytes(b.bytes) : num(b.files), sub: pct(measure === 'bytes' ? b.bytes : b.files, total), color: famColor(b.key) }))}
              onpick={(r) => (fam = r.key)} />
            <p class="pt-2 text-xs text-slate-400">{t('stats.pickFamily')}</p>
          </div>
        {/if}
      </div>
    </div>

    <!-- folders -->
    {#if top}
      <div class="card space-y-3">
        <div class="flex flex-wrap items-center gap-1 text-sm" data-testid="crumbs">
          <button class="font-semibold text-slate-800 hover:underline" onclick={() => (zoom = [])} title={top.path}>{top.path ? base(top.path) : top.name}</button>
          {#each zoom as z, i (z.path)}
            <ChevronRight size={14} class="text-slate-300" />
            <button class="text-slate-700 hover:underline" onclick={() => (zoom = zoom.slice(0, i + 1))}>{z.name}</button>
          {/each}
          <span class="ml-2 text-xs text-slate-400 tabular-nums">{bytes(node.bytes)} · {num(node.files)} {t('stats.files')}</span>
          <div class="ml-auto flex gap-2">
            {#if node.path && !running}
              <button class="btn-ghost btn-sm" disabled={busy} onclick={() => deeper(node.path)} title={t('stats.deeperTip')}><ScanSearch size={12} />{t('stats.deeper')}</button>
            {/if}
          </div>
        </div>
        <Treemap {node} onopen={openNode} highlight={fam} />
        <p class="text-xs text-slate-400">{t('stats.treemapHint')}</p>
      </div>
    {/if}

    <div class="grid grid-cols-1 gap-5 lg:grid-cols-2">
      <div class="card">
        <b>{t('stats.byYear')}</b>
        <p class="mb-3 text-xs text-slate-400">{t('stats.byYearHint')}{fam ? ' · ' + t('fam.' + fam) : ''}</p>
        <YearBars years={res.years || []} highlight={fam} />
      </div>
      <div class="card">
        <b>{t('stats.bySize')}</b>
        <p class="mb-3 text-xs text-slate-400">{t('stats.bySizeHint')}</p>
        <HBars rows={(res.sizes || []).map((s) => ({ key: s.label, label: s.label, value: s.files, text: num(s.files), sub: bytes(s.bytes) }))} />
      </div>
    </div>

    <!-- duplicates -->
    {#if d}
      <div class="card space-y-4" data-testid="stats-dups">
        <div class="flex flex-wrap items-center gap-3">
          <Copy size={16} class="text-slate-400" /><b>{t('stats.dupTitle')}</b>
          <span class="pill {d.mode === 'full' ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-600'}">{t('stats.dups.' + d.mode)}</span>
        </div>
        {#if d.groups}
          <div class="flex flex-wrap items-center gap-6 rounded-lg bg-amber-50 p-4">
            <div><div class="text-3xl font-semibold text-amber-700 tabular-nums">{bytes(d.wasted)}</div><div class="text-sm text-amber-800">{t('stats.dupHeadline', { share: wastedShare })}</div></div>
            <div class="h-12 w-px bg-amber-200"></div>
            <div class="text-sm text-amber-900">{@html t('stats.dupDetail', { extra: num(d.extra), groups: num(d.groups), files: num(d.files) })}</div>
          </div>
          <div class="grid grid-cols-1 gap-5 md:grid-cols-2">
            <div><span class="lbl">{t('stats.dupByFamily')}</span>
              <div class="mt-2"><HBars rows={(d.byFamily || []).map((b) => ({ key: b.key, label: t('fam.' + b.key), value: b.wasted, text: bytes(b.wasted), sub: num(b.files), color: famColor(b.key) }))} onpick={(r) => (fam = r.key)} /></div>
            </div>
            <div><span class="lbl">{t('stats.dupByExt')}</span>
              <div class="mt-2"><HBars mono rows={(d.byExt || []).slice(0, 8).map((b) => ({ key: b.key, label: extName(b.key), value: b.wasted, text: bytes(b.wasted), sub: num(b.files), color: famColor(b.family) }))} /></div>
            </div>
          </div>
          {#if d.pairs?.length}
            <div>
              <span class="lbl">{t('stats.pairs')}</span>
              <p class="text-xs text-slate-400">{t('stats.pairsHint')}</p>
              <div class="mt-2 divide-y divide-slate-100 rounded-lg border border-slate-200" data-testid="pairs">
                {#each allPairs ? d.pairs : d.pairs.slice(0, 6) as p (p.a + p.b)}
                  <div class="flex items-center gap-3 px-3 py-2 text-sm">
                    <div class="min-w-0 flex-1">
                      <div class="truncate font-mono text-xs text-slate-700" title={p.a}>{rel(p.a)}</div>
                      <div class="truncate font-mono text-xs text-slate-700" title={p.b}>{rel(p.b)}</div>
                    </div>
                    <div class="shrink-0 text-right text-xs text-slate-500 tabular-nums"><b class="text-slate-700">{bytes(p.bytes)}</b><br />{num(p.files)} {t('stats.files')}</div>
                    {#if !nested(p.a, p.b)}<button class="btn-ghost btn-sm shrink-0" onclick={() => mergeFolders(p.a, p.b)} title={t('stats.mergeTip')}><GitMerge size={12} />{t('stats.merge')}</button>{/if}
                  </div>
                {/each}
              </div>
              {#if d.pairs.length > 6}<button class="link mt-1 text-xs" onclick={() => (allPairs = !allPairs)}>{allPairs ? t('stats.showLess') : t('stats.showAll', { n: num(d.pairs.length) })}</button>{/if}
            </div>
          {/if}
          <div>
            <span class="lbl">{t('stats.groups')}</span>
            <div class="mt-2 divide-y divide-slate-100 rounded-lg border border-slate-200" data-testid="dup-groups">
              {#each (d.top || []).slice(0, allGroups ? 100 : 8) as g, i (g.hash + g.size)}
                <div>
                  <button class="flex w-full items-center gap-3 px-3 py-2 text-left text-sm hover:bg-slate-50" onclick={() => (openGroup = openGroup === i ? -1 : i)}>
                    {#if openGroup === i}<ChevronDown size={14} class="text-slate-400" />{:else}<ChevronRight size={14} class="text-slate-400" />{/if}
                    <span class="h-2.5 w-2.5 shrink-0 rounded-sm" style="background:{famColor(g.family)}"></span>
                    <span class="min-w-0 flex-1 truncate font-mono text-xs text-slate-700">{short(g.paths[0])}</span>
                    {#if g.verified}<BadgeCheck size={14} class="shrink-0 text-emerald-600" />{/if}
                    <span class="shrink-0 text-xs text-slate-500 tabular-nums">{t('stats.copies', { n: g.count, size: bytes(g.size) })}</span>
                    <b class="w-20 shrink-0 text-right text-xs text-amber-700 tabular-nums">{bytes(g.wasted)}</b>
                  </button>
                  {#if openGroup === i}
                    <div class="space-y-0.5 bg-slate-50 px-10 py-2">
                      {#each g.paths as p (p)}<div class="truncate font-mono text-xs text-slate-600" title={p}>{rel(p)}</div>{/each}
                      {#if g.count > g.paths.length}<div class="text-xs text-slate-400">{t('stats.morePaths', { n: num(g.count - g.paths.length) })}</div>{/if}
                    </div>
                  {/if}
                </div>
              {/each}
            </div>
            {#if (d.top || []).length > 8}<button class="link mt-1 text-xs" onclick={() => (allGroups = !allGroups)}>{allGroups ? t('stats.showLess') : t('stats.showAll', { n: num(Math.min(100, d.top.length)) })}</button>{/if}
          </div>
          {#if d.mode === 'fast'}<p class="flex items-center gap-1.5 text-xs text-slate-500"><Info size={12} />{t('stats.fastNote')}</p>{/if}
        {:else if res.phase !== 'dups'}
          <p class="text-sm text-emerald-700">{t('stats.noDups')}</p>
        {/if}
      </div>
    {/if}

    <div class="grid grid-cols-1 gap-5 lg:grid-cols-2">
      <!-- largest files -->
      <div class="card">
        <b>{t('stats.largest')}{fam ? ' · ' + t('fam.' + fam) : ''}</b>
        <div class="mt-3 space-y-1.5" data-testid="largest">
          {#each largest as f (f.path)}
            <div class="flex items-center gap-2 text-xs" title={f.path}>
              <span class="h-2.5 w-2.5 shrink-0 rounded-sm" style="background:{famColor(f.family)}"></span>
              <span class="min-w-0 flex-1 truncate font-mono text-slate-700">{rel(f.path)}</span>
              <span class="shrink-0 text-slate-500 tabular-nums">{bytes(f.size)}</span>
            </div>
          {/each}
        </div>
      </div>
      <!-- extensions -->
      <div class="card">
        <div class="flex items-center gap-2">
          <b>{t('stats.extensions')}</b>
          <div class="ml-auto flex overflow-hidden rounded-lg border border-slate-300 text-xs">
            {#each ['known', 'unknown'] as v (v)}
              <button class="px-2 py-1 {extTab === v ? 'bg-slate-800 text-white' : 'bg-white text-slate-600'}" onclick={() => (extTab = v)}>{t('stats.ext.' + v)} ({num(v === 'known' ? res.exts?.length || 0 : res.unknown?.length || 0)})</button>
            {/each}
          </div>
        </div>
        <label class="relative mt-3 block"><Search size={14} class="absolute top-2.5 left-2.5 text-slate-400" /><input class="input pl-8" bind:value={extQ} placeholder={t('stats.extSearch')} /></label>
        <div class="scroll-thin mt-2 max-h-72 overflow-auto" data-testid="exts">
          <table class="w-full text-xs">
            <tbody>
              {#each exts as e (e.key)}
                <tr class="border-b border-slate-100">
                  <td class="py-1 pr-2 font-mono text-slate-700">{extName(e.key)}</td>
                  <td class="py-1 pr-2 text-slate-400">{catName(e.category)}</td>
                  <td class="w-24 py-1 pr-2"><div class="h-1.5 overflow-hidden rounded-full bg-slate-100"><div class="h-full rounded-full" style="width:{(100 * e.bytes) / maxExt}%;background:{famColor(e.family)}"></div></div></td>
                  <td class="py-1 pr-2 text-right text-slate-500 tabular-nums">{num(e.files)}</td>
                  <td class="py-1 text-right text-slate-600 tabular-nums">{bytes(e.bytes)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
          {#if extTab === 'unknown' && res.unknown?.length}<p class="mt-2 text-xs text-slate-400">{t('stats.unknownHint')}</p>{/if}
        </div>
      </div>
    </div>

    {#if res.problems?.length || res.otherVolumes?.length}
      <div class="card space-y-2">
        <b>{t('stats.notes')}</b>
        {#each res.otherVolumes || [] as v (v)}
          <div class="flex items-center gap-2 text-xs text-slate-600"><HardDrive size={12} class="text-slate-400" />{t('stats.otherVolume', { path: v })}</div>
        {/each}
        {#each res.problems || [] as p (p.path + p.err)}
          <div class="flex items-start gap-2 text-xs"><TriangleAlert size={12} class="mt-0.5 shrink-0 text-rose-500" /><span class="font-mono break-all text-slate-700">{p.path}</span><span class="text-rose-700">{p.err}</span></div>
        {/each}
        {#if res.errors > (res.problems?.length || 0)}<p class="text-xs text-slate-400">{t('stats.moreProblems', { n: num(res.errors - (res.problems?.length || 0)) })}</p>{/if}
      </div>
    {/if}
  {:else if !job}
    <div class="card flex flex-col items-center gap-2 py-10 text-center text-slate-500">
      <ChartPie size={32} class="text-slate-300" />
      <p>{t('stats.intro')}</p>
    </div>
  {/if}
</section>
