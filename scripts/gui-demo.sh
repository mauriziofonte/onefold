#!/usr/bin/env bash
# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# Records the README demo of the GUI (internal/gui/web/demo: screenshots of the
# real binary on a made-up sandbox) and assembles assets/demo/gui-merge.gif.
# Needs Node (internal/gui/web), the browser of playwright.demo.config.js and
# ffmpeg.
#
# Usage: scripts/gui-demo.sh   (DEMO_ROOT, default /tmp/onefold-demo, must not exist)
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
export ONEFOLD_BIN="$work/onefold"
export DEMO_OUT="$work/video"
(cd "$repo" && go build -o "$ONEFOLD_BIN" ./cmd/onefold)
(cd "$repo/internal/gui/web" && npx playwright test -c playwright.demo.config.js)
frames="$DEMO_OUT/frames"
if ! ls "$frames"/f00000.png >/dev/null 2>&1; then
    echo "no frames recorded" >&2
    exit 1
fi
video="$work/demo.mp4"
# frames were taken about every 200 ms: 5 per second of the run
ffmpeg -loglevel error -y -framerate 5 -i "$frames/f%05d.png" -pix_fmt yuv420p -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2" "$video"
mkdir -p "$repo/assets/demo"
# two-pass palette: small file, clean colours
ffmpeg -loglevel error -y -i "$video" -vf "fps=8,scale=960:-1:flags=lanczos,palettegen=stats_mode=diff" "$work/palette.png"
ffmpeg -loglevel error -y -i "$video" -i "$work/palette.png" \
    -lavfi "fps=8,scale=960:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" \
    "$repo/assets/demo/gui-merge.gif"
echo "assets/demo/gui-merge.gif: $(du -h "$repo/assets/demo/gui-merge.gif" | cut -f1)"
echo "scratch: $work and ${DEMO_ROOT:-/tmp/onefold-demo} (remove them)"
