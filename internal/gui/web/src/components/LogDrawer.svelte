<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { ScrollText, Clipboard, Download, ChevronUp, ChevronDown } from '@lucide/svelte';
  import { store } from '../lib/store.svelte.js';
  import { settings } from '../lib/settings.svelte.js';
  import { t } from '../lib/i18n.svelte.js';
  import { clock, num } from '../lib/format.js';

  let body = $state();
  const entries = $derived(store.jobLog ? store.jobLog.entries : store.log);
  const counts = $derived({
    error: entries.filter((e) => e.level === 'error').length,
    warn: entries.filter((e) => e.level === 'warn').length,
    info: entries.filter((e) => e.level === 'info').length,
  });
  const shown = $derived(settings.logFilter === 'all' ? entries : entries.filter((e) => e.level === settings.logFilter));
  const last = $derived(entries[entries.length - 1]);
  let seenErrors = $state(0);
  const alert = $derived(!settings.drawerOpen && counts.error > seenErrors);

  function text(e) {
    const kindArgs = e.args ? { ...e.args } : {};
    if (kindArgs.kind) kindArgs.kind = t('kind.' + kindArgs.kind);
    if (kindArgs.stage) kindArgs.stage = t('stage.' + kindArgs.stage);
    if (kindArgs.status) kindArgs.status = t('status.' + kindArgs.status);
    const k = e.key ? t(e.key, kindArgs) : '';
    const path = e.path && !(e.msg || '').includes(e.path) ? e.path : '';
    return [k, e.msg, path].filter(Boolean).join(' · ');
  }

  $effect(() => {
    if (settings.drawerOpen) seenErrors = counts.error;
  });
  $effect(() => {
    shown.length;
    if (body) queueMicrotask(() => (body.scrollTop = body.scrollHeight));
  });

  function asText() {
    return shown.map((e) => `${e.t} ${e.level.toUpperCase()} ${text(e)}`).join('\n');
  }
  function copy() {
    navigator.clipboard?.writeText(asText());
  }
  function download() {
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([asText()], { type: 'text/plain' }));
    a.download = 'onefold-log.txt';
    a.click();
    URL.revokeObjectURL(a.href);
  }
  const level = { error: 'ERROR', warn: 'WARN', info: 'INFO' };
  const style = { error: 'text-rose-700 bg-rose-50/70', warn: 'text-amber-800', info: 'text-slate-600' };
</script>

<section class="flex shrink-0 flex-col border-t border-slate-200 bg-white transition-all {alert ? 'ring-2 ring-rose-300' : ''}" style="height:{settings.drawerOpen ? '280px' : '40px'}" data-testid="logdrawer">
  <div class="flex h-10 shrink-0 cursor-pointer items-center gap-3 px-4 select-none" role="button" tabindex="0"
    onclick={() => (settings.drawerOpen = !settings.drawerOpen)} onkeydown={(e) => e.key === 'Enter' && (settings.drawerOpen = !settings.drawerOpen)}>
    <ScrollText size={16} class="text-slate-500" /><b class="text-slate-700">{store.jobLog ? t('log.ofRun', { id: store.jobLog.id }) : t('log.title')}</b>
    <span class="pill bg-rose-100 text-rose-700" data-testid="log-errors">{t('log.errors', { n: num(counts.error) })}</span>
    <span class="pill bg-amber-100 text-amber-700">{t('log.warnings', { n: num(counts.warn) })}</span>
    <span class="pill bg-slate-100 text-slate-600">{t('log.info', { n: num(counts.info) })}</span>
    {#if store.jobLog}<button class="link text-xs" onclick={(e) => { e.stopPropagation(); store.jobLog = null; }}>{t('log.backLive')}</button>{/if}
    <span class="max-w-xl truncate text-xs text-slate-400">{last ? clock(last.t) + '  ' + text(last) : ''}</span>
    <div class="ml-auto flex items-center gap-1" role="toolbar" tabindex="-1" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()}>
      <button class="flt" class:flt-on={settings.logFilter === 'all'} onclick={() => (settings.logFilter = 'all')}>{t('log.all')}</button>
      <button class="flt" class:flt-on={settings.logFilter === 'error'} onclick={() => (settings.logFilter = 'error')}>{t('log.filterErrors')}</button>
      <button class="flt" class:flt-on={settings.logFilter === 'warn'} onclick={() => (settings.logFilter = 'warn')}>{t('log.filterWarnings')}</button>
      <button class="icon-btn" title={t('log.copy')} onclick={copy}><Clipboard size={14} /></button>
      <button class="icon-btn" title={t('log.save')} onclick={download}><Download size={14} /></button>
    </div>
    <span class="flex items-center gap-1 text-xs text-slate-400">
      {#if settings.drawerOpen}<ChevronDown size={14} />{t('log.collapse')}{:else}<ChevronUp size={14} />{t('log.expand')}{/if}
    </span>
  </div>
  <div bind:this={body} class="scroll-thin flex-1 overflow-auto border-t border-slate-100 font-mono text-xs" data-testid="log-body">
    {#each shown as e, i (i)}
      <div class="border-b border-slate-50 px-4 py-1 {style[e.level]}"><span class="text-slate-400">{clock(e.t)}</span>&nbsp;&nbsp;<b class="inline-block w-12">{level[e.level]}</b>{text(e)}</div>
    {:else}
      <div class="px-4 py-2 text-slate-400">{t('log.empty')}</div>
    {/each}
  </div>
</section>
