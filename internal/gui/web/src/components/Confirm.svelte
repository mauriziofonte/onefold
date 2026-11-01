<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { TriangleAlert, Play } from '@lucide/svelte';
  import { t } from '../lib/i18n.svelte.js';
  import { dialogs } from '../lib/dialogs.svelte.js';

  let reviewed = $state(false);
  let typed = $state('');
  const d = $derived(dialogs.confirm);
  const ok = $derived((d?.simple || reviewed) && (!d?.count || typed.trim() === String(d.count)));

  $effect(() => {
    if (d) { reviewed = false; typed = ''; }
  });

  function close(v) {
    const r = d?.resolve;
    dialogs.confirm = null;
    r?.(v);
  }
</script>

{#if d}
  <div class="fixed inset-0 z-50 grid place-items-center bg-slate-900/30" role="dialog" aria-modal="true">
    <div class="w-[540px] space-y-4 rounded-xl bg-white p-5 shadow-xl">
      <div class="flex items-center gap-2 text-lg font-semibold text-slate-900"><TriangleAlert size={18} class="text-amber-500" />{d.title}</div>
      <div class="space-y-2 text-slate-600">{@html d.body}</div>
      {#if d.count}
        <label class="block"><span class="lbl">{t('confirm.typeDeletes')}</span><input class="input mt-1" bind:value={typed} data-testid="confirm-count" /></label>
      {/if}
      {#if !d.simple}
        <label class="chk"><input type="checkbox" bind:checked={reviewed} data-testid="confirm-reviewed" />{t('confirm.reviewed')}</label>
      {/if}
      <div class="flex justify-end gap-2">
        <button class="btn-ghost" onclick={() => close(false)}>{t('common.cancel')}</button>
        <button class="btn-danger" disabled={!ok} onclick={() => close(true)} data-testid="confirm-go"><Play size={14} />{d.go || t('confirm.go')}</button>
      </div>
    </div>
  </div>
{/if}
