<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { PackageOpen } from '@lucide/svelte';
  import { api } from '../lib/api.js';
  import { t } from '../lib/i18n.svelte.js';
  import { num, bytes } from '../lib/format.js';
  import { applyExclusions } from '../lib/plan.js';

  // Folders that look like driver or installer packages: flagged,
  // never excluded on their own; "exclude them all" adds them as exact-path
  // exclusions. pkgs (bindable): [{ src: folder, note: kind, size, dst: files }].
  let { job, cfg, canExclude = false, pkgs = $bindable([]) } = $props();
  let open = $state(false);
  const n = $derived(job?.summary?.packages || 0);

  $effect(() => {
    const id = job?.id;
    job?.updated;
    if (!id || !n) { pkgs = []; return; }
    api.get(`/api/jobs/${id}/items?list=packages&limit=5000`).then((r) => { pkgs = r.items || []; }).catch(() => { pkgs = []; });
  });
  const kinds = $derived(pkgs.reduce((m, p) => ({ ...m, [p.note]: (m[p.note] || 0) + 1 }), {}));
  const files = $derived(pkgs.reduce((a, p) => a + (Number(p.dst) || 0), 0));
  const size = $derived(pkgs.reduce((a, p) => a + (p.size || 0), 0));

  function excludeAll() {
    const paths = [...(cfg.filters.excludePaths || [])];
    for (const p of pkgs) if (!paths.includes(p.src)) paths.push(p.src);
    applyExclusions(cfg, job, { paths, globs: cfg.filters.excludeGlobs || [] });
  }
</script>

{#if n && pkgs.length}
  <div class="banner flex-wrap border-amber-200 bg-amber-50 text-amber-800" data-testid="packages">
    <PackageOpen size={16} class="shrink-0" />
    <span class="flex-1">{t('packages.banner', { n: num(pkgs.length), driver: num(kinds.driver || 0), installer: num(kinds.installer || 0), files: num(files), size: bytes(size) })}</span>
    <button class="btn-ghost btn-sm" onclick={() => (open = !open)}>{open ? t('packages.hide') : t('packages.show')}</button>
    {#if canExclude}<button class="btn-primary btn-sm" onclick={excludeAll} data-testid="packages-exclude">{t('packages.excludeAll')}</button>{/if}
    {#if open}
      <ul class="mt-1 w-full space-y-0.5 text-xs">
        {#each pkgs as p (p.src)}<li class="font-mono"><span class="tag-amber mr-1 font-sans">{t('packages.' + p.note)}</span>{p.src} <span class="font-sans text-amber-700">· {t('plan.nFiles', { n: num(Number(p.dst) || 0) })}, {bytes(p.size || 0)}</span></li>{/each}
      </ul>
    {/if}
  </div>
{/if}
