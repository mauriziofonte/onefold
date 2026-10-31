#!/usr/bin/env bash
# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# Builds the made-up folders the CLI demo tapes work on, under $1 (wiped
# first): an old laptop and a phone backup that share some photos and
# documents under other names, with fixed dates. No personal data: every
# photo is a generated color pattern, every document a line of made-up text.
# Needs ffmpeg (pictures) and exiftool (capture dates).
set -euo pipefail

root=${1:?usage: sandbox.sh DIR}
rm -rf -- "$root"
mkdir -p "$root"
cd "$root"

photo() { # photo PATH "YYYY:MM:DD HH:MM:SS" COLOR
    mkdir -p "$(dirname "$1")"
    ffmpeg -loglevel error -f lavfi -i "color=c=$3:s=320x240,drawgrid=w=40:h=40:c=white@0.4" -frames:v 1 -y "$1"
    if command -v exiftool >/dev/null; then exiftool -q -overwrite_original -Model="Demo Phone" -Make="Onefold" "-DateTimeOriginal=$2" "$1"; fi
    local day=${2%% *}
    touch -d "${day//:/-} ${2##* }" "$1"
}
doc() { # doc PATH TEXT DATE
    mkdir -p "$(dirname "$1")"
    printf '%s\n' "$2" > "$1"
    touch -d "$3" "$1"
}

photo laptop/Pictures/Holidays/IMG_0412.jpg "2021:08:14 10:21:05" steelblue
photo laptop/Pictures/Holidays/IMG_0413.jpg "2021:08:14 10:22:40" seagreen
photo laptop/Pictures/Birthday/DSC_1001.jpg "2022:03:02 18:05:11" darkorange
photo phone-backup/DCIM/Camera/IMG_20210814_102105.jpg "2021:08:14 10:21:05" steelblue
photo phone-backup/DCIM/Camera/IMG_20230105_090000.jpg "2023:01:05 09:00:00" slateblue
cp -p laptop/Pictures/Holidays/IMG_0413.jpg "phone-backup/DCIM/Camera/IMG_0413 (1).jpg"

doc laptop/Documents/taxes-2022.txt "Made-up tax notes for the demo." 2022-04-10
doc laptop/Documents/recipes/pasta.txt "Boil water, add salt, cook pasta." 2020-11-02
doc "phone-backup/Download/taxes-2022 (copy).txt" "Made-up tax notes for the demo." 2022-04-10
doc phone-backup/Download/shopping.txt "Milk, bread, coffee." 2023-02-01
