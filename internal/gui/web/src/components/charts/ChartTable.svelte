<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  // The numbers of a chart as a table, folded under it: what a screen reader
  // reads instead of the shapes, and the exact values for everyone. The first
  // column names the row, the others are numbers.
  import { t } from '../../lib/i18n.svelte.js';

  let { head = [], rows = [], testid = 'chart-table' } = $props();
</script>

{#if rows.length}
  <details class="mt-2 w-full text-xs" data-testid={testid}>
    <summary class="cursor-pointer text-brand-700 select-none">{t('chart.table')}</summary>
    <div class="mt-1 overflow-hidden rounded-lg border border-slate-200">
      <table class="tbl">
        <thead>
          <tr>
            {#each head as h, i (i)}<th scope="col" class={i ? 'text-right' : ''}>{h}</th>{/each}
          </tr>
        </thead>
        <tbody>
          {#each rows as r, j (j)}
            <tr>
              {#each r as c, i (i)}
                <td class={i ? 'num' : 'break-all text-slate-700'}>{c}</td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  </details>
{/if}
