<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  // A treemap of the subfolders of a stats node; a click on a folder zooms in.
  import { squarify, famColor, dominant, pct } from '../../lib/charts.js';
  import { bytes, num } from '../../lib/format.js';
  import { t } from '../../lib/i18n.svelte.js';

  let { node, onopen = () => {}, height = 360, highlight = '' } = $props();
  let w = $state(800);
  let tip = $state(null);

  const items = $derived.by(() => {
    if (!node) return [];
    const kids = (node.children || []).map((c) => ({ key: c.path, value: c.bytes, node: c, label: c.name }));
    const sum = kids.reduce((s, k) => s + k.value, 0) + (node.moreBytes || 0);
    const here = node.bytes - sum;
    if (node.moreBytes > 0) kids.push({ key: '__more', value: node.moreBytes, label: t('stats.moreFolders', { n: num(node.more) }), more: true });
    if (here > 0) kids.push({ key: '__here', value: here, label: t('stats.filesHere'), here: true });
    return squarify(kids, 0, 0, w, height);
  });

  function color(it) {
    if (it.more || it.here) return '#cbd5e1';
    return famColor(dominant(it.node.families));
  }
  function dim(it) {
    return highlight && it.node && !(it.node.families?.[highlight] > it.node.bytes * 0.25);
  }
  function move(e, it) {
    const r = e.currentTarget.parentElement.getBoundingClientRect();
    tip = { x: e.clientX - r.left, y: e.clientY - r.top, it };
  }
</script>

<div class="relative overflow-hidden rounded-lg bg-slate-100" style="height:{height}px" bind:clientWidth={w} data-testid="treemap">
  {#each items as it (it.key)}
    <button
      class="absolute overflow-hidden border border-white/80 p-1.5 text-left text-white transition-[filter,opacity] hover:brightness-110 {it.node && (it.node.children?.length || 0) > 0 ? 'cursor-zoom-in' : 'cursor-default'}"
      style="left:{it.x}px;top:{it.y}px;width:{it.w}px;height:{it.h}px;background:{color(it)};opacity:{dim(it) ? 0.3 : 1}"
      onclick={() => it.node && onopen(it.node)}
      onmousemove={(e) => move(e, it)}
      onmouseleave={() => (tip = null)}
    >
      {#if it.w > 70 && it.h > 34}
        <div class="truncate text-xs font-medium {it.more || it.here ? 'text-slate-600' : ''}" style="text-shadow:0 1px 2px rgba(0,0,0,.25)">{it.label}</div>
        <div class="text-[11px] opacity-90 {it.more || it.here ? 'text-slate-500' : ''}">{bytes(it.value)}</div>
      {/if}
    </button>
  {/each}
  {#if tip}
    <div class="pointer-events-none absolute z-10 max-w-xs rounded-lg border border-slate-200 bg-white p-2 text-xs shadow-lg"
      style="left:{Math.min(tip.x + 12, w - 240)}px;top:{Math.min(tip.y + 12, height - 90)}px">
      <div class="font-medium text-slate-800 break-all">{tip.it.node ? tip.it.node.path : tip.it.label}</div>
      <div class="text-slate-500">{bytes(tip.it.value)} · {pct(tip.it.value, node.bytes)}{#if tip.it.node} · {num(tip.it.node.files)} {t('stats.files')}{/if}</div>
      {#if tip.it.node}
        <div class="mt-1 flex h-1.5 overflow-hidden rounded-full">
          {#each Object.entries(tip.it.node.families || {}).sort((a, b) => b[1] - a[1]) as [f, b] (f)}
            <div style="width:{(100 * b) / tip.it.node.bytes}%;background:{famColor(f)}"></div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>
