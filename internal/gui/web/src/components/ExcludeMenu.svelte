<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { Ban } from '@lucide/svelte';
  import { t } from '../lib/i18n.svelte.js';
  import { ancestors, relTo } from '../lib/paths.js';

  // Exclusion choices for one source: the file, one of its folders
  // (down to its room, which stays), or its content everywhere.
  // onexclude({ path } | { contentOf }).
  let { path, room = '', content = false, onexclude } = $props();
  let open = $state(false);
  const folders = $derived(open ? ancestors(path, room).filter((d) => !room || d.length > room.replace(/[\\/]+$/, '').length) : []);

  function pick(v) {
    open = false;
    onexclude(v);
  }
</script>

<div class="relative inline-block">
  <button class="icon-btn" title={t('plan.exclude')} onclick={() => (open = !open)} data-testid="row-exclude"><Ban size={14} /></button>
  {#if open}
    <div class="absolute right-0 z-20 mt-1 w-96 max-w-[70vw] rounded-lg border border-slate-200 bg-white p-1 text-left text-xs shadow-lg" role="menu">
      <button role="menuitem" class="block w-full rounded px-2 py-1.5 text-left hover:bg-slate-100" onclick={() => pick({ path })} data-testid="exclude-file">{t('plan.excludeFile')}</button>
      {#each folders as d (d)}
        <button role="menuitem" class="block w-full rounded px-2 py-1.5 text-left hover:bg-slate-100" onclick={() => pick({ path: d })} data-testid="exclude-folder">
          {t('plan.excludeFolder')} <span class="font-mono text-slate-600">{relTo(d, room) || d}</span>
        </button>
      {/each}
      {#if content}
        <button role="menuitem" class="block w-full rounded border-t border-slate-100 px-2 py-1.5 text-left hover:bg-slate-100" onclick={() => pick({ contentOf: path })} data-testid="exclude-content">{t('plan.excludeContent')}</button>
      {/if}
      <button role="menuitem" class="block w-full rounded px-2 py-1.5 text-left text-slate-400 hover:bg-slate-100" onclick={() => (open = false)}>{t('common.cancel')}</button>
    </div>
  {/if}
</div>
