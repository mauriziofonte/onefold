<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { GitMerge, FolderTree, Sparkles, History, Stethoscope, BookOpen, WifiOff, CircleAlert, X, Settings, Wand, Info, CircleCheck } from '@lucide/svelte';
  import Logo from './components/Logo.svelte';
  import { onMount } from 'svelte';
  import { store, init, jobsSorted } from './lib/store.svelte.js';
  import { settings, save } from './lib/settings.svelte.js';
  import { t, setLang, lang, LANGS } from './lib/i18n.svelte.js';
  import { api } from './lib/api.js';
  import JobBar from './components/JobBar.svelte';
  import LogDrawer from './components/LogDrawer.svelte';
  import FolderPicker from './components/FolderPicker.svelte';
  import Confirm from './components/Confirm.svelte';
  import Merge from './views/Merge.svelte';
  import Rearrange from './views/Rearrange.svelte';
  import Golden from './views/Golden.svelte';
  import Runs from './views/Runs.svelte';
  import Journals from './views/Journals.svelte';
  import Doctor from './views/Doctor.svelte';
  import Config from './views/Config.svelte';
  import Wizard from './views/Wizard.svelte';

  let ready = $state(false);
  let error = $state('');
  let presets = $state([]);

  onMount(async () => {
    try {
      await init();
      setLang(settings.lang || store.info.lang);
      const n = await api.get('/api/naming');
      presets = n.presets || [];
      store.naming = n;
      ready = true;
    } catch (e) {
      error = e.message;
    }
  });

  // persist every settings change (localStorage)
  $effect(() => {
    JSON.stringify(settings);
    save();
  });

  function chooseLang(l) {
    settings.lang = l;
    setLang(l);
  }

  const tools = $derived(store.info ? Object.values(store.info.tools || {}).filter(Boolean).length : 0);
  const toolsTotal = $derived(store.info ? Object.keys(store.info.tools || {}).length : 0);
  const unfinished = $derived(jobsSorted().filter((j) => j.status === 'interrupted' || j.status === 'stopped'));

  const nav = [
    { group: 'nav.start', items: [['wizard', Wand]] },
    { group: 'nav.modes', items: [['merge', GitMerge], ['rearrange', FolderTree], ['golden', Sparkles]] },
    { group: 'nav.history', items: [['runs', History], ['journals', BookOpen]] },
    { group: 'nav.system', items: [['config', Settings], ['doctor', Stethoscope]] },
  ];

  // open a job in the view of its mode
  function openJob(j) {
    if (j.kind === 'merge' || j.kind === 'rearrange') {
      settings[j.kind].jobId = j.id;
      settings[j.kind].tab = 'plan';
      settings.view = j.kind;
    } else if (j.kind === 'purge') {
      settings.merge.purgeJobId = j.id;
      settings.merge.tab = 'plan';
      settings.view = 'merge';
    } else if (j.kind === 'recover') {
      settings.view = 'journals';
    } else {
      settings.golden[j.kind + 'JobId'] = j.id;
      settings.view = 'golden';
    }
  }
</script>

<div class="flex h-full flex-col overflow-hidden">
  <header class="flex h-12 shrink-0 items-center gap-3 border-b border-slate-200 bg-white px-4">
    <div class="flex items-center gap-2">
      <Logo height={22} />
      <span class="text-xs font-normal text-slate-400">{store.info?.version || ''}</span>
    </div>
    {#if store.info}
      <div class="ml-4 flex items-center gap-2 text-xs">
        <span class="rounded-full bg-slate-100 px-2 py-0.5 text-slate-600">{store.info.os === 'windows' ? 'Windows' : store.info.os === 'linux' ? 'Linux' : store.info.os === 'darwin' ? 'macOS' : store.info.os} · {store.info.cpus} CPU</span>
        <button class="rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-emerald-700 hover:bg-emerald-100" onclick={() => (settings.view = 'doctor')}>
          {tools ? t('header.tools', { n: tools, total: toolsTotal }) : t('header.toolsNone')}
        </button>
      </div>
    {/if}
    <div class="ml-auto flex items-center gap-3">
      {#if ready && !store.connected}<span class="flex items-center gap-1 text-xs text-amber-600"><WifiOff size={14} />{t('header.offline')}</span>{/if}
      <div class="flex overflow-hidden rounded-lg border border-slate-300 text-xs" role="group" aria-label={t('header.lang')} data-testid="lang-switch">
        {#each LANGS as l (l)}
          <button class="px-2 py-1 {lang.current === l ? 'bg-slate-800 text-white' : 'bg-white text-slate-600 hover:bg-slate-50'}" onclick={() => chooseLang(l)}>{l.toUpperCase()}</button>
        {/each}
      </div>
    </div>
  </header>

  {#if ready}
    <JobBar onopen={openJob} />
  {/if}

  <div class="flex min-h-0 flex-1">
    <nav class="flex w-56 shrink-0 flex-col gap-1 border-r border-slate-200 bg-white p-3">
      {#each nav as g (g.group)}
        <div class="px-2 pt-3 pb-2 text-[11px] font-semibold tracking-wide text-slate-400 uppercase first:pt-1">{t(g.group)}</div>
        {#each g.items as [v, Icon] (v)}
          <button class="nav-btn" class:nav-on={settings.view === v} onclick={() => (settings.view = v)} data-testid={'nav-' + v}>
            <Icon size={16} />{t('nav.' + v)}
            {#if v === 'runs' && unfinished.length}<span class="ml-auto pill bg-amber-100 text-amber-700">{unfinished.length}</span>{/if}
          </button>
        {/each}
      {/each}
      <div class="mt-auto rounded-lg border border-slate-200 bg-slate-50 p-2 text-xs leading-relaxed text-slate-500">{@html t('nav.note')}</div>
    </nav>

    <main class="scroll-thin flex-1 overflow-auto p-6">
      {#if error}
        <div class="banner border-rose-200 bg-rose-50 text-rose-800"><CircleAlert size={16} />{error}</div>
      {:else if ready}
        {#if unfinished.length && settings.view !== 'runs'}
          <div class="banner mx-auto mb-4 max-w-6xl border-amber-200 bg-amber-50 text-amber-800" data-testid="unfinished-banner">
            <CircleAlert size={16} />{t('runs.banner', { n: unfinished.length })}
            <button class="link ml-auto" onclick={() => (settings.view = 'runs')}>{t('nav.runs')}</button>
          </div>
        {/if}
        {#if settings.notice}
          <div class="banner mx-auto mb-4 max-w-6xl border-indigo-200 bg-indigo-50 text-indigo-800" data-testid="notice">
            <Info size={16} class="shrink-0" /><span class="flex-1">{t(settings.notice)}</span>
            <button class="icon-btn" onclick={() => (settings.notice = '')}><X size={14} /></button>
          </div>
        {/if}
        {#if settings.view === 'wizard'}<Wizard {presets} />
        {:else if settings.view === 'merge'}<Merge />
        {:else if settings.view === 'rearrange'}<Rearrange />
        {:else if settings.view === 'golden'}<Golden />
        {:else if settings.view === 'runs'}<Runs onopen={openJob} />
        {:else if settings.view === 'journals'}<Journals />
        {:else if settings.view === 'config'}<Config />
        {:else}<Doctor />{/if}
      {/if}
    </main>
  </div>

  <LogDrawer />
  <FolderPicker />
  <Confirm />
  {#if store.toast}
    <div class="fixed right-4 bottom-14 z-40 flex max-w-lg items-start gap-2 rounded-lg border bg-white p-3 text-sm shadow-lg {store.toastOk ? 'border-emerald-200 text-emerald-700' : 'border-rose-200 text-rose-700'}" data-testid="toast">
      {#if store.toastOk}<CircleCheck size={16} class="mt-0.5 shrink-0" />{:else}<CircleAlert size={16} class="mt-0.5 shrink-0" />{/if}<span class="flex-1">{store.toast}</span>
      <button class="icon-btn" onclick={() => (store.toast = '')}><X size={14} /></button>
    </div>
  {/if}
</div>
