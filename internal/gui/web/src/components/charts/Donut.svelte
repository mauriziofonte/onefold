<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  // A donut chart of slices {key, value, color, label}; a click selects a slice.
  let { slices = [], selected = '', onselect = () => {}, center = '', sub = '', size = 220 } = $props();
  let hover = $state('');

  const total = $derived(slices.reduce((s, x) => s + x.value, 0));
  const arcs = $derived.by(() => {
    const out = [];
    let a = -Math.PI / 2;
    const r = 1, ri = 0.62;
    for (const s of slices) {
      if (!s.value) continue;
      const da = (s.value / total) * Math.PI * 2;
      const gap = slices.length > 1 ? Math.min(0.012, da / 4) : 0;
      const a0 = a + gap, a1 = a + da - gap;
      const large = a1 - a0 > Math.PI ? 1 : 0;
      const p = (rr, t) => `${(rr * Math.cos(t)).toFixed(4)} ${(rr * Math.sin(t)).toFixed(4)}`;
      const d = da >= Math.PI * 2 - 1e-6
        ? `M ${p(r, 0)} A ${r} ${r} 0 1 1 ${p(r, Math.PI)} A ${r} ${r} 0 1 1 ${p(r, 0)} M ${p(ri, 0)} A ${ri} ${ri} 0 1 0 ${p(ri, Math.PI)} A ${ri} ${ri} 0 1 0 ${p(ri, 0)} Z`
        : `M ${p(r, a0)} A ${r} ${r} 0 ${large} 1 ${p(r, a1)} L ${p(ri, a1)} A ${ri} ${ri} 0 ${large} 0 ${p(ri, a0)} Z`;
      out.push({ ...s, d });
      a += da;
    }
    return out;
  });
  const shown = $derived(slices.find((s) => s.key === (hover || selected)));
</script>

<div class="relative" style="width:{size}px;height:{size}px">
  <svg viewBox="-1.08 -1.08 2.16 2.16" width={size} height={size} role="img">
    {#each arcs as s (s.key)}
      <path
        d={s.d}
        fill={s.color}
        fill-rule="evenodd"
        class="cursor-pointer transition-opacity"
        opacity={(hover || selected) && (hover || selected) !== s.key ? 0.35 : 1}
        transform={(hover || selected) === s.key ? 'scale(1.04)' : ''}
        onmouseenter={() => (hover = s.key)}
        onmouseleave={() => (hover = '')}
        onclick={() => onselect(selected === s.key ? '' : s.key)}
        role="button"
        tabindex="0"
        onkeydown={(e) => e.key === 'Enter' && onselect(selected === s.key ? '' : s.key)}
        aria-label={s.label}
      />
    {/each}
  </svg>
  <div class="pointer-events-none absolute inset-0 flex flex-col items-center justify-center text-center">
    {#if shown}
      <div class="text-xs text-slate-500">{shown.label}</div>
      <div class="text-lg font-semibold text-slate-900 tabular-nums">{shown.text}</div>
      <div class="text-xs text-slate-400">{shown.share}</div>
    {:else}
      <div class="text-xs text-slate-500">{sub}</div>
      <div class="text-lg font-semibold text-slate-900 tabular-nums">{center}</div>
    {/if}
  </div>
</div>
