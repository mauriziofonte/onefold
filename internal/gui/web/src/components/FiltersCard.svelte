<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { Filter, ChevronDown } from '@lucide/svelte';
  import { t } from '../lib/i18n.svelte.js';
  import { store } from '../lib/store.svelte.js';
  import ExclusionsBox from './ExclusionsBox.svelte';

  // f: the filter settings of a mode ({ maxSize, exclude, rules }); the file
  // types come from the families (merge, rearrange) or are fixed (golden
  // archive), never from here. rules: catalog rule name -> on (missing =
  // on). Rules that cannot change the scope are not shown: unknown extensions
  // and files without one follow "everything else"; system extensions,
  // and shortcuts, only matter with it; the golden scope is photos and videos.
  // onexclusions(paths, globs): how a change of the exclusions applies (a
  // re-plan of a previewed job); default: the form only.
  let { f, rest = false, golden = false, onexclusions = null } = $props();
  // open by itself when exclusions are set: they change the plan
  const open = (f.excludePaths?.length || 0) + (f.excludeGlobs?.length || 0) > 0;
  const hidden = $derived(new Set(['unknown-ext', 'noext', 'big', ...(rest ? [] : ['system-ext']), ...(golden ? ['system-ext', 'shortcuts'] : [])]));
  const rules = $derived((store.naming?.rules || []).filter((r) => !hidden.has(r.name)));
  const on = (name) => f.rules?.[name] !== false;
  function set(name, v) {
    f.rules = { ...(f.rules || {}), [name]: v };
  }
  function exclusions(paths, globs) {
    if (onexclusions) return onexclusions(paths, globs);
    f.excludePaths = paths;
    f.excludeGlobs = globs;
  }
</script>

<details class="card group" {open} data-testid="filters-card">
  <summary class="card-h mb-0 cursor-pointer list-none"><Filter size={16} />{t('filters.title')} <span class="hint">{t('filters.hint')}</span><ChevronDown size={16} class="ml-auto transition group-open:rotate-180" /></summary>
  <div class="mt-3 grid grid-cols-2 gap-x-4 gap-y-2" data-testid="filter-rules">
    {#each rules as r (r.name)}
      <label class="chk items-start" title={r.doc}><input class="mt-1" type="checkbox" checked={on(r.name)} onchange={(e) => set(r.name, e.currentTarget.checked)} data-testid={'rule-' + r.name} />
        <span><span>{t('crule.' + r.name)}</span><span class="block text-xs text-slate-400">{t('crule.' + r.name + '.ex')}</span></span>
      </label>
    {/each}
    <label><span class="lbl">{t('filters.maxSize')}</span><input class="input mt-1" bind:value={f.maxSize} /></label>
    <label class="col-span-2"><span class="lbl">{t('filters.exclude')}</span><input class="input mt-1" bind:value={f.exclude} placeholder="Backup old, .trash" /></label>
    <div class="col-span-2 border-t border-slate-100 pt-2"><ExclusionsBox paths={f.excludePaths || []} globs={f.excludeGlobs || []} onchange={exclusions} /></div>
  </div>
</details>
