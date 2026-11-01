// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Exclusions after the preview: a previewed merge or rearrange job
// is re-planned at once on the server; otherwise only the form changes.
import { act, flash } from './store.svelte.js';
import { t } from './i18n.svelte.js';

// applyExclusions sets the exclusions of cfg (the form of a mode) and, when
// its job is a preview, re-plans the job. { paths, globs } are the new
// sets, contentOf a path whose content goes everywhere.
export async function applyExclusions(cfg, job, { paths, globs, contentOf = '' }) {
  if (job && job.status === 'previewed' && job.stage === 'preview' && !cfg.stale) {
    try {
      const j = await act(job.id, 'exclude', { paths, globs, contentOf });
      const f = j?.params?.filters || {};
      cfg.filters.excludePaths = f.excludePaths || [];
      cfg.filters.excludeGlobs = f.excludeGlobs || [];
      flash(t('plan.replanned', { n: j?.summary?.toCopy ?? j?.summary?.moves ?? 0 }), true);
      return;
    } catch (e) {
      if (!e.data?.needsPreview) {
        flash(e.message);
        return;
      }
      cfg.stale = true;
      flash(t('plan.needsPreview'));
    }
  }
  cfg.filters.excludePaths = paths;
  cfg.filters.excludeGlobs = globs;
}

// excludeOne adds one exclusion from a plan row: a path or a content.
export function excludeOne(cfg, job, v) {
  const paths = cfg.filters.excludePaths || [];
  const globs = cfg.filters.excludeGlobs || [];
  if (v.contentOf) return applyExclusions(cfg, job, { paths, globs, contentOf: v.contentOf });
  if (paths.includes(v.path)) return;
  return applyExclusions(cfg, job, { paths: [...paths, v.path], globs });
}
