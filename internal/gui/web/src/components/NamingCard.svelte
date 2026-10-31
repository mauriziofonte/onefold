<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { TextCursorInput, CircleAlert } from '@lucide/svelte';
  import { api } from '../lib/api.js';
  import { t } from '../lib/i18n.svelte.js';
  import { FAMILIES, GENERIC, STEMS, MAX_LEVEL, namingParams, idleSelectors, isGoldenTree } from '../lib/regime.js';

  // cfg: { fams, choice, stem, level, free, template } (persisted); dirs:
  // folders to sample. File types and names of merge/rearrange: per
  // family a preset valid for it and what to do with the file name; the server
  // composes the naming (naming.Compose) and renders the samples.
  let { cfg, dirs = [] } = $props();
  let samples = $state([]);
  let error = $state('');
  let composed = $state({ naming: '', catalogPreset: '' });
  let showRef = $state(false);
  let placeholders = $state([]);
  let timer;

  const effective = $derived(namingParams(cfg));
  const idle = $derived(cfg.free ? idleSelectors(cfg.template, cfg.fams) : []);
  const anyFam = $derived(FAMILIES.some((f) => cfg.fams[f.key]));
  const levels = Array.from({ length: MAX_LEVEL }, (_, i) => i + 1);

  // a free template starts from the composed naming
  $effect(() => {
    if (cfg.free && !cfg.template && composed.naming) cfg.template = composed.naming;
  });

  $effect(() => {
    const body = { ...effective, dirs: dirs.filter(Boolean), max: 6 };
    clearTimeout(timer);
    timer = setTimeout(async () => {
      if (isGoldenTree(body.template)) { samples = []; error = t('regime.goldenTree'); return; }
      try {
        const r = await api.post('/api/naming/preview', body);
        samples = r.samples || [];
        error = r.error || '';
        if (!r.error && !body.template) composed = { naming: r.naming, catalogPreset: r.catalogPreset };
      } catch (e) {
        error = e.message;
      }
    }, 350);
  });

  async function reference() {
    showRef = !showRef;
    if (showRef && !placeholders.length) placeholders = (await api.get('/api/naming')).placeholders || [];
  }
</script>

<div class="card">
  <div class="card-h"><TextCursorInput size={16} />{t('naming.title')} <span class="hint">{t('naming.hint')}</span></div>
  <div class="space-y-1" data-testid="families">
    <div class="grid grid-cols-[11rem_1fr_13rem_6rem] gap-3 pb-1 text-xs text-slate-400">
      <span></span><span>{t('naming.layoutCol')}</span><span>{t('naming.stemCol')}</span><span>{t('naming.levelCol')}</span>
    </div>
    {#each FAMILIES as f (f.key)}
      <div class="grid grid-cols-[11rem_1fr_13rem_6rem] items-center gap-3 border-t border-slate-100 py-1.5 {cfg.fams[f.key] ? '' : 'opacity-60'}">
        <label class="chk"><input type="checkbox" bind:checked={cfg.fams[f.key]} data-testid={'fam-' + f.key} /><b>{t('fam.' + f.key)}</b></label>
        {#if f.key === 'rest' && !cfg.fams.rest}
          <span class="col-span-3 text-xs text-slate-400">{t('naming.restOff')}</span>
        {:else}
          <select class="input" bind:value={cfg.choice[f.key]} disabled={!cfg.fams[f.key] || cfg.free} data-testid={'choice-' + f.key}>
            {#if f.specific.length}
              <optgroup label={t('naming.forFam', { fam: t('fam.' + f.key).toLowerCase() })}>
                {#each f.specific as p (p)}<option value={p}>{t('preset.' + p)} ({p})</option>{/each}
              </optgroup>
            {/if}
            <optgroup label={t('naming.generic')}>
              {#each GENERIC as p (p)}<option value={p}>{t('preset.' + p)} ({p})</option>{/each}
            </optgroup>
          </select>
          <select class="input" bind:value={cfg.stem[f.key]} disabled={!cfg.fams[f.key] || cfg.free} data-testid={'stem-' + f.key} title={t('naming.stem.' + (cfg.stem[f.key] || 'norm') + '.ex')}>
            {#each STEMS as m (m)}<option value={m}>{t('naming.stem.' + m)}</option>{/each}
          </select>
          {#if cfg.stem[f.key] === 'prefix'}
            <select class="input" bind:value={cfg.level[f.key]} disabled={!cfg.fams[f.key] || cfg.free} data-testid={'level-' + f.key} title={t('naming.levelHint')}>
              {#each levels as n (n)}<option value={n}>{t('naming.level', { n })}</option>{/each}
            </select>
          {:else}
            <span></span>
          {/if}
        {/if}
      </div>
    {/each}
    {#if !anyFam}<div class="banner border-amber-200 bg-amber-50 text-xs text-amber-800"><CircleAlert size={14} />{t('naming.noFamily')}</div>{/if}
    <p class="pt-1 text-xs text-slate-500">{t('naming.prefixNote')}</p>
  </div>
  <div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
    <div class="space-y-2 text-xs">
      <div class="grid grid-cols-[11rem_1fr] gap-3">
        <span class="lbl">{t('naming.catalogPreset')}</span><span class="mono" data-testid="catalog-preset">{composed.catalogPreset || '-'}</span>
      </div>
      <p class="text-slate-500">{t('regime.formNote')}</p>
    </div>
    <div class="space-y-3">
      <label class="chk"><input type="checkbox" bind:checked={cfg.free} data-testid="naming-free" />{t('naming.free')}</label>
      <label class="block"><span class="lbl">{cfg.free ? t('naming.template') : t('naming.resulting')}</span>
        {#if cfg.free}
          <textarea class="input mt-1 font-mono text-xs" rows="4" bind:value={cfg.template} data-testid="naming-template"></textarea>
        {:else}
          <div class="input mt-1 bg-slate-50 font-mono text-xs break-all" data-testid="naming-spec">{composed.naming}</div>
        {/if}
      </label>
      {#if idle.length}<div class="banner border-amber-200 bg-amber-50 text-xs text-amber-800" data-testid="naming-idle"><CircleAlert size={14} />{t('regime.idle', { sel: idle.join(', ') })}</div>{/if}
      <button class="link text-xs" onclick={reference}>{t('naming.reference')}</button>
      {#if showRef}
        <ul class="scroll-thin max-h-48 space-y-0.5 overflow-auto rounded-lg border border-slate-200 p-2 text-[11px]">
          {#each placeholders as p (p[0])}<li><span class="font-mono text-indigo-700">{'{' + p[0] + '}'}</span> {p[1]}</li>{/each}
        </ul>
      {/if}
    </div>
  </div>
  <div class="mt-4">
    <span class="lbl">{t('naming.preview')}</span>
    {#if error}<div class="banner mt-2 border-rose-200 bg-rose-50 text-xs text-rose-700">{error}</div>{/if}
    <table class="tbl mt-2">
      <thead><tr><th>{t('common.source')}</th><th></th><th>{t('common.target')}</th></tr></thead>
      <tbody>
        {#each samples as s (s.src)}
          <tr><td class="mono text-slate-500">{s.src}</td><td class="text-slate-300">→</td><td class="mono text-slate-800">{s.dst}</td></tr>
        {:else}
          <tr><td colspan="3" class="py-3 text-xs text-slate-400">{t('naming.previewEmpty')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
</div>
