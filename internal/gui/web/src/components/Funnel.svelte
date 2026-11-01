<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { t } from '../lib/i18n.svelte.js';
  import { num } from '../lib/format.js';

  // The numbers of a plan that add up: input files and where each
  // goes; the exclusions by reason. kind: merge | rearrange.
  let { s, kind = 'merge' } = $props();
  const by = $derived(Object.entries(s.excludedBy || {}).sort((a, b) => b[1] - a[1]));
  const total = $derived(kind === 'merge'
    ? (s.toCopy || 0) + (s.alreadyPresent || 0) + (s.inputDupes || 0) + (s.blocked || 0)
    : (s.stay || 0) + (s.moves || 0) + (s.deletes || 0));
</script>

<div class="space-y-1 text-sm text-slate-700" data-testid="funnel">
  {#if kind === 'merge'}
    <p>{@html t('funnel.merge', { n: num(total), copy: num(s.toCopy || 0), present: num(s.alreadyPresent || 0), dupes: num(s.inputDupes || 0) })}{#if s.blocked}{@html t('funnel.blocked', { n: num(s.blocked) })}{/if}</p>
  {:else}
    <p>{@html t('funnel.rearrange', { n: num(total), stay: num(s.stay || 0), moves: num(s.moves || 0), deletes: num(s.deletes || 0) })}</p>
  {/if}
  {#if s.excluded}
    <p class="text-xs text-slate-500">{t('funnel.excluded', { n: num(s.excluded) })}
      {#each by as [r, n], i (r)}{i ? ' · ' : ''}{t('crule.' + r)} {num(n)}{/each}
    </p>
  {/if}
</div>
