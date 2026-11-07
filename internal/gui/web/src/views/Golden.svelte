<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { Scan, ListChecks, Play, ScanSearch, X, RotateCcw, Wand } from '@lucide/svelte';
  import { onMount } from 'svelte';
  import { openWizard } from '../lib/wizard.js';
  import { settings } from '../lib/settings.svelte.js';
  import { store, start, act, flash } from '../lib/store.svelte.js';
  import { api } from '../lib/api.js';
  import { t } from '../lib/i18n.svelte.js';
  import { num, bytes, when } from '../lib/format.js';
  import { confirmDialog } from '../lib/dialogs.svelte.js';
  import PathList from '../components/PathList.svelte';
  import PathField from '../components/PathField.svelte';
  import ResultTable from '../components/ResultTable.svelte';
  import FiltersCard from '../components/FiltersCard.svelte';

  const cfg = settings.golden;
  const busy = $derived(!!store.active);
  const ij = $derived(cfg.indexJobId ? store.jobs[cfg.indexJobId] : null);
  const pj = $derived(cfg.planJobId ? store.jobs[cfg.planJobId] : null);
  const aj = $derived(cfg.applyJobId ? store.jobs[cfg.applyJobId] : null);
  let files = $state([]);
  let hosts = $state([]);
  let typedManifest = $state('');

  async function loadFiles() {
    try { files = (await api.get('/api/golden')).files || []; } catch { files = []; }
  }
  async function loadHosts() {
    if (!cfg.plan) { hosts = []; return; }
    try { hosts = (await api.get('/api/golden?plan=' + encodeURIComponent(cfg.plan))).hosts || []; } catch { hosts = []; }
  }
  $effect(() => { ij?.status; pj?.status; loadFiles(); });
  $effect(() => { cfg.plan; loadHosts(); });

  // a finished index adds its manifest to step 2, a finished plan selects itself in step 3
  $effect(() => {
    const m = ij?.status === 'done' && ij.summary?.manifest;
    if (m && !cfg.manifests.includes(m)) cfg.manifests = [...cfg.manifests, m];
  });
  $effect(() => {
    const p = pj?.status === 'done' && pj.summary?.plan;
    if (p && cfg.plan !== p) cfg.plan = p;
  });

  // owner, signature and language come from the user's config (Config view)
  const conf = $derived(store.config?.config || {});
  const sig = $derived(store.config?.effectiveSignature || '');
  // tools that silver media: missing ones turn photo/video copies into faithful, unstamped ones for good
  const missingTools = $derived(store.info ? ['exiftool', 'ffmpeg'].filter((n) => !store.info.tools?.[n]) : []);
  // the checked target needs tools that are missing and the check did not allow unstamped copies: the server refuses
  const blockedByTools = $derived(!!aj?.summary?.missingTools?.length && !aj.summary.allowUnstamped);
  const manifests = $derived(files.filter((f) => !f.plan));
  const plans = $derived(files.filter((f) => f.plan));

  // the golden scope is fixed (preset media): only skip rules and exclusions vary
  const indexParams = () => ({ rooms: cfg.rooms, host: cfg.host.trim(), filters: { ...cfg.filters } });
  // the guided setup opens this view with its answers: index this computer at once
  onMount(() => {
    if (cfg.autoIndex) {
      cfg.autoIndex = false;
      if (!busy && cfg.rooms.length) run('index', indexParams(), 'indexJobId');
    }
  });

  async function run(kind, params, key) {
    try {
      const j = await start(kind, params);
      cfg[key] = j.id;
    } catch (e) {
      flash(e.message);
    }
  }
  function toggleManifest(p) {
    cfg.manifests = cfg.manifests.includes(p) ? cfg.manifests.filter((x) => x !== p) : [...cfg.manifests, p];
  }
  async function apply() {
    const s = aj.summary;
    if (!(await confirmDialog(t('confirm.applyTitle'), t('confirm.applyBody', { n: num(s.toWrite), size: bytes(s.bytes), target: s.target })))) return;
    act(aj.id, 'execute', {}).catch((e) => flash(e.message));
  }
  function stateTag(j) {
    if (!j) return '';
    return t('status.' + j.status);
  }
</script>

<section class="mx-auto max-w-6xl space-y-5">
  <div class="flex items-start gap-4">
    <div class="flex-1">
      <h1 class="text-xl font-semibold text-slate-900">{t('golden.title')}</h1>
      <p class="text-slate-500">{t('golden.desc')}</p>
    </div>
    <button class="btn-ghost shrink-0" onclick={() => openWizard('golden')} data-testid="open-wizard"><Wand size={14} />{t('wizard.open')}</button>
  </div>
  <div class="grid grid-cols-1 gap-5 xl:grid-cols-3">
    <!-- 1. index -->
    <div class="card space-y-3">
      <div class="flex items-center gap-2"><span class="step step-on">1</span><b>{t('golden.index')}</b>{#if ij}<span class="ml-auto tag-slate">{stateTag(ij)}</span>{/if}</div>
      <span class="lbl">{t('golden.rooms')}</span>
      <PathList bind:items={cfg.rooms} testid="golden-rooms" />
      <div class="grid grid-cols-2 gap-2">
        <label><span class="lbl">{t('golden.host')}</span><input class="input mt-1" bind:value={cfg.host} placeholder={store.info?.hostname || "pc"} /></label>
        <div><span class="lbl">{t('golden.types')}</span><div class="mt-2 flex items-center gap-2"><span class="pill bg-slate-100 text-slate-600" data-testid="golden-scope">{t('golden.scope')}</span><span class="text-[11px] text-slate-400">{t('golden.scopeFixed')}</span></div></div>
      </div>
      <p class="text-xs text-slate-500">{t('golden.scopeNote')}</p>
      <button class="btn-primary w-full justify-center" disabled={busy || !cfg.rooms.length} onclick={() => run('index', indexParams(), 'indexJobId')}><Scan size={14} />{t('golden.runIndex')}</button>
      {#if ij?.summary?.manifest}
        <div class="grid grid-cols-2 gap-2 text-center"><div class="mini"><div class="mini-v">{num(ij.summary.assets)}</div><div class="mini-l">{t('golden.assets')}</div></div><div class="mini"><div class="mini-v">{bytes(ij.summary.bytes)}</div><div class="mini-l">{t('common.size')}</div></div></div>
      {/if}
      <p class="text-xs text-slate-500">{t('golden.otherMachines')}</p>
    </div>

    <!-- 2. plan -->
    <div class="card space-y-3">
      <div class="flex items-center gap-2"><span class="step {cfg.manifests.length ? 'step-on' : ''}">2</span><b>{t('golden.plan')}</b>{#if pj}<span class="ml-auto tag-slate">{stateTag(pj)}</span>{/if}</div>
      <span class="lbl">{t('golden.manifests')}</span>
      <ul class="scroll-thin max-h-40 space-y-1 overflow-auto text-xs">
        {#each [...new Set([...cfg.manifests, ...manifests.map((m) => m.path)])] as p (p)}
          {@const f = manifests.find((m) => m.path === p)}
          <li class="flex items-center gap-2 mini"><input type="checkbox" class="accent-brand-600" checked={cfg.manifests.includes(p)} onchange={() => toggleManifest(p)} aria-label={f ? f.name : p} /><span class="mono flex-1">{f ? f.name : p}</span>{#if f}<span class="text-slate-400">{bytes(f.size)}</span>{/if}</li>
        {/each}
      </ul>
      <form class="flex gap-2" onsubmit={(e) => { e.preventDefault(); if (typedManifest.trim()) toggleManifest(typedManifest.trim()); typedManifest = ''; }}>
        <input class="input font-mono text-xs" bind:value={typedManifest} placeholder={t('golden.addManifest')} />
      </form>
      <label class="chk"><input type="checkbox" bind:checked={cfg.verifyFull} />{t('golden.verifyFull')}</label>
      <label class="chk"><input type="checkbox" bind:checked={cfg.videoTier} />{t('golden.videoTier')}</label>
      <p class="text-xs text-slate-500">{t('golden.otherFiles')}</p>
      <p class="text-xs text-slate-500" data-testid="golden-owner">{t('golden.owner', { v: conf.artist || t('config.unset') })} <button class="link" onclick={() => (settings.view = 'config')}>{t('golden.change')}</button></p>
      <button class="btn-primary w-full justify-center" disabled={busy || !cfg.manifests.length} onclick={() => run('plan', { manifests: cfg.manifests, verifyFull: cfg.verifyFull, videoTier: cfg.videoTier, sidecars: !!cfg.filters.sidecars }, 'planJobId')}><ListChecks size={14} />{t('golden.runPlan')}</button>
      {#if pj?.summary?.plan}
        <div class="grid grid-cols-2 gap-2 text-center"><div class="mini"><div class="mini-v">{num(pj.summary.golden)}</div><div class="mini-l">{t('golden.goldenN')}</div></div><div class="mini"><div class="mini-v">{num(pj.summary.inputFiles - pj.summary.golden)}</div><div class="mini-l">{t('golden.dropped')}</div></div></div>
      {/if}
    </div>

    <!-- 3. apply -->
    <div class="card space-y-3">
      <div class="flex items-center gap-2"><span class="step {cfg.plan ? 'step-on' : ''}">3</span><b>{t('golden.apply')}</b>{#if aj}<span class="ml-auto tag-slate">{stateTag(aj)}</span>{/if}</div>
      <label class="block"><span class="lbl">{t('golden.planFile')}</span>
        <select class="input mt-1" bind:value={cfg.plan}>
          <option value="">{t('golden.pickPlan')}</option>
          {#each plans as p (p.path)}<option value={p.path}>{p.name} · {when(p.modified)}</option>{/each}
        </select>
      </label>
      <span class="lbl">{t('golden.target')}</span>
      <PathField bind:value={cfg.target} />
      <label class="block"><span class="lbl">{t('golden.onlyHost')}</span>
        <select class="input mt-1" bind:value={cfg.onlyHost}>
          <option value="">{t('golden.allHosts')}</option>
          {#each hosts as h (h.host)}<option value={h.host}>{h.host} · {t('golden.files', { n: num(h.files) })} · {bytes(h.bytes)}</option>{/each}
        </select>
      </label>
      <p class="text-xs text-slate-500">{t('golden.silverNote')}</p>
      {#if missingTools.length}
        <div class="rounded-lg border border-amber-200 bg-amber-50 p-2 text-xs text-amber-800" data-testid="golden-missing-tools">
          {t('golden.missingTools', { tools: missingTools.join(', ') })}
          <button class="link" onclick={() => (settings.view = 'doctor')}>{t('nav.doctor')}</button>
          <label class="mt-1 flex items-center gap-2"><input type="checkbox" bind:checked={cfg.allowUnstamped} data-testid="golden-allow-unstamped" />{t('golden.allowUnstamped')}</label>
        </div>
      {/if}
      <p class="text-xs text-slate-500" data-testid="golden-tags">{t('golden.tags', { sig: sig || t('config.unset'), lang: conf.language || t('config.unset') })} <button class="link" onclick={() => (settings.view = 'config')}>{t('golden.change')}</button></p>
      <div class="flex gap-2">
        <button class="btn-ghost flex-1 justify-center" disabled={busy || !cfg.plan || !cfg.target.trim()} onclick={() => run('apply', { plan: cfg.plan, target: cfg.target.trim(), onlyHost: cfg.onlyHost, allowUnstamped: !!cfg.allowUnstamped }, 'applyJobId')}><ScanSearch size={14} />{t('golden.checkTarget')}</button>
        <button class="btn-danger flex-1 justify-center" disabled={busy || aj?.status !== 'previewed' || blockedByTools} onclick={apply}><Play size={14} />{t('golden.apply')}…</button>
      </div>
      {#if aj?.summary?.toWrite !== undefined}
        <div class="grid grid-cols-3 gap-2 text-center">
          <div class="mini"><div class="mini-v">{num(aj.summary.toWrite)}</div><div class="mini-l">{t('golden.toWrite')}</div></div>
          <div class="mini"><div class="mini-v">{num(aj.summary.existing)}</div><div class="mini-l">{t('golden.existing')}</div></div>
          <div class="mini"><div class="mini-v">{num(aj.summary.written ?? 0)}</div><div class="mini-l">{t('golden.written')}</div></div>
        </div>
        {#if blockedByTools && aj.status === 'previewed'}
          <p class="rounded-lg border border-amber-200 bg-amber-50 p-2 text-xs text-amber-800" data-testid="golden-blocked-tools">{t('golden.blockedTools', { tools: aj.summary.missingTools.join(', ') })}</p>
        {/if}
        {#if aj.summary.unsilvered}
          <p class="rounded-lg border border-amber-200 bg-amber-50 p-2 text-xs text-amber-800" data-testid="golden-unsilvered">{t('golden.unsilvered', { n: num(aj.summary.unsilvered) })}</p>
        {/if}
      {/if}
      {#if aj && (aj.status === 'interrupted' || aj.status === 'stopped' || aj.status === 'failed') && aj.stage === 'execute'}
        <button class="btn-primary btn-sm" disabled={busy} onclick={() => act(aj.id, 'continue').catch((e) => flash(e.message))}><RotateCcw size={12} />{t('job.continue')}</button>
      {/if}
    </div>
  </div>
  <FiltersCard f={cfg.filters} golden />
  {#if aj?.summary?.toWrite}
    <ResultTable jobId={aj.id} tabs={[{ key: 'toWrite', label: t('golden.toWrite'), count: aj.summary.toWrite }]} version={aj.updated} />
  {/if}
</section>
