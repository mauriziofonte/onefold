<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { Settings, Save, ScrollText, Info, Clock } from '@lucide/svelte';
  import { store, saveConfig, flash } from '../lib/store.svelte.js';
  import { t } from '../lib/i18n.svelte.js';

  // the form follows the saved config; Save writes it back (validated by the server)
  let form = $state({ artist: '', signature: '', language: '', timezone: '', dateOrder: '', namesLanguage: '', photoDateFallback: false, keepAwake: true, lowPriority: false, diagLog: false });
  // IANA zones the browser knows, offered as suggestions
  const zones = typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : [];
  let saved = $state(false);
  const c = $derived(store.config);
  $effect(() => {
    const v = c?.config;
    if (v) form = { artist: v.artist || '', signature: v.signature || '', language: v.language || '', timezone: v.timezone || '', dateOrder: v.dateOrder || '', namesLanguage: v.namesLanguage || '', photoDateFallback: !!v.photoDateFallback, keepAwake: !v.noKeepAwake, lowPriority: !!v.lowPriority, diagLog: !!v.diagLog };
  });

  async function save() {
    saved = false;
    try {
      await saveConfig({ artist: form.artist.trim(), signature: form.signature.trim(), language: form.language.trim().toLowerCase(),
        timezone: form.timezone.trim(), dateOrder: form.dateOrder, namesLanguage: form.namesLanguage, photoDateFallback: form.photoDateFallback, noKeepAwake: !form.keepAwake, lowPriority: form.lowPriority, diagLog: form.diagLog });
      saved = true;
    } catch (e) {
      flash(e.message);
    }
  }
</script>

<section class="mx-auto max-w-4xl space-y-5">
  <div>
    <h1 class="text-xl font-semibold text-slate-900">{t('config.title')}</h1>
    <p class="text-slate-500">{t('config.desc')}</p>
  </div>
  {#if c}
    <form class="card space-y-4" onsubmit={(e) => { e.preventDefault(); save(); }} data-testid="config-form">
      <div class="card-h"><Settings size={16} />{t('config.silver')}</div>
      <label class="block"><span class="lbl">{t('config.artist')}</span>
        <input class="input mt-1" bind:value={form.artist} placeholder={t('config.unset')} oninput={() => (saved = false)} data-testid="config-artist" /></label>
      <p class="hint">{t('config.artistHint')}</p>
      <label class="block"><span class="lbl">{t('config.signature')}</span>
        <input class="input mt-1" bind:value={form.signature} placeholder={c.defaultSignature} oninput={() => (saved = false)} data-testid="config-signature" /></label>
      <p class="hint">{t('config.signatureHint', { def: c.defaultSignature, none: c.none })}</p>
      <label class="block w-40"><span class="lbl">{t('config.language')}</span>
        <input class="input mt-1" bind:value={form.language} placeholder={t('config.unset')} maxlength="3" oninput={() => (saved = false)} data-testid="config-language" /></label>
      <p class="hint">{t('config.languageHint')}</p>
      <div class="border-t border-slate-100 pt-4 space-y-4">
        <div class="card-h"><Clock size={16} />{t('config.time')}</div>
        <label class="block w-72"><span class="lbl">{t('config.timezone')}</span>
          <input class="input mt-1" list="config-zones" bind:value={form.timezone} placeholder={c.zone?.name || t('config.unset')} oninput={() => (saved = false)} data-testid="config-timezone" /></label>
        <datalist id="config-zones">{#each zones as z}<option value={z}></option>{/each}</datalist>
        <p class="hint" data-testid="config-zone-info">{#if c.zone?.error}<span class="text-rose-700">{c.zone.error}</span>{:else}{t('config.timezoneHint', { zone: c.zone?.name || '', source: t('config.zoneSource.' + (c.zone?.source || 'host')) })}{/if}</p>
        <label class="block w-72"><span class="lbl">{t('config.dateOrder')}</span>
          <select class="input mt-1" bind:value={form.dateOrder} onchange={() => (saved = false)} data-testid="config-date-order">
            <option value="">{t('config.dateOrder.none')}</option><option value="dmy">{t('config.dateOrder.dmy')}</option><option value="mdy">{t('config.dateOrder.mdy')}</option>
          </select></label>
        <p class="hint">{t('config.dateOrderHint')}</p>
        <label class="block w-72"><span class="lbl">{t('config.namesLanguage')}</span>
          <select class="input mt-1" bind:value={form.namesLanguage} onchange={() => (saved = false)} data-testid="config-names-language">
            <option value="">{t('config.namesLanguage.default')}</option><option value="en">English</option><option value="it">Italiano</option>
          </select></label>
        <p class="hint">{t('config.namesLanguageHint')}</p>
        <label class="chk"><input type="checkbox" bind:checked={form.photoDateFallback} onchange={() => (saved = false)} data-testid="config-photo-date-fallback" />{t('config.photoDateFallback')}</label>
        <p class="hint">{t('config.photoDateFallbackHint')}</p>
        <label class="chk"><input type="checkbox" bind:checked={form.keepAwake} onchange={() => (saved = false)} data-testid="config-keep-awake" />{t('config.keepAwake')}</label>
        <p class="hint">{t('config.keepAwakeHint')}</p>
        <label class="chk"><input type="checkbox" bind:checked={form.lowPriority} onchange={() => (saved = false)} data-testid="config-low-priority" />{t('config.lowPriority')}</label>
        <p class="hint">{t('config.lowPriorityHint')}</p>
      </div>
      {#if c.diagSupported}
        <div class="border-t border-slate-100 pt-4">
          <div class="card-h"><ScrollText size={16} />{t('config.diag')}</div>
          <label class="chk mt-2"><input type="checkbox" bind:checked={form.diagLog} onchange={() => (saved = false)} data-testid="config-diag" />{t('config.diagLabel')}</label>
          <p class="hint">{t('config.diagHint')}</p>
        </div>
      {/if}
      <div class="flex items-center gap-3">
        <button class="btn-primary" type="submit" data-testid="config-save"><Save size={14} />{t('config.save')}</button>
        {#if saved}<span class="text-sm text-emerald-700" data-testid="config-saved">{t('config.saved')}</span>{/if}
      </div>
      <p class="hint flex items-start gap-1.5"><Info size={14} class="mt-0.5 shrink-0" />{t('config.where', { path: c.path })}</p>
    </form>
    <div class="card space-y-1 text-sm text-slate-600">
      <div class="card-h"><Info size={16} />{t('config.about')}</div>
      <p>onefold {store.info?.version} · {t('config.license')}</p>
    </div>
  {/if}
</section>
