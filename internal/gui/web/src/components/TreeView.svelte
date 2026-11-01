<!--
  onefold
  Author: Maurizio Fonte <fonte.maurizio@gmail.com>
  Copyright (c) 2026 Maurizio Fonte
  SPDX-License-Identifier: AGPL-3.0-or-later
-->

<script>
  import TreeBranch from './TreeBranch.svelte';

  // The plan list grouped by source folder: folders with file count
  // and size, opened on demand; a branch can be excluded at once.
  let props = $props();
</script>

<div class="py-1" data-testid="tree-view">
  <TreeBranch {...props} />
</div>
