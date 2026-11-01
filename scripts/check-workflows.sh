#!/usr/bin/env bash
# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# Checks the GitHub workflows before they reach GitHub:
#   1. actionlint (with shellcheck) and zizmor (pedantic) on .github/
#   2. act runs, on a clean clone of HEAD (only what is committed), of the CI
#      (Linux job) and of the release triggered by TAG (publishing simulated),
#      plus a negative run: a tag on a commit outside main must stop the release.
#
# Usage: scripts/check-workflows.sh [TAG]   (default: v<VERSION>, the only tag the release accepts)
# Needs: actionlint, act, docker; shellcheck and zizmor, or uvx to run them.
# Override tool paths with ACTIONLINT, SHELLCHECK, ACT. KEEP=1 keeps the work dir.
#
# act cannot run everything GitHub runs; the clone gets these adaptations, and
# only the clone: `uses: $/...` (self-repository form) becomes `./...`, the
# artifact actions use v4 (act's artifact server rejects the newer protocol),
# the Windows matrix entry is left out (no Windows containers).
set -euo pipefail

root=$(git rev-parse --show-toplevel)
cd "$root"
ACTIONLINT=${ACTIONLINT:-actionlint}
ACT=${ACT:-act}
IMAGE=${ACT_IMAGE:-catthehacker/ubuntu:act-24.04}

if [ -n "${SHELLCHECK:-}" ]; then
    shellcheck_cmd=$SHELLCHECK
elif command -v shellcheck >/dev/null; then
    shellcheck_cmd=shellcheck
else
    shellcheck_cmd=$(uvx --quiet --from shellcheck-py python -c 'import shutil; print(shutil.which("shellcheck"))')
fi
zizmor() { if type -P zizmor >/dev/null; then command zizmor "$@"; else uvx --quiet zizmor "$@"; fi; }

step() { printf '\n== %s\n' "$*"; }

step "actionlint"
"$ACTIONLINT" -shellcheck "$shellcheck_cmd" -no-color

step "zizmor (pedantic)"
if [ -n "${GH_TOKEN:-}" ]; then zizmor --persona=pedantic .github/; else zizmor --offline --persona=pedantic .github/; fi

tag=${1:-v$(cat VERSION)} # the release workflow accepts only v<VERSION>
work=$(mktemp -d "${TMPDIR:-/tmp}/onefold-workflows.XXXXXX")
cleanup() { if [ -z "${KEEP:-}" ]; then rm -rf -- "$work"; else echo "work dir kept: $work"; fi; }
trap cleanup EXIT

step "clean clone of HEAD, tag $tag"
git clone -q --no-local "$root" "$work/repo"
cd "$work/repo"
git config user.name "workflow check"
git config user.email "workflow-check@localhost"
sed -i \
    -e 's|uses: \$/\.github/|uses: ./.github/|' \
    -e 's|actions/upload-artifact@[0-9a-f]\{40\}.*$|actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02 # v4.6.2 (act)|' \
    -e 's|actions/download-artifact@[0-9a-f]\{40\}.*$|actions/download-artifact@d3f86a106a0bac45b974a628896c90dbdf5c8093 # v4.3.0 (act)|' \
    .github/workflows/release.yml
git commit -qam "act adaptations"
git tag -f -a "$tag" -m "workflow check" >/dev/null
git update-ref refs/remotes/origin/main HEAD

event() { printf '{"ref":"refs/tags/%s","ref_name":"%s","repository":{"full_name":"local/onefold","name":"onefold","owner":{"login":"local"}}}\n' "$1" "$1" > "$work/$1.json"; echo "$work/$1.json"; }
run_act() {
    local log=$1; shift
    if "$ACT" "$@" -P "ubuntu-24.04=$IMAGE" --container-architecture linux/amd64 --matrix os:ubuntu-24.04 \
        --artifact-server-path "$work/artifacts" >"$log" 2>&1; then rc=0; else rc=$?; fi
    grep -E 'Success - Main|Failure - |Job (succeeded|failed)|::(error|warning)' "$log" | sed 's/^\[[^]]*\] *//' || true
    return "$rc"
}

step "act: ci"
run_act "$work/ci.log" push -W .github/workflows/ci.yml

step "act: release $tag (publishing simulated)"
run_act "$work/release.log" push -W .github/workflows/release.yml -e "$(event "$tag")"

step "act: a tag outside main must stop the release"
git checkout -q -b off-main
echo >> README.md
git commit -qam "off main"
git tag -a v999.0.0 -m "off main"
if run_act "$work/off-main.log" push -W .github/workflows/release.yml -j build -e "$(event v999.0.0)"; then
    echo "FAIL: the release accepted a tag outside main"; exit 1
fi
grep -q 'Failure - Main the tag names a commit of main' "$work/off-main.log" || { echo "FAIL: stopped for another reason"; exit 1; }

step "all workflow checks passed"
