<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { Plus, X, ChevronUp, ChevronDown } from '@lucide/svelte';
  import { t } from '../lib/i18n.svelte.js';
  import { pickFolder } from '../lib/dialogs.svelte.js';

  let { items = $bindable([]), tag = null, testid = '' } = $props();
  let typed = $state('');

  async function add() {
    const p = await pickFolder(items[items.length - 1] || '');
    if (p && !items.includes(p)) items = [...items, p];
  }
  function addTyped(e) {
    e.preventDefault();
    const p = typed.trim();
    if (p && !items.includes(p)) items = [...items, p];
    typed = '';
  }
  function move(i, d) {
    const a = [...items];
    [a[i], a[i + d]] = [a[i + d], a[i]];
    items = a;
  }
</script>

<ul class="divide-y divide-slate-100" data-testid={testid}>
  {#each items as p, i (p)}
    <li class="row">
      <span class="badge">{i + 1}</span>
      <span class="mono flex-1">{p}</span>
      {#if tag && tag(p)}<span class="tag-brand">{tag(p)}</span>{/if}
      <button class="icon-btn" disabled={i === 0} onclick={() => move(i, -1)} title={t('common.up')}><ChevronUp size={14} /></button>
      <button class="icon-btn" disabled={i === items.length - 1} onclick={() => move(i, 1)} title={t('common.down')}><ChevronDown size={14} /></button>
      <button class="icon-btn" onclick={() => (items = items.filter((x) => x !== p))} title={t('common.remove')}><X size={14} /></button>
    </li>
  {/each}
</ul>
<form class="mt-3 flex gap-2" onsubmit={addTyped}>
  <input class="input flex-1 font-mono text-xs" bind:value={typed} placeholder={t('common.folderPlaceholder')} data-testid={testid ? testid + '-input' : ''} />
  <button class="btn-ghost" type="button" onclick={add}><Plus size={14} />{t('common.addFolder')}</button>
</form>
