<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { Download, X, CircleCheck, ShieldCheck } from '@lucide/svelte';
  import { store, installTools, cancelTools, flash } from '../lib/store.svelte.js';
  import { t } from '../lib/i18n.svelte.js';
  import { bytes } from '../lib/format.js';
  import Logo from '../components/Logo.svelte';

  const order = ['exiftool', 'ffmpeg', 'ffprobe', 'pdfinfo', 'pdftotext', 'antiword', '7z', 'unrar'];

  const pkgs = $derived(store.tools?.packages || []);
  const running = $derived(!!store.tools?.running);
  const missing = $derived(pkgs.filter((p) => p.installed !== p.version));
  const blocked = $derived(running || !!store.active);

  async function install(ids) {
    try {
      await installTools(ids);
    } catch (e) {
      flash(e.message);
    }
  }

  function pct(pr) {
    return pr && pr.total > 0 ? Math.floor((pr.done * 100) / pr.total) : 0;
  }

  function host(url) {
    try { return new URL(url).host; } catch { return url; }
  }
</script>

<section class="mx-auto max-w-4xl space-y-5">
  <h1 class="text-xl font-semibold text-slate-900">{t('doctor.title')}</h1>
  {#if store.info?.toolsDisabled}<div class="banner border-amber-200 bg-amber-50 text-amber-800">{t('doctor.disabled')}</div>{/if}

  {#if pkgs.length}
    <div class="card space-y-3" data-testid="tools-download">
      <div class="flex flex-wrap items-start gap-3">
        <div class="min-w-0 flex-1">
          <div class="card-h"><Download size={16} />{t('tools.title')}</div>
          <p class="hint mt-1 flex items-start gap-1.5"><ShieldCheck size={14} class="mt-0.5 shrink-0 text-emerald-600" />{t('tools.pinned')}</p>
        </div>
        {#if running}
          <button class="btn-ghost btn-sm" onclick={() => cancelTools()} data-testid="tools-cancel"><X size={12} />{t('tools.cancel')}</button>
        {:else}
          <button class="btn-primary btn-sm" disabled={blocked || !missing.length} onclick={() => install(missing.map((p) => p.id))} data-testid="tools-all">
            <Download size={12} />{missing.length ? t('tools.all', { size: bytes(missing.reduce((a, p) => a + p.size, 0)) }) : t('tools.allDone')}
          </button>
        {/if}
      </div>
      {#if store.active && !running}<p class="hint text-amber-700">{t('tools.jobActive')}</p>{/if}
      <table class="tbl">
        <thead><tr><th>{t('tools.package')}</th><th>{t('tools.provides')}</th><th>{t('tools.source')}</th><th class="text-right">{t('tools.size')}</th><th class="w-64">{t('doctor.status')}</th></tr></thead>
        <tbody>
          {#each pkgs as p (p.id)}
            {@const pr = p.progress}
            <tr data-testid={'tools-row-' + p.id}>
              <td><div class="font-medium text-slate-800">{p.name}</div><div class="font-mono text-xs text-slate-500">{p.version} · {p.license}</div></td>
              <td class="font-mono text-xs">{p.provides.join(', ')}</td>
              <td class="font-mono text-xs whitespace-nowrap text-slate-500">{host(p.home)}</td>
              <td class="num">{bytes(p.size)}</td>
              <td>
                {#if pr && (pr.phase === 'queued' || pr.phase === 'download' || pr.phase === 'extract')}
                  <div class="flex items-center justify-between text-xs text-slate-600">
                    <span class="whitespace-nowrap">{t('tools.phase.' + pr.phase)}</span>
                    {#if pr.phase === 'download'}<span class="whitespace-nowrap font-mono">{bytes(pr.done)} / {bytes(pr.total)} · {pct(pr)}%</span>{/if}
                  </div>
                  <div class="mt-1 h-1.5 overflow-hidden rounded-full bg-slate-100">
                    <div class="h-full transition-all {pr.phase === 'queued' ? 'bg-slate-300' : 'stripes bg-brand-600'}" style="width:{pr.phase === 'extract' ? 100 : pct(pr)}%" data-testid={'tools-bar-' + p.id}></div>
                  </div>
                {:else}
                  <div class="flex items-center gap-2">
                    {#if p.installed === p.version}
                      <span class="tag-emerald"><CircleCheck size={12} />{t('tools.installed', { v: p.installed })}</span>
                    {:else if p.installed}
                      <span class="tag-amber">{t('tools.older', { v: p.installed })}</span>
                    {:else}
                      <span class="tag-slate">{t('tools.notInstalled')}</span>
                    {/if}
                    {#if !running}
                      <button class="link ml-auto text-xs" disabled={blocked} onclick={() => install([p.id])} data-testid={'tools-get-' + p.id}>
                        {p.installed === p.version ? t('tools.reinstall') : t('tools.get')}
                      </button>
                    {/if}
                  </div>
                  {#if pr?.phase === 'error'}<div class="mt-1 text-xs {pr.err === 'cancelled' ? 'text-amber-700' : 'text-rose-700'}" data-testid={'tools-err-' + p.id}>{pr.err === 'cancelled' ? t('tools.cancelled') : pr.err}</div>{/if}
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
      <p class="hint">{t('tools.where')} <span class="font-mono">{store.tools.dir}</span></p>
    </div>
  {:else if store.tools}
    <p class="hint" data-testid="tools-pkgmgr">{t('tools.pkgManager')}</p>
  {/if}

  <div class="card p-0">
    <table class="tbl">
      <thead><tr><th>{t('doctor.tool')}</th><th>{t('doctor.status')}</th><th>{t('doctor.usedFor')}</th></tr></thead>
      <tbody>
        {#each order as name (name)}
          {@const p = store.info?.tools?.[name]}
          <tr><td class="font-mono text-xs whitespace-nowrap">{name}</td><td>{#if p}<span class="tag-emerald font-mono">{p}</span>{:else}<span class="tag-slate">{t('doctor.missing')}</span>{/if}</td><td>{t('tool.' + name)}</td></tr>
        {/each}
      </tbody>
    </table>
  </div>
  <p class="text-slate-500">{t('doctor.native')}</p>
  <div class="card flex flex-wrap items-start gap-6" data-testid="about">
    <div class="space-y-1">
      <Logo height={26} />
      <div class="text-xs text-slate-500">{t('doctor.tagline')}</div>
    </div>
    <dl class="grid flex-1 grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
      <dt class="text-slate-500">{t('doctor.version')}</dt><dd class="font-mono" data-testid="about-version">{store.info?.version}</dd>
      {#if store.info?.build}<dt class="text-slate-500">{t('doctor.build')}</dt><dd class="font-mono">{store.info.build}</dd>{/if}
      <dt class="text-slate-500">{t('doctor.platform')}</dt><dd class="font-mono">{store.info?.os}/{store.info?.arch} · {store.info?.cpus} CPU · {store.info?.go}</dd>
      <dt class="text-slate-500">{t('doctor.state')}</dt><dd class="font-mono break-all">{store.info?.stateDir}</dd>
      <dt class="text-slate-500">{t('doctor.license')}</dt><dd>{t('doctor.licenseText')}</dd>
      <dt class="text-slate-500">{t('doctor.source')}</dt><dd class="font-mono select-all">https://github.com/mauriziofonte/onefold</dd>
    </dl>
  </div>
</section>
