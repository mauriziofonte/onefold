<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  let { label, value, sub = '', tone = '', ring = '', testid = '' } = $props();
</script>

<div class="stat {ring ? 'ring-2 ' + ring : ''}" data-testid={testid}>
  <div class="stat-l">{label}</div>
  <div class="stat-v {tone}">{value}</div>
  {#if sub}<div class="stat-s">{sub}</div>{/if}
</div>
