// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineConfig } from '@playwright/test';
import { existsSync } from 'node:fs';

// End-to-end tests of the GUI against the real onefold binary (make web-e2e).
// The browser: E2E_CHROME, else a chrome-headless-shell installed system-wide,
// else the Chromium of `npx playwright install chromium`.
const chrome = process.env.E2E_CHROME
  || (existsSync('/usr/local/bin/chrome-headless-shell') ? '/usr/local/bin/chrome-headless-shell' : undefined);
export default defineConfig({
  testDir: './e2e',
  timeout: 15 * 60 * 1000,
  expect: { timeout: 60 * 1000 },
  workers: 1,
  reporter: [['list']],
  use: {
    viewport: { width: 1440, height: 950 },
    launchOptions: chrome ? { executablePath: chrome } : {},
  },
});
