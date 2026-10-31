#!/usr/bin/env bash
# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# Renders the CLI demo GIFs of the README (assets/demo/cli-*.gif) with vhs:
# builds onefold from this checkout, makes the made-up sandbox of sandbox.sh
# in a temp folder, and runs every tape there with that onefold on PATH.
# Needs vhs (with ttyd), ffmpeg, exiftool. In CI: .github/workflows/demo.yml.
#
# Usage: scripts/tapes/render.sh [TAPE...]   (default: every scripts/tapes/*.tape)
set -euo pipefail

root=$(git rev-parse --show-toplevel)
cd "$root"
work=$(mktemp -d "${TMPDIR:-/tmp}/onefold-tapes.XXXXXX")
trap 'rm -rf -- "$work"' EXIT

mkdir -p "$work/bin" assets/demo
CGO_ENABLED=0 go build -o "$work/bin/onefold" ./cmd/onefold
export PATH="$work/bin:$PATH" ONEFOLD_STATE_DIR="$work/state" TZ=UTC

tapes=("$@")
[ ${#tapes[@]} -gt 0 ] || tapes=(scripts/tapes/*.tape)
for tape in "${tapes[@]}"; do
    out=$(awk '$1 == "Output" {print $2; exit}' "$tape")
    echo "== $tape -> $out"
    scripts/tapes/sandbox.sh "$work/sandbox"
    # vhs writes Output relative to its working directory: run it in the
    # sandbox, then move the GIF into the checkout
    (cd "$work/sandbox" && mkdir -p "$(dirname "$out")" && vhs "$root/$tape")
    mv "$work/sandbox/$out" "$root/$out"
    ls -l "$root/$out"
done
