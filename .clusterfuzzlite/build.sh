#!/bin/bash -eu
# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# Builds every native Go fuzz target as a libFuzzer binary. Each target lives
# in a fuzz_test.go (journal_test.go for rearrange) that compiles alone: the
# builder turns that one file into a normal source with a testing shim, so
# it must not use helpers of the package's other test files.

# The testing shim, at the version the image's go-118-fuzz-build was built
# with (go.mod of the container only; nothing is committed).
go get github.com/AdamKorcz/go-118-fuzz-build/testing@v0.0.0-20250520111509-a70c2aa677fa

m=github.com/mauriziofonte/onefold/internal
while read -r pkg fn; do
  [ -n "$pkg" ] || continue
  compile_native_go_fuzzer "$m/$pkg" "$fn" "${pkg}_${fn}"
done <<'TARGETS'
meta FuzzExifTIFF
meta FuzzISOBMFF
meta FuzzMatroska
meta FuzzFLV
meta FuzzRIFF
meta FuzzRAR
meta Fuzz7z
meta FuzzZip
meta FuzzISO9660
meta FuzzOLE2
meta FuzzPDF
meta FuzzRTF
meta FuzzJPEG
textexport FuzzDocText
textexport FuzzXLSText
textexport FuzzPPTText
textexport FuzzRTFText
textexport FuzzHTMLText
catalog FuzzReadManifest
plan FuzzReadPlan
rearrange FuzzReadJournal
TARGETS
