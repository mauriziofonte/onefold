#!/usr/bin/env bash
# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# Generates testdata/media-synth: tiny synthetic photos and videos (solid
# colours, made-up devices and dates) that pin the media naming and winner
# rules in public CI (internal/plan TestMediaSynthRegression). Run once; the
# output is committed and the test reads it with the pure-Go extractors only,
# so CI needs neither ffmpeg nor exiftool. Regenerating changes the bytes:
# update the expectations of the test in the same change.
#
# Usage: scripts/gen-media-synth.sh [OUT]   (default testdata/media-synth, must not exist)
set -euo pipefail

out="${1-testdata/media-synth}"
if [ -e "$out" ]; then
    echo "$out exists: remove it first" >&2
    exit 1
fi
for t in ffmpeg exiftool; do
    command -v "$t" >/dev/null || { echo "$t missing" >&2; exit 1; }
done
mkdir -p "$out/A" "$out/B"

# jpg FILE COLOUR WxH: one-frame image
img() {
    local f="$1" c="$2" s="$3"
    ffmpeg -loglevel error -f lavfi -i "color=c=${c}:s=${s}" -frames:v 1 -bitexact "$f"
}
# tag FILE ARGS...: exif tags, no backup file
tag() {
    local f="$1"
    shift
    exiftool -q -overwrite_original "$@" "$f"
}
# vid FILE COLOUR WxH SECONDS UTC-TIME: H.264 mp4 with a creation time
vid() {
    local f="$1" c="$2" s="$3" d="$4" t="$5"
    ffmpeg -loglevel error -f lavfi -i "color=c=${c}:s=${s}:d=${d}:r=2" -c:v libx264 -preset ultrafast -crf 51 \
        -pix_fmt yuv420p -metadata creation_time="$t" -bitexact -fflags +bitexact "$f"
}

phone=(-Make=Apple -Model="iPhone 12")
cam=(-Make=NIKON -Model="NIKON D3500")
when=(-DateTimeOriginal="2021:05:06 07:08:09")

# without a sub-second a photo is identified by its bytes: a lower resolution
# of the same shot is its own asset (same name, collision suffix)
img "$out/A/IMG_0001.jpg" red 64x48
tag "$out/A/IMG_0001.jpg" "${phone[@]}" "${when[@]}"
cp "$out/A/IMG_0001.jpg" "$out/B/IMG_0001 (1).jpg"
img "$out/B/IMG_0001_small.jpg" red 32x24
tag "$out/B/IMG_0001_small.jpg" "${phone[@]}" "${when[@]}"
# a second phone, same second, same prefix: same name, collision suffix
img "$out/B/IMG_9001.jpg" green 64x48
tag "$out/B/IMG_9001.jpg" -Make=Samsung -Model="SM-G991B" "${when[@]}"

# with a sub-second the shot is one cluster: largest resolution wins
sub=(-DateTimeOriginal="2021:06:07 10:11:12" -SubSecTimeOriginal=30)
img "$out/A/IMG_0010.jpg" red 64x48
tag "$out/A/IMG_0010.jpg" "${phone[@]}" "${sub[@]}"
img "$out/B/IMG_0010_small.jpg" red 32x24
tag "$out/B/IMG_0010_small.jpg" "${phone[@]}" "${sub[@]}"
# an edit of that shot: its own asset (SNPSD prefix)
img "$out/A/IMG_0010_edit.jpg" pink 64x48
tag "$out/A/IMG_0010_edit.jpg" "${phone[@]}" "${sub[@]}" -Software="Snapseed 2.0"
# another device in that sub-second: its own asset (DSC prefix)
img "$out/B/DSC_0010.jpg" blue 64x48
tag "$out/B/DSC_0010.jpg" "${cam[@]}" "${sub[@]}"
# equal resolution: the HDR output wins
hdr=(-DateTimeOriginal="2021:06:07 10:11:13" -SubSecTimeOriginal=40)
img "$out/A/IMG_0011_HDR.jpg" orange 64x48
tag "$out/A/IMG_0011_HDR.jpg" "${phone[@]}" "${hdr[@]}"
img "$out/B/IMG_0011.jpg" yellow 64x48
tag "$out/B/IMG_0011.jpg" "${phone[@]}" "${hdr[@]}"
# equal resolution, no HDR: the larger file wins
big=(-DateTimeOriginal="2021:06:07 10:11:14" -SubSecTimeOriginal=60)
img "$out/A/IMG_0012.jpg" white 64x48
tag "$out/A/IMG_0012.jpg" "${phone[@]}" "${big[@]}" -ImageDescription="a longer description makes this file larger"
img "$out/B/IMG_0012.jpg" white 64x48
tag "$out/B/IMG_0012.jpg" "${phone[@]}" "${big[@]}"

# burst: one second, two sub-seconds
img "$out/A/IMG_0003.jpg" navy 64x48
tag "$out/A/IMG_0003.jpg" "${cam[@]}" -DateTimeOriginal="2022:01:02 03:04:05" -SubSecTimeOriginal=10
img "$out/A/IMG_0004.jpg" teal 64x48
tag "$out/A/IMG_0004.jpg" "${cam[@]}" -DateTimeOriginal="2022:01:02 03:04:05" -SubSecTimeOriginal=50
# one shot in two formats (JPG + TIFF): two assets, one per format
img "$out/A/DSC_0005.jpg" olive 64x48
tag "$out/A/DSC_0005.jpg" "${cam[@]}" -DateTimeOriginal="2022:01:02 03:04:06" -SubSecTimeOriginal=20
img "$out/A/DSC_0005.tif" olive 64x48
tag "$out/A/DSC_0005.tif" "${cam[@]}" -DateTimeOriginal="2022:01:02 03:04:06" -SubSecTimeOriginal=20
# no time at all, and a time from the file name only
img "$out/B/undated.png" purple 16x16
img "$out/B/IMG_20190102_030405.jpg" maroon 16x16
# a PNG saved as .jpg next to the same bytes as .png: two assets
img "$out/A/logo.png" silver 16x16
cp "$out/A/logo.png" "$out/B/logo.jpg"

# videos: same start, same length, two resolutions; same start, other length
vid "$out/A/VID_0001.mp4" red 1280x720 2 2023-07-08T09:10:11Z
vid "$out/B/VID_0001_lq.mp4" red 640x360 2 2023-07-08T09:10:11Z
vid "$out/B/VID_0002.mp4" blue 640x360 6 2023-07-08T09:10:11Z
# a 4K clip without a twin
vid "$out/A/VID_0003.mp4" green 3840x2160 1 2024-02-03T04:05:06Z

touch -d "2020-01-01 00:00:00 UTC" "$out"/A/* "$out"/B/*
echo "generated $(find "$out" -type f | wc -l) files in $out"
