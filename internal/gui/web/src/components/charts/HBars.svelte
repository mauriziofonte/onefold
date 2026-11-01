<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  // Horizontal bars: rows {key, label, value, text, sub, color}.
  let { rows = [], onpick = null, mono = false } = $props();
  const max = $derived(Math.max(1, ...rows.map((r) => r.value)));
</script>

<div class="flex flex-col gap-1.5">
  {#each rows as r (r.key)}
    <button class="group grid grid-cols-[minmax(0,9rem)_1fr_auto] items-center gap-2 text-left text-xs {onpick ? 'cursor-pointer' : 'cursor-default'}"
      onclick={() => onpick?.(r)} title={r.title || r.label}>
      <span class="truncate text-slate-600 {mono ? 'font-mono' : ''}">{r.label}</span>
      <span class="h-2.5 overflow-hidden rounded-full bg-slate-100">
        <span class="block h-full rounded-full transition-all group-hover:brightness-110" style="width:{Math.max(1.5, (100 * r.value) / max)}%;background:{r.color || '#0f766e'}"></span>
      </span>
      <span class="text-right whitespace-nowrap text-slate-500 tabular-nums">{r.text}{#if r.sub}<span class="ml-1.5 text-slate-400">{r.sub}</span>{/if}</span>
    </button>
  {/each}
</div>
