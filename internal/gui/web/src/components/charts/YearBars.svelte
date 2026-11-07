<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  // Bytes per year of last modification, stacked by family.
  import { FAMILIES, famColor } from '../../lib/charts.js';
  import { bytes, num } from '../../lib/format.js';
  import { t } from '../../lib/i18n.svelte.js';
  import ChartTable from './ChartTable.svelte';

  let { years = [], highlight = '', height = 200 } = $props();
  let hover = $state(null);
  const val = (y) => (highlight ? y.families?.[highlight] || 0 : y.bytes);
  const max = $derived(Math.max(1, ...years.map(val)));
  // a label every few years: at most about eight
  const step = $derived(Math.max(1, Math.ceil(years.length / 8)));
</script>

<div class="relative">
  <div class="flex items-end gap-1" style="height:{height}px" data-testid="years">
    {#each years as y (y.year)}
      <div class="group flex h-full min-w-0 flex-1 flex-col justify-end" role="img" aria-label={`${y.year || t('stats.yearUnknown')}: ${bytes(val(y))}`}
        onmouseenter={() => (hover = y)} onmouseleave={() => (hover = null)}>
        <div class="flex flex-col-reverse overflow-hidden rounded-t transition-opacity group-hover:opacity-80" style="height:{(100 * val(y)) / max}%">
          {#each FAMILIES.filter((f) => (!highlight || f === highlight) && y.families?.[f]) as f (f)}
            <div style="flex:{y.families[f]} 0 0;background:{famColor(f)}"></div>
          {/each}
        </div>
      </div>
    {/each}
  </div>
  <div class="mt-1 flex gap-1 text-[10px] text-slate-400">
    {#each years as y, i (y.year)}
      <div class="min-w-0 flex-1 overflow-visible text-center whitespace-nowrap">{i % step === 0 ? (y.year || '?') : ''}</div>
    {/each}
  </div>
  {#if hover}
    <div class="pointer-events-none absolute top-0 right-0 rounded-lg border border-slate-200 bg-white p-2 text-xs shadow">
      <div class="font-medium text-slate-800">{hover.year || t('stats.yearUnknown')}</div>
      <div class="text-slate-500">{bytes(val(hover))} · {num(hover.files)} {t('stats.files')}</div>
    </div>
  {/if}
</div>
<ChartTable head={highlight ? [t('chart.col.year'), t('chart.col.size') + ' · ' + t('fam.' + highlight)] : [t('chart.col.year'), t('chart.col.size'), t('chart.col.files')]}
  rows={years.filter((y) => val(y) > 0).map((y) => highlight ? [y.year || t('stats.yearUnknown'), bytes(val(y))] : [y.year || t('stats.yearUnknown'), bytes(y.bytes), num(y.files)])} testid="years-table" />
