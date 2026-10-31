// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

// The build lands in dist/, which the Go binary embeds (internal/gui/embed.go);
// dist/THIRD_PARTY_LICENSES.md lists the licenses of the bundled packages
// (`make notices` adds it to the release notices).
export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  build: { outDir: 'dist', emptyOutDir: true, assetsInlineLimit: 0, chunkSizeWarningLimit: 1500, license: { fileName: 'THIRD_PARTY_LICENSES.md' } },
  server: { proxy: { '/api': 'http://127.0.0.1:47115' } },
});
