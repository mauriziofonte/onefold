# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# ClusterFuzzLite builder: the OSS-Fuzz Go image (Go and libFuzzer), pinned
# by digest; Dependabot proposes its updates.
FROM gcr.io/oss-fuzz-base/base-builder-go:v1@sha256:0ebd94f573c7917b93a7edc8cd1db21c167adbd1d9c59943c3ebe4afb7aa4497
COPY . $SRC/onefold
WORKDIR $SRC/onefold
COPY .clusterfuzzlite/build.sh $SRC/
