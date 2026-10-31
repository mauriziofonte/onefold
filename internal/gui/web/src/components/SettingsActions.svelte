<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { Download, Terminal } from '@lucide/svelte';
  import { api } from '../lib/api.js';
  import { flash } from '../lib/store.svelte.js';
  import { t } from '../lib/i18n.svelte.js';

  // The settings of a form as a file for `onefold <kind> -settings FILE`, and
  // the equivalent command line. params: () => job params.
  let { kind, params } = $props();

  function exportSettings() {
    const blob = new Blob([JSON.stringify({ kind, ...params() }, null, 2) + '\n'], { type: 'application/json' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `onefold-${kind}.json`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }

  async function copyCommand() {
    try {
      const r = await api.post('/api/cli', { kind, params: params() });
      await navigator.clipboard.writeText(r.command);
      flash(t('settings.commandCopied'), true);
    } catch (e) {
      flash(e.message);
    }
  }
</script>

<div class="ml-auto flex gap-2">
  <button class="btn-ghost btn-sm" onclick={exportSettings} data-testid={kind + '-export'} title={t('settings.exportHint')}><Download size={12} />{t('settings.export')}</button>
  <button class="btn-ghost btn-sm" onclick={copyCommand} data-testid={kind + '-command'}><Terminal size={12} />{t('settings.command')}</button>
</div>
