<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { FolderOpen, Folder, HardDrive, ArrowUp, X } from '@lucide/svelte';
  import { api } from '../lib/api.js';
  import { t } from '../lib/i18n.svelte.js';
  import { dialogs } from '../lib/dialogs.svelte.js';
  import { settings } from '../lib/settings.svelte.js';

  let path = $state('');
  let entries = $state([]);
  let roots = $state([]);
  let parent = $state('');
  let error = $state('');
  let hidden = $state(false);
  let typed = $state('');

  const d = $derived(dialogs.picker);

  async function load(p) {
    error = '';
    try {
      const r = await api.get('/api/fs?path=' + encodeURIComponent(p) + (hidden ? '&hidden=1' : ''));
      roots = r.roots || [];
      path = r.path || '';
      parent = r.parent || '';
      entries = r.entries || [];
      typed = path;
    } catch (e) {
      error = e.message;
    }
  }

  $effect(() => {
    if (d) load(d.start || settings.pickerLast || '');
  });

  function close(v) {
    const r = d?.resolve;
    dialogs.picker = null;
    if (v) settings.pickerLast = v;
    r?.(v || '');
  }
</script>

{#if d}
  <div class="fixed inset-0 z-50 grid place-items-center bg-slate-900/30" role="dialog" aria-modal="true">
    <div class="flex max-h-[85vh] w-[640px] flex-col gap-3 rounded-xl bg-white p-5 shadow-xl">
      <div class="flex items-center gap-2 font-semibold text-slate-900">
        <FolderOpen size={16} />{d.title || t('picker.title')}
        <button class="icon-btn ml-auto" onclick={() => close('')} aria-label={t('common.cancel')}><X size={16} /></button>
      </div>
      <form class="flex gap-2" onsubmit={(e) => { e.preventDefault(); load(typed); }}>
        <input class="input font-mono text-xs" bind:value={typed} placeholder={t('common.folderPlaceholder')} />
        <button class="btn-ghost btn-sm" type="button" disabled={!parent} onclick={() => load(parent)}><ArrowUp size={14} />{t('picker.up')}</button>
      </form>
      <div class="flex flex-wrap items-center gap-1 text-xs">
        <span class="text-slate-400">{t('picker.places')}:</span>
        {#each roots as r (r.path)}
          <button class="crumb flex items-center gap-1" onclick={() => load(r.path)}><HardDrive size={12} />{r.name}</button>
        {/each}
      </div>
      {#if error}<div class="banner border-rose-200 bg-rose-50 text-rose-700 text-xs">{error}</div>{/if}
      <ul class="scroll-thin min-h-40 flex-1 divide-y divide-slate-100 overflow-auto rounded-lg border border-slate-200">
        {#each entries as e (e.path)}
          <li>
            <button class="row w-full hover:bg-indigo-50" ondblclick={() => close(e.path)} onclick={() => load(e.path)}>
              <Folder size={16} class="text-amber-400" />{e.name}
            </button>
          </li>
        {:else}
          <li class="p-3 text-xs text-slate-400">{path ? t('picker.empty') : ''}</li>
        {/each}
      </ul>
      <div class="flex items-center gap-2">
        <label class="chk text-xs"><input type="checkbox" bind:checked={hidden} onchange={() => load(path)} />{t('picker.hidden')}</label>
        <div class="ml-auto flex gap-2">
          <button class="btn-ghost" onclick={() => close('')}>{t('common.cancel')}</button>
          <button class="btn-primary max-w-80 truncate" disabled={!path} onclick={() => close(path)}>{t('picker.select', { path: path || '…' })}</button>
        </div>
      </div>
    </div>
  </div>
{/if}
