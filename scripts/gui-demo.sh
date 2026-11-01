#!/usr/bin/env bash
# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# Records the README demo of the GUI (internal/gui/web/demo: screenshots of the
# real binary on a made-up sandbox) and assembles it into a GIF with
# scripts/gif-from-frames.sh. Needs Node (internal/gui/web, `npm ci` done),
# a Playwright Chromium (or DEMO_CHROME) and ffmpeg. Runs on Linux, macOS and
# Windows (Git Bash); without ffmpeg, FRAMES_ONLY=1 keeps the screenshots.
# The binary carries the version of VERSION (v0.0.2-alpha, not dev or
# +dev.<commit>): the GIF shows the release the README describes.
#
# Usage: scripts/gui-demo.sh [OUT.gif]   (default assets/demo/gui-merge.gif;
# DEMO_ROOT, default onefold-demo in the temp folder, must not exist)
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
out="${1:-$repo/assets/demo/gui-merge.gif}"
work="$(mktemp -d)"
if command -v cygpath >/dev/null; then
    # Git Bash: Go, Node and ffmpeg take C:/... paths, not /tmp/...
    work="$(cygpath -m "$work")"
fi
export ONEFOLD_BIN="$work/onefold$(cd "$repo" && go env GOEXE)"
export DEMO_OUT="${DEMO_OUT:-$work/video}"
(cd "$repo" && go build -ldflags "-X main.version=v$(cat VERSION)" -o "$ONEFOLD_BIN" ./cmd/onefold)
(cd "$repo/internal/gui/web" && npx playwright test -c playwright.demo.config.js)
if [ "${FRAMES_ONLY:-}" = 1 ]; then
    echo "frames: $DEMO_OUT/frames"
    exit 0
fi
"$repo/scripts/gif-from-frames.sh" "$DEMO_OUT/frames" "$out"
echo "scratch: $work and the DEMO_ROOT folder (remove them)"
