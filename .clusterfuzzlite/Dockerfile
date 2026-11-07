# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# ClusterFuzzLite builder: the OSS-Fuzz Go image (Go and libFuzzer), pinned
# by digest; Dependabot proposes its updates.
FROM gcr.io/oss-fuzz-base/base-builder-go:v1@sha256:1dbadefbb28c6bfd796c64d9c2115977deb8f19494533cc5a3b724c3441df23d
COPY . $SRC/onefold
WORKDIR $SRC/onefold
COPY .clusterfuzzlite/build.sh $SRC/
