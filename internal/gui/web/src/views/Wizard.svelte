<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { Wand, Lock, Check, ArrowLeft, ArrowRight, ScanSearch, Copy, Save, Trash2, CircleAlert, Info } from '@lucide/svelte';
  import { onMount } from 'svelte';
  import { settings, wizardDefaults } from '../lib/settings.svelte.js';
  import { api } from '../lib/api.js';
  import { flash } from '../lib/store.svelte.js';
  import { t } from '../lib/i18n.svelte.js';
  import { num, bytes } from '../lib/format.js';
  import { FAMILIES, GENERIC, RULES, catalogPreset, familiesOf, recommendedChoice } from '../lib/regime.js';
  import { finish, wizardDirs, namingOfWizard, wizardParams } from '../lib/wizard.js';
  import { command } from '../lib/cli.js';
  import PathList from '../components/PathList.svelte';
  import PathField from '../components/PathField.svelte';

  // Guided setup: questions on the goal, a quick scan of the folders and
  // the real outcome on a sample; it ends by filling the form of the mode,
  // which previews. Nothing is written by the wizard.
  let { presets = [] } = $props();
  const w = settings.wizard;
  const GOALS = ['merge', 'rearrange', 'golden', 'dupes'];

  let scan = $state(null); // last quick scan
  let scanKey = $state(''); // the folders it covered
  let scanning = $state(false);
  let rows = $state([]);
  let rowsErr = $state('');
  let recipes = $state([]);
  let recipeName = $state('');
  let timer;

  if (!w.choice) w.choice = recommendedChoice();

  const steps = $derived([
    { k: 'goal' }, { k: 'where' }, { k: 'what', lock: w.goal === 'golden' },
    ...(w.goal === 'dupes' ? [] : [{ k: 'layout' }, { k: 'dupes' }]),
    { k: 'excl' }, { k: 'sum' },
  ]);
  const cur = $derived(steps[Math.min(w.step, steps.length - 1)].k);
  const dirs = $derived(wizardDirs(w).filter((d) => d && d.trim()));
  const dirKey = $derived(dirs.join('\n'));
  const scanned = $derived(!!scan && scanKey === dirKey && dirs.length > 0);
  // the naming the families compose to (server: naming.Compose)
  const families = $derived(familiesOf(namingOfWizard(w)));
  let naming = $state({ naming: '' });
  $effect(() => {
    const body = { families, dirs: [] };
    if (w.goal === 'golden' || !w.goal) return;
    api.post('/api/naming/preview', body).then((r) => { naming = { naming: r.error ? '' : r.naming }; }).catch(() => {});
  });
  const enabledMask = $derived(RULES.reduce((m, r, i) => (w.rules[r] !== false ? m | (1 << i) : m), 0));

  // exact counts from the scan: files the setup considers, per family / class
  function sum(map, keep) {
    let n = 0, b = 0;
    for (const [k, [c, s]] of Object.entries(map || {})) {
      const [key, mask] = k.split('|');
      if (keep(key) && (Number(mask) & enabledMask) === 0) { n += c; b += s; }
    }
    return { n, b };
  }
  const famCount = (f) => sum(scan?.counts, (k) => k === f).n;
  const considered = $derived(w.goal === 'golden' ? sum(scan?.golden, (k) => k === 'in') : sum(scan?.counts, (k) => w.fams.rest || w.fams[k]));
  const goldenOut = $derived({ gifbmp: sum(scan?.golden, (k) => k === 'gifbmp').n, gfx: sum(scan?.golden, (k) => k === 'gfx').n });

  const canNext = $derived(
    (cur !== 'goal' || !!w.goal) &&
    (cur !== 'where' || (scanned && (w.goal !== 'merge' || w.output.trim()) && (w.goal !== 'golden' || w.target.trim()))) &&
    (cur !== 'what' || w.goal === 'golden' || FAMILIES.some((f) => w.fams[f.key])),
  );

  onMount(loadRecipes);
  async function loadRecipes() {
    try { recipes = (await api.get('/api/recipes')).recipes || []; } catch { recipes = []; }
  }

  function chooseGoal(g) {
    const keep = { inputs: w.inputs, output: w.output, root: w.root, rooms: w.rooms, host: w.host, target: w.target };
    Object.assign(w, wizardDefaults(), keep, { goal: g, step: 0, choice: recommendedChoice() });
  }
  function go(i) {
    if (i <= w.step || (scanned && i < steps.length)) w.step = i;
  }
  function next() {
    let i = w.step + 1;
    while (steps[i]?.lock) i++;
    w.step = Math.min(i, steps.length - 1);
  }
  function prev() {
    let i = w.step - 1;
    while (i > 0 && steps[i]?.lock) i--;
    w.step = Math.max(0, i);
  }

  async function runScan() {
    scanning = true;
    try {
      scan = await api.post('/api/wizard/scan', { dirs, goal: w.goal });
      scanKey = dirKey;
      // until the user picks the families, start from the ones that hold files
      if (w.famsAuto && w.goal !== 'golden') {
        const present = FAMILIES.filter((f) => Object.keys(scan.counts).some((k) => k.startsWith(f.key + '|')));
        if (present.length) w.fams = Object.fromEntries(FAMILIES.map((f) => [f.key, present.includes(f)]));
      }
    } catch (e) {
      flash(e.message);
    } finally {
      scanning = false;
    }
  }

  // live preview of the real engine on the sample, after every answer
  $effect(() => {
    const body = {
      goal: w.goal, sample: scanned ? scan.sample : [], fams: FAMILIES.filter((f) => w.fams[f.key]).map((f) => f.key),
      families, rules: { ...w.rules }, exclude: w.exclude, dupes: w.dupes, videoTier: w.videoTier,
    };
    clearTimeout(timer);
    if (!body.sample.length || !w.goal) { rows = []; return; }
    timer = setTimeout(async () => {
      try {
        const r = await api.post('/api/wizard/preview', body);
        rows = r.rows || [];
        rowsErr = r.error || '';
      } catch (e) {
        rowsErr = e.message;
      }
    }, 300);
  });

  const TONE = { dupDelete: 'tag-rose', canon: 'tag-brand', byteCopy: 'tag-brand', markers: 'tag-emerald', collision: 'tag-amber',
    dateName: 'tag-amber', dateMtime: 'tag-amber' };
  const tagText = (g) => t('wtag.' + g.k, { a: g.k === 'excl' ? t('rule.' + g.a) : g.a });

  // the merge/rearrange line comes from the server (settings.Args)
  let cliLine = $state('');
  $effect(() => {
    if (!w.goal || w.goal === 'golden') { cliLine = ''; return; }
    const req = wizardParams(w);
    api.post('/api/cli', req).then((r) => { cliLine = r.command || ''; }).catch(() => { cliLine = ''; });
  });
  const cli = $derived(command(w, t, cliLine));

  async function copyCli() {
    try { await navigator.clipboard.writeText(cli); flash(t('wizard.copied'), true); } catch { /* clipboard denied */ }
  }

  async function saveRecipe() {
    const name = recipeName.trim() || t('wizard.recipeDefault', { n: recipes.length + 1 });
    const answers = $state.snapshot(w);
    delete answers.step;
    try {
      recipes = (await api.post('/api/recipes', { name, goal: w.goal, answers })).recipes || [];
      recipeName = '';
      flash(t('wizard.recipeSaved', { name }), true);
    } catch (e) {
      flash(e.message);
    }
  }
  async function applyRecipe(r) {
    Object.assign(w, wizardDefaults(), r.answers);
    w.step = steps.length - 1;
    await runScan();
  }
  async function deleteRecipe(r) {
    recipes = (await api.del('/api/recipes/' + r.id)).recipes || [];
  }

  function done() {
    finish($state.snapshot(w));
  }
  const examples = (k) => rows.filter((r) => r.fam === k && r.dst && !r.skip && !r.dup).slice(0, 2);
</script>

<section class="mx-auto max-w-7xl space-y-4" data-testid="wizard">
  <div>
    <h1 class="flex items-center gap-2 text-xl font-semibold text-slate-900"><Wand size={18} />{t('wizard.title')}</h1>
    <p class="text-slate-500">{t('wizard.desc')}</p>
  </div>

  <div class="flex flex-wrap gap-1.5" data-testid="wizard-steps">
    {#each steps as s, i (s.k)}
      <button class="phase {s.lock ? 'border-dashed border-slate-300 bg-slate-100 text-slate-400' : i < w.step ? 'border-emerald-200 text-emerald-700' : i === w.step ? 'border-brand-200 bg-brand-50 font-medium text-brand-700' : 'border-slate-200 text-slate-500'} {!w.goal && i > 0 ? 'opacity-40' : ''}"
        onclick={() => go(i)} disabled={!w.goal && i > 0}>
        {#if s.lock}<Lock size={12} />{:else if i < w.step}<Check size={12} />{:else}{i + 1}{/if}
        {t('wizard.step.' + s.k)}
      </button>
    {/each}
  </div>

  <div class="grid grid-cols-1 items-start gap-5 xl:grid-cols-[minmax(0,1fr)_28rem]">
    <div class="space-y-4">
      {#if cur === 'goal'}
        <div class="card">
          <div class="card-h">{t('wizard.goal.q')}</div>
          <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
            {#each GOALS as g (g)}
              <button class="flex flex-col items-start rounded-xl border-2 p-3 text-left {w.goal === g ? 'border-brand-600 bg-brand-50' : 'border-slate-200 hover:border-brand-200'}" onclick={() => chooseGoal(g)} data-testid={'goal-' + g}>
                <div class="font-semibold text-slate-900">{t('wizard.goal.' + g)}</div>
                <div class="mt-1 text-sm text-slate-600">{t('wizard.goal.' + g + '.d')}</div>
                <div class="mt-2 border-t border-dashed border-slate-200 pt-2 text-xs text-slate-500"><b>{t('wizard.eff')}</b> {t('wizard.goal.' + g + '.e')}</div>
              </button>
            {/each}
          </div>
        </div>
        <div class="card">
          <div class="card-h">{t('wizard.recipes')} <span class="hint">{t('wizard.recipesHint')}</span></div>
          {#each recipes as r (r.id)}
            <div class="mb-2 flex items-center gap-3 rounded-lg border border-slate-200 px-3 py-2" data-testid="recipe">
              <b>{r.name}</b><span class="text-xs text-slate-500">{t('wizard.goal.' + r.goal)}</span>
              <button class="btn-ghost btn-sm ml-auto" onclick={() => applyRecipe(r)}>{t('wizard.recipeApply')}</button>
              <button class="icon-btn" onclick={() => deleteRecipe(r)} title={t('common.remove')}><Trash2 size={14} /></button>
            </div>
          {:else}
            <p class="text-xs text-slate-400">{t('wizard.noRecipes')}</p>
          {/each}
        </div>

      {:else if cur === 'where'}
        <div class="card space-y-3">
          {#if w.goal === 'merge' || w.goal === 'dupes'}
            <div class="card-h mb-0">{t('wizard.where.inputs')}</div>
            <p class="text-xs text-slate-500">{t(w.goal === 'merge' ? 'wizard.where.inputsHint' : 'wizard.where.dupesHint')}</p>
            <PathList bind:items={w.inputs} testid="wizard-inputs" />
            {#if w.goal === 'merge'}
              <span class="lbl">{t('wizard.where.output')}</span>
              <PathField bind:value={w.output} testid="wizard-output" />
              <p class="text-xs text-slate-500">{t('wizard.where.outputHint')}</p>
            {/if}
          {:else if w.goal === 'rearrange'}
            <div class="card-h mb-0">{t('wizard.where.root')}</div>
            <p class="text-xs text-slate-500">{t('wizard.where.rootHint')}</p>
            <PathField bind:value={w.root} testid="wizard-root" />
          {:else}
            <div class="card-h mb-0">{t('wizard.where.rooms')}</div>
            <p class="text-xs text-slate-500">{t('wizard.where.roomsHint')}</p>
            <PathList bind:items={w.rooms} testid="wizard-rooms" />
            <div class="grid grid-cols-1 gap-3 md:grid-cols-3">
              <label><span class="lbl">{t('golden.host')}</span><input class="input mt-1" bind:value={w.host} placeholder="pc" data-testid="wizard-host" /></label>
              <div class="md:col-span-2"><span class="lbl">{t('golden.target')}</span><div class="mt-1"><PathField bind:value={w.target} testid="wizard-target" /></div></div>
            </div>
          {/if}
          <div class="flex items-center gap-3 border-t border-slate-100 pt-3">
            <button class="btn-primary" disabled={!dirs.length || scanning} onclick={runScan} data-testid="wizard-scan"><ScanSearch size={14} />{scanned ? t('wizard.rescan') : t('wizard.scan')}</button>
            <span class="text-xs text-slate-500">{scanning ? t('wizard.scanning') : t('wizard.scanHint')}</span>
          </div>
          {#if scanned}
            <div class="banner border-brand-200 bg-brand-50 text-sm text-brand-800" data-testid="wizard-scanned">
              <Info size={14} />{t(scan.truncated ? 'wizard.scannedPartial' : 'wizard.scanned', { secs: scan.secs.toFixed(1), n: num(scan.files), size: bytes(scan.bytes) })}
            </div>
            {#if w.goal === 'golden'}
              <div class="grid grid-cols-[12rem_1fr_6rem] items-center gap-2 text-sm">
                <span>{t('wizard.golden.in')}</span><div class="h-2 rounded bg-slate-100"><div class="h-2 rounded bg-brand-600" style="width:{Math.max(2, (considered.n / Math.max(1, scan.files)) * 100)}%"></div></div><span class="num">{num(considered.n)}</span>
              </div>
              {#if goldenOut.gifbmp + goldenOut.gfx}
                <div class="banner border-amber-200 bg-amber-50 text-xs text-amber-800" data-testid="wizard-golden-out"><CircleAlert size={14} />{t('wizard.golden.out', { gifbmp: num(goldenOut.gifbmp), gfx: num(goldenOut.gfx) })}</div>
              {/if}
            {:else}
              <div class="space-y-1">
                {#each FAMILIES as f (f.key)}
                  {@const n = famCount(f.key)}
                  <div class="grid grid-cols-[12rem_1fr_6rem] items-center gap-2 text-sm"><span>{t('fam.' + f.key)}</span><div class="h-2 rounded bg-slate-100"><div class="h-2 rounded bg-brand-600" style="width:{Math.max(1, (n / Math.max(1, scan.files)) * 100)}%"></div></div><span class="num">{num(n)}</span></div>
                {/each}
              </div>
            {/if}
            <p class="text-xs text-slate-500">{t('wizard.cases', { copies: num(scan.cases.copies || 0), dated: num(scan.cases.dated || 0), origin: num(scan.cases.origin || 0) })}</p>
          {/if}
        </div>

      {:else if cur === 'what'}
        {#if w.goal === 'golden'}
          <div class="card">
            <div class="card-h">{t('wizard.what.q')} <span class="pill bg-slate-100 text-slate-600">{t('wizard.fixed')}</span></div>
            <div class="rounded-lg border border-dashed border-slate-300 bg-slate-50 p-3 text-sm text-slate-600" data-testid="wizard-golden-locked">{@html t('wizard.what.golden')}</div>
          </div>
        {:else}
          <div class="card">
            <div class="card-h">{t('wizard.what.q')}</div>
            <p class="mb-2 text-xs text-slate-500">{t('wizard.what.hint')}</p>
            {#each FAMILIES as f (f.key)}
              <label class="chk border-t border-slate-100 py-2 first:border-0"><input type="checkbox" bind:checked={w.fams[f.key]} onchange={() => (w.famsAuto = false)} data-testid={'wfam-' + f.key} />
                <div class="flex-1"><b>{t('fam.' + f.key)}</b><div class="text-xs text-slate-500">{t('fam.' + f.key + '.ex')}</div></div>
                <span class="text-xs text-slate-500">{t('wizard.nFiles', { n: num(famCount(f.key)) })}</span>
              </label>
            {/each}
            <p class="mt-2 text-xs text-slate-500">{@html t('wizard.what.preset', { p: catalogPreset(w.fams) || '-' })}</p>
          </div>
        {/if}

      {:else if cur === 'layout'}
        {#if w.goal === 'golden'}
          <div class="card space-y-3">
            <div class="card-h mb-0">{t('wizard.layout.q')} <span class="pill bg-slate-100 text-slate-600">{t('wizard.canonical')}</span></div>
            <div class="rounded-lg border border-dashed border-slate-300 bg-slate-50 p-3 text-sm text-slate-600">{t('wizard.layout.golden')}</div>
            <label class="chk"><input type="checkbox" bind:checked={w.videoTier} />{t('golden.videoTier')}</label>
          </div>
        {:else}
          <div class="card space-y-3">
            <div class="card-h mb-0">{t('wizard.layout.q')}</div>
            <p class="text-xs text-slate-500">{t('wizard.layout.hint')}</p>
            {#each FAMILIES.filter((f) => w.fams[f.key]) as f (f.key)}
              <div class="rounded-lg border border-slate-200 p-3" data-testid={'wlayout-' + f.key}>
                <div class="mb-2 flex items-baseline gap-2"><b>{t('fam.' + f.key)}</b><span class="text-xs text-slate-500">{t('wizard.nFiles', { n: num(famCount(f.key)) })}</span></div>
                <div class="flex flex-wrap gap-1.5">
                  {#each f.chips as p, i (p)}
                    <button class="rounded-lg border px-2.5 py-1 text-sm {w.choice[f.key] === p ? 'border-brand-600 bg-brand-50 font-medium text-brand-700' : 'border-slate-200 hover:border-brand-200'}" onclick={() => (w.choice[f.key] = p)}>
                      {t('preset.' + p)}{#if i === 0}<span class="ml-1 text-[10px] text-emerald-600">{t('wizard.recommended')}</span>{/if}
                    </button>
                  {/each}
                  <select class="rounded-lg border border-slate-200 px-2 py-1 text-sm" value="" onchange={(e) => { if (e.currentTarget.value) w.choice[f.key] = e.currentTarget.value; }}>
                    <option value="">{t('wizard.otherSchemes')}</option>
                    {#each GENERIC.filter((p) => !f.chips.includes(p)) as p (p)}<option value={p} selected={w.choice[f.key] === p}>{t('preset.' + p)}</option>{/each}
                  </select>
                </div>
                {#each examples(f.key) as r (r.src)}<div class="mono mt-1 truncate text-slate-500">{r.src.split('/').pop()} <span class="text-slate-300">→</span> <span class="text-slate-800">{r.dst}</span></div>{/each}
              </div>
            {/each}
            <label class="chk"><input type="checkbox" bind:checked={w.clean} />{t('naming.clean')}</label>
            <div><span class="lbl">{t('naming.resulting')}</span><div class="input mt-1 bg-slate-50 font-mono text-xs break-all" data-testid="wizard-naming">{naming.naming}</div></div>
          </div>
        {/if}

      {:else if cur === 'dupes'}
        <div class="card space-y-3">
          <div class="card-h mb-0">{t('wizard.dupes.q.' + w.goal)}</div>
          <p class="text-sm text-slate-600">{t('wizard.dupes.' + w.goal)}</p>
          {#if w.goal === 'rearrange'}
            <label class="chk"><input type="radio" bind:group={w.dupes} value="delete" />{t('rearrange.dupesDelete')}</label>
            <label class="chk"><input type="radio" bind:group={w.dupes} value="report" />{t('rearrange.dupesReport')}</label>
            <label class="chk border-t border-slate-100 pt-2"><input type="checkbox" bind:checked={w.keepEmpty} />{t('rearrange.keepEmpty')}</label>
          {:else if w.goal === 'golden'}
            <label class="chk"><input type="checkbox" bind:checked={w.verifyFull} />{t('golden.verifyFull')}</label>
          {/if}
        </div>

      {:else if cur === 'excl'}
        <div class="card">
          <div class="card-h">{t('wizard.excl.q')}</div>
          <p class="mb-2 text-xs text-slate-500">{t('wizard.excl.hint')}</p>
          {#if w.goal === 'golden'}<div class="banner mb-2 border-amber-200 bg-amber-50 text-xs text-amber-800"><CircleAlert size={14} />{t('wizard.excl.golden')}</div>{/if}
          {#each RULES as r (r)}
            <label class="chk border-t border-slate-100 py-2 first:border-0"><input type="checkbox" bind:checked={w.rules[r]} data-testid={'wrule-' + r} />
              <div class="flex-1"><b>{t('rule.' + r)}</b><div class="text-xs text-slate-500">{t('rule.' + r + '.ex')}</div></div>
              <span class="text-xs text-slate-500">{t('wizard.nFiles', { n: num(scan?.rules?.[r] || 0) })}</span>
            </label>
          {/each}
          <label class="mt-3 block"><span class="lbl">{t('filters.exclude')}</span><input class="input mt-1" bind:value={w.exclude} placeholder="Backup WhatsApp" data-testid="wizard-exclude" /></label>
        </div>

      {:else}
        <div class="card space-y-3" data-testid="wizard-summary">
          <div class="card-h mb-0">{t('wizard.step.sum')}</div>
          <div class="grid grid-cols-[10rem_1fr] gap-x-3 gap-y-1 text-sm">
            <span class="text-slate-500">{t('wizard.sum.goal')}</span><b>{t('wizard.goal.' + w.goal)}</b>
            <span class="text-slate-500">{t('wizard.sum.files')}</span><span>{w.goal === 'golden' ? t('golden.scope') : FAMILIES.filter((f) => w.fams[f.key]).map((f) => t('fam.' + f.key)).join(', ')}</span>
            {#if w.goal !== 'dupes'}
              <span class="text-slate-500">{t('wizard.step.layout')}</span>
              <span>{w.goal === 'golden' ? t('wizard.canonical') + (w.videoTier ? ', ' + t('golden.videoTier').toLowerCase() : '') : FAMILIES.filter((f) => w.fams[f.key]).map((f) => t('fam.' + f.key) + ': ' + t('preset.' + w.choice[f.key]).toLowerCase()).join('; ')}</span>
            {/if}
            {#if w.goal === 'rearrange'}<span class="text-slate-500">{t('wizard.step.dupes')}</span><span>{t(w.dupes === 'delete' ? 'rearrange.dupesDelete' : 'rearrange.dupesReport')}</span>{/if}
            <span class="text-slate-500">{t('wizard.step.excl')}</span><span>{RULES.filter((r) => w.rules[r] !== false).map((r) => t('rule.' + r).toLowerCase()).join(', ') || '-'}{w.exclude ? ' + "' + w.exclude + '"' : ''}</span>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div class="mini text-center"><div class="mini-v" data-testid="wizard-considered">{num(considered.n)}</div><div class="mini-l">{t('wizard.sum.considered')}</div></div>
            <div class="mini text-center"><div class="mini-v">{bytes(considered.b)}</div><div class="mini-l">{t('common.size')}</div></div>
          </div>
          {#if w.exclude}<p class="text-xs text-slate-500">{t('wizard.sum.excludeNote')}</p>{/if}
          {#if w.goal === 'rearrange' && w.dupes === 'delete'}
            <div class="banner border-rose-200 bg-rose-50 text-xs text-rose-800"><CircleAlert size={14} />{t('wizard.sum.delete')}</div>
          {:else}
            <div class="banner border-brand-200 bg-brand-50 text-xs text-brand-800"><Info size={14} />{t('wizard.sum.safe.' + w.goal)}</div>
          {/if}
          <div>
            <span class="lbl">{t('wizard.sum.cli')}</span>
            <pre class="mt-1 rounded-lg bg-slate-900 p-3 font-mono text-xs break-all whitespace-pre-wrap text-slate-100" data-testid="wizard-cli">{cli}</pre>
            <button class="btn-ghost btn-sm mt-1" onclick={copyCli}><Copy size={12} />{t('wizard.copy')}</button>
          </div>
          <div>
            <span class="lbl">{t('wizard.sum.recipe')}</span>
            <div class="mt-1 flex gap-2"><input class="input" bind:value={recipeName} placeholder={t('wizard.recipePlaceholder')} data-testid="wizard-recipe-name" /><button class="btn-ghost" onclick={saveRecipe} data-testid="wizard-recipe-save"><Save size={14} />{t('wizard.save')}</button></div>
          </div>
        </div>
      {/if}

      <div class="flex items-center gap-3">
        {#if w.step > 0}<button class="btn-ghost" onclick={prev}><ArrowLeft size={14} />{t('wizard.back')}</button>{/if}
        <button class="link text-sm" onclick={() => (settings.view = w.goal === 'golden' ? 'golden' : w.goal === 'rearrange' ? 'rearrange' : 'merge')}>{t('wizard.fullForm')}</button>
        <span class="ml-auto"></span>
        {#if cur === 'sum'}
          <button class="btn-primary" disabled={!scanned} onclick={done} data-testid="wizard-finish">{t('wizard.finish.' + w.goal)}<ArrowRight size={14} /></button>
        {:else}
          <button class="btn-primary" disabled={!canNext} onclick={next} data-testid="wizard-next">{t('wizard.next')}<ArrowRight size={14} /></button>
        {/if}
      </div>
    </div>

    <aside class="card sticky top-0 space-y-2" data-testid="wizard-preview">
      <div class="card-h mb-0">{t('wizard.preview')}</div>
      {#if !w.goal || cur === 'goal'}
        <p class="text-xs text-slate-500">{t('wizard.previewHow')}</p>
        <p class="text-xs text-slate-500">{t('wizard.previewRegimes')}</p>
      {:else if !scanned}
        <p class="text-xs text-slate-500">{t('wizard.previewAfterScan')}</p>
      {:else}
        <p class="text-xs text-slate-500">{t('wizard.previewSample', { n: rows.length })}</p>
        {#if rowsErr}<div class="banner border-rose-200 bg-rose-50 text-xs text-rose-700">{rowsErr}</div>{/if}
        <ul class="scroll-thin max-h-[70vh] space-y-1.5 overflow-auto">
          {#each rows as r (r.src)}
            <li class="border-t border-slate-100 pt-1.5 first:border-0" data-testid="wizard-row">
              <div class="mono {r.skip || r.del ? 'text-slate-400 line-through' : 'text-slate-500'}">{r.src}</div>
              {#if r.dst}<div class="mono text-slate-900">→ {r.dst}</div>{/if}
              <div class="mt-0.5 flex flex-wrap gap-1">{#each r.tags as g, i (i)}<span class={TONE[g.k] || 'tag-slate'}>{tagText(g)}</span>{/each}</div>
            </li>
          {/each}
        </ul>
      {/if}
    </aside>
  </div>
</section>
