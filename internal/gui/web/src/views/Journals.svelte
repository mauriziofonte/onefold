<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { Info, RefreshCw } from '@lucide/svelte';
  import { store, start, flash } from '../lib/store.svelte.js';
  import { api } from '../lib/api.js';
  import { t } from '../lib/i18n.svelte.js';
  import { num, when } from '../lib/format.js';
  import { settings } from '../lib/settings.svelte.js';
  import { confirmDialog } from '../lib/dialogs.svelte.js';
  import PathField from '../components/PathField.svelte';

  const cfg = settings.journals;
  let list = $state([]);
  const busy = $derived(!!store.active);

  async function load() {
    try {
      list = (await api.get('/api/journals?root=' + encodeURIComponent(cfg.root.trim()))).journals || [];
    } catch (e) {
      flash(e.message);
    }
  }
  $effect(() => { cfg.root; store.active; load(); });

  async function recover(j, undo) {
    const title = undo ? t('journals.undo') : t('journals.resume');
    if (!(await confirmDialog(title, undo ? t('journals.undoConfirm') : t('journals.resumeConfirm'), 0, { simple: true, go: title }))) return;
    try { await start('recover', { journal: j.path, undo }); } catch (e) { flash(e.message); }
  }
</script>

<section class="mx-auto max-w-6xl space-y-5">
  <div>
    <h1 class="text-xl font-semibold text-slate-900">{t('journals.title')}</h1>
    <p class="text-slate-500">{t('journals.desc')}</p>
  </div>
  <div class="card space-y-2">
    <span class="lbl">{t('journals.scan')}</span>
    <div class="flex gap-2"><div class="flex-1"><PathField bind:value={cfg.root} /></div><button class="btn-ghost" onclick={load}><RefreshCw size={14} />{t('common.refresh')}</button></div>
  </div>
  <div class="card p-0">
    <table class="tbl" data-testid="journals-table">
      <thead><tr><th>{t('journals.when')}</th><th>{t('journals.folder')}</th><th>{t('journals.layout')}</th><th class="text-right">{t('journals.moves')}</th><th class="text-right">{t('journals.deletes')}</th><th>{t('journals.state')}</th><th></th></tr></thead>
      <tbody>
        {#each list as j (j.path)}
          <tr class:bg-amber-50={j.state === 'interrupted'}>
            <td class="text-xs whitespace-nowrap">{when(j.time)}</td>
            <td class="mono">{j.root}</td>
            <td class="mono max-w-56 truncate" title={j.naming}>{j.naming}</td>
            <td class="num">{num(j.moves)}</td>
            <td class="num">{num(j.deletes)}</td>
            <td>
              {#if j.state === 'complete'}<span class="tag-emerald">{t('journals.complete')}</span>
              {:else if j.state === 'interrupted'}<span class="tag-amber">{t('journals.interrupted', { n: num(j.inFlight) })}</span>
              {:else}<span class="tag-slate">{t('journals.notApplied')}</span>{/if}
            </td>
            <td class="text-right whitespace-nowrap">
              {#if j.state !== 'complete'}<button class="btn-primary btn-sm" disabled={busy} onclick={() => recover(j, false)}>{t('journals.resume')}</button>{/if}
              {#if j.state !== 'notApplied'}<button class="btn-ghost btn-sm" disabled={busy} onclick={() => recover(j, true)}>{t('journals.undo')}</button>{/if}
            </td>
          </tr>
        {:else}
          <tr><td colspan="7" class="py-4 text-center text-xs text-slate-400">{t('journals.empty')}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
  <p class="flex gap-1.5 text-xs text-slate-500"><Info size={14} class="shrink-0" />{t('journals.note')}</p>
</section>
