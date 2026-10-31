<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { X, Plus } from '@lucide/svelte';
  import { t } from '../lib/i18n.svelte.js';

  // The user's exclusions: exact files and folders, and patterns on the
  // folder-relative path. onchange(paths, globs) applies a change (the form,
  // or a re-plan of a previewed job).
  let { paths = [], globs = [], onchange } = $props();
  let pattern = $state('');

  function addGlob() {
    const g = pattern.trim();
    if (!g || globs.includes(g)) return;
    pattern = '';
    onchange(paths, [...globs, g]);
  }
</script>

<div class="space-y-2" data-testid="exclusions">
  <span class="lbl">{t('filters.exclusions')}</span>
  <div class="flex flex-wrap gap-1">
    {#each paths as p (p)}
      <span class="tag-rose max-w-full" title={p}><span class="truncate font-mono">{p}</span>
        <button class="shrink-0" title={t('filters.removeExclusion')} onclick={() => onchange(paths.filter((x) => x !== p), globs)} data-testid="exclusion-remove"><X size={11} /></button></span>
    {/each}
    {#each globs as g (g)}
      <span class="tag-indigo" title={t('filters.pattern')}><span class="font-mono">{g}</span>
        <button title={t('filters.removeExclusion')} onclick={() => onchange(paths, globs.filter((x) => x !== g))} data-testid="exclusion-remove"><X size={11} /></button></span>
    {/each}
    {#if !paths.length && !globs.length}<span class="text-xs text-slate-400">{t('filters.noExclusions')}</span>{/if}
  </div>
  <div class="flex gap-2">
    <input class="input font-mono text-xs" bind:value={pattern} placeholder="*.data, **/DRIVERS/**" onkeydown={(e) => e.key === 'Enter' && addGlob()} data-testid="glob-input" />
    <button class="btn-ghost btn-sm shrink-0" disabled={!pattern.trim()} onclick={addGlob} data-testid="glob-add"><Plus size={12} />{t('filters.addPattern')}</button>
  </div>
  <p class="text-xs text-slate-400">{t('filters.patternHint')}</p>
</div>
