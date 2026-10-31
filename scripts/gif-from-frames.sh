#!/usr/bin/env bash
# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# Assembles the screenshots of a GUI demo run (f00000.png, f00001.png, ...
# taken about every 200 ms) into a README GIF. Needs ffmpeg.
#
# Usage: scripts/gif-from-frames.sh FRAMES_DIR OUT.gif
set -euo pipefail

frames=${1:?usage: gif-from-frames.sh FRAMES_DIR OUT.gif}
out=${2:?usage: gif-from-frames.sh FRAMES_DIR OUT.gif}
if [ ! -f "$frames/f00000.png" ]; then
    echo "$frames: no frames recorded" >&2
    exit 1
fi
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
video="$work/demo.mp4"
# about 5 frames per second of the run
ffmpeg -loglevel error -y -framerate 5 -i "$frames/f%05d.png" -pix_fmt yuv420p -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2" "$video"
mkdir -p "$(dirname "$out")"
# two-pass palette: small file, clean colours
ffmpeg -loglevel error -y -i "$video" -vf "fps=8,scale=960:-1:flags=lanczos,palettegen=stats_mode=diff" "$work/palette.png"
ffmpeg -loglevel error -y -i "$video" -i "$work/palette.png" \
    -lavfi "fps=8,scale=960:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" \
    "$out"
echo "$out: $(du -h "$out" | cut -f1)"
