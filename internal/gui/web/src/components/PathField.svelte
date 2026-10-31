<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import { FolderOpen } from '@lucide/svelte';
  import { t } from '../lib/i18n.svelte.js';
  import { pickFolder } from '../lib/dialogs.svelte.js';

  let { value = $bindable(''), placeholder = '', testid = '' } = $props();

  async function browse() {
    const p = await pickFolder(value);
    if (p) value = p;
  }
</script>

<div class="flex gap-2">
  <input class="input flex-1 font-mono" bind:value placeholder={placeholder || t('common.folderPlaceholder')} data-testid={testid} />
  <button class="btn-ghost" onclick={browse}><FolderOpen size={14} />{t('common.browse')}</button>
</div>
