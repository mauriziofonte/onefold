<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { ChevronRight, ChevronDown, Folder, Ban } from '@lucide/svelte';
  import { api } from '../lib/api.js';
  import { t } from '../lib/i18n.svelte.js';
  import { num, bytes } from '../lib/format.js';
  import { roomOf } from '../lib/paths.js';
  import ExcludeMenu from './ExcludeMenu.svelte';
  import Self from './TreeBranch.svelte';

  // One level of the plan grouped by source folder, loaded on demand.
  let { jobId, list, dir = '', q = '', ext = '', version = '', rooms = [], labels = [], exclude = false, onexclude = null,
    note = null, abs = null, depth = 0 } = $props();
  let nodes = $state([]);
  let more = $state(false);
  let open = $state({});

  $effect(() => {
    const p = new URLSearchParams({ list, dir, q, ext });
    version;
    api.get(`/api/jobs/${jobId}/tree?${p}`).then((r) => {
      nodes = r.nodes || [];
      more = !!r.more;
    }).catch(() => { nodes = []; });
  });

  const full = (p) => (abs ? abs(p) : p);
  const isRoom = (n) => depth === 0 && rooms.some((r) => r === n.path);
  const label = (n) => (isRoom(n) ? ((labels[rooms.indexOf(n.path)] || '') + ' ' + n.path).trim() : n.name);
</script>

{#each nodes as n (n.path)}
  {#if n.dir}
    <div class="flex items-center gap-1 border-b border-slate-100 py-1 pr-3 text-xs hover:bg-slate-50" style="padding-left: {0.75 + depth * 1.1}rem" data-testid="tree-folder">
      <button class="flex min-w-0 flex-1 items-center gap-1 text-left" onclick={() => (open[n.path] = !open[n.path])}>
        {#if open[n.path]}<ChevronDown size={14} class="shrink-0" />{:else}<ChevronRight size={14} class="shrink-0" />{/if}
        <Folder size={14} class="shrink-0 text-amber-500" /><span class="truncate font-mono" title={full(n.path)}>{label(n)}</span>
      </button>
      <span class="w-28 text-right text-slate-500 tabular-nums">{t('plan.nFiles', { n: num(n.files) })}</span>
      <span class="w-20 text-right tabular-nums">{bytes(n.bytes)}</span>
      {#if exclude && !isRoom(n)}
        <button class="icon-btn" title={t('plan.excludeFolder') + ' ' + n.name} onclick={() => onexclude({ path: full(n.path) })} data-testid="tree-exclude"><Ban size={14} /></button>
      {:else}<span class="w-7"></span>{/if}
    </div>
    {#if open[n.path]}
      <Self {jobId} {list} dir={n.path} {q} {ext} {version} {rooms} {labels} {exclude} {onexclude} {note} {abs} depth={depth + 1} />
    {/if}
  {:else}
    <div class="flex items-center gap-2 border-b border-slate-100 py-1 pr-3 text-xs" style="padding-left: {2.1 + depth * 1.1}rem">
      <span class="min-w-0 flex-1 truncate font-mono" title={full(n.path)}>{n.name}</span>
      <span class="min-w-0 flex-1 truncate font-mono text-slate-500" title={n.dst}>→ {n.dst || ''}{#if n.note && note} · {note(n.note)}{/if}</span>
      <span class="w-20 text-right tabular-nums">{n.bytes ? bytes(n.bytes) : ''}</span>
      {#if exclude}<ExcludeMenu path={full(n.path)} room={roomOf(full(n.path), rooms).room} {onexclude} />{:else}<span class="w-7"></span>{/if}
    </div>
  {/if}
{/each}
{#if more}<div class="px-3 py-1 text-xs text-slate-400" style="padding-left: {2.1 + depth * 1.1}rem">{t('plan.treeMore')}</div>{/if}
{#if depth === 0 && !nodes.length}<div class="py-4 text-center text-xs text-slate-400">{t('common.empty')}</div>{/if}
