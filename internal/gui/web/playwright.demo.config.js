// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

import { defineConfig } from '@playwright/test';
import { existsSync } from 'node:fs';

// The README demo of the GUI (scripts/gui-demo.sh): the real binary on a
// made-up sandbox, recorded as screenshots, converted to a GIF by the script.
// The browser: DEMO_CHROME, else a chrome-headless-shell installed system-wide,
// else the Chromium of `npx playwright install chromium`.
const chrome = process.env.DEMO_CHROME
  || (existsSync('/usr/local/bin/chrome-headless-shell') ? '/usr/local/bin/chrome-headless-shell' : undefined);
export default defineConfig({
  testDir: './demo',
  timeout: 5 * 60 * 1000,
  expect: { timeout: 60 * 1000 },
  workers: 1,
  reporter: [['list']],
  outputDir: process.env.DEMO_OUT || 'demo-results',
  use: {
    viewport: { width: 1280, height: 760 },
    launchOptions: chrome ? { executablePath: chrome } : {},
  },
});
