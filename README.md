# onefold

[![ci](https://github.com/mauriziofonte/onefold/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/mauriziofonte/onefold/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/mauriziofonte/onefold?include_prereleases&sort=semver)](https://github.com/mauriziofonte/onefold/releases)
[![license: AGPL-3.0-or-later](https://img.shields.io/badge/license-AGPL--3.0--or--later-blue)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/mauriziofonte/onefold.svg)](https://pkg.go.dev/github.com/mauriziofonte/onefold)

Your files are spread over a laptop, an old PC, a phone backup, two external drives and a NAS. The same photo exists five times under five names; some copies were resized, some renamed by tools that added `(1)` or a date in front. onefold finds what is the same, keeps one copy of every distinct thing and puts it under a predictable name, without ever touching the originals unless you ask.

![The onefold GUI merging two folders: preview, then execute](assets/demo/gui-merge.gif)

One static binary for Linux (amd64, arm64) and Windows (amd64), with a local GUI and a full command line. Current version: 0.0.1-alpha, an early pre-release ([CHANGELOG.md](CHANGELOG.md)).

## Contents

- [What it does](#what-it-does)
- [Safety](#safety)
- [Install](#install)
- [Quick start](#quick-start)
- [The GUI](#the-gui)
- [Merge folders](#merge-folders)
- [Empty the sources after a merge](#empty-the-sources-after-a-merge)
- [Reorganize a folder in place](#reorganize-a-folder-in-place)
- [Photo and video archive](#photo-and-video-archive)
- [Configuration](#configuration)
- [Optional tools](#optional-tools)
- [Windows notes](#windows-notes)
- [Documentation](#documentation)
- [Build from source](#build-from-source)
- [Contributing](#contributing)
- [License](#license)

## What it does

| Mode | You get | Commands |
|---|---|---|
| Merge folders | one folder holding every distinct file of several folders exactly once, named by a pattern you choose | `merge`, then optionally `purge-sources` |
| Reorganize in place | the same folder, files renamed and moved to a pattern, exact duplicates removed | `rearrange` |
| Photo and video archive | a "golden" tree with one file per photo or video, canonical names such as `PICTURES/JPG/2021/IMG_20210506_070809.jpg`, the capture date and device written into each copy | `catalog`, `plan`, `apply`; later `golden-index`, `match` |

Around them: lists of look-alike photos and same-text documents to review (`similar`), a verified list of leftover copies with a deletion script you run yourself (`reclaim`), a copy for a disk you put away with a later integrity check (`backup`), plain-text copies of documents (`export-text`), transfer helpers (`push` over SFTP, `seed` scripts for rsync or robocopy).

How it decides that two files are the same:

- exact copies: a fast sampled hash, always confirmed on the full content before anything is skipped or deleted;
- photos and videos: also the same shot saved differently (a resized or re-encoded copy, the HDR and normal frames of one capture). The sharpest version wins; the others are recorded, never deleted by the planner;
- distinct things stay distinct: two photos taken in the same second by different phones, the RAW and the JPG of one shot, an edited photo and its original.

## Safety

- **Sources are read-only.** Two operations delete files, only when you ask for them: `rearrange` deletes duplicates confirmed on their full content, and `purge-sources` deletes merged input files whose copy in the output was read again and found equal. Both are dry runs until you add `-execute`, and both write a journal first.
- **Nothing is overwritten.** Every file is written under a temporary name, verified, and only then given its final name with a rename that refuses to replace an existing file.
- **Run it again to finish.** Every command is idempotent: an interrupted run, a power cut, a closed laptop lid are fixed by running the same command again.
- **Preview first.** Commands that write do nothing without `-execute`; the GUI always shows a preview and executes exactly what you reviewed.
- **Local only.** The GUI listens on 127.0.0.1, behind a per-session link. Nothing is sent anywhere.

## Install

Download the binary for your system from the [releases page](https://github.com/mauriziofonte/onefold/releases) and check it:

```bash
sha256sum -c SHA256SUMS --ignore-missing
```

| System | File |
|---|---|
| Windows 10/11 x64 | `onefold-windows-amd64.exe` |
| Linux x64 (also WSL, most NAS) | `onefold-linux-amd64` |
| Linux ARM64 (Raspberry Pi 4/5, ARM NAS) | `onefold-linux-arm64` |

Nothing to install: put the file anywhere (rename it to `onefold.exe` or `onefold` if you like). The optional helper tools are covered [below](#optional-tools).

## Quick start

**With the GUI.** On Windows, double-click `onefold.exe`. Elsewhere run `onefold gui`. The guided setup asks what you want to do, scans your folders and shows what would happen to a sample of your files before anything is written.

**From a terminal**, merging an old disk and a temporary folder into `D:\FILES`:

```bat
:: preview: lists what would be copied, writes nothing
onefold.exe --input D:\FILES --input E:\old-disk --input D:\TMP --output D:\FILES

:: do it
onefold.exe --input D:\FILES --input E:\old-disk --input D:\TMP --output D:\FILES --execute
```

`onefold help` lists every command and flag, `onefold naming` the naming patterns, `onefold doctor` what your system offers.

## The GUI

On Windows a window opens (the console closes); without the WebView2 runtime, the default browser opens instead. From a terminal, on any system: `onefold gui`.

- **Guided setup**: the first page on a fresh install, and a button in every mode. It asks what you want to do (bring folders together, tidy up a folder, a photo and video archive from several computers, only find duplicates), scans the folders quickly (names and sizes only) and shows, after every answer, what would happen to a sample of your files, computed by the real engine. At the end it fills the right mode and starts its preview; it also shows the equivalent command (PowerShell) and saves the answers as a recipe.
- **Every mode of the command line**: merge, rearrange, golden archive, journals and undo, configuration, tool downloads.
- **Preview, then execute**: Execute runs exactly what the preview showed and asks you to type the number of files a rearrange or a source purge will delete.
- **Naming per kind of file**: for photos and images, videos, documents, music, archives and everything else you pick a layout and what happens to the file name (normalized, clean, original, or with the name of a folder in front, like `[merge] posta.mbox`). The filters show every rule; the preview lists the files left out with the reason. "Export settings" saves them for `onefold merge -settings FILE`; "Copy command" gives the same run as a command line.
- **Plan review**: after a preview the Plan tab lists every file (or groups them by folder, with counts and sizes), sorted and filtered by extension. Leave out a file, a folder or a pattern such as `*.data` from there: the plan updates at once, without scanning the disks again. Folders that look like driver or installer packages are pointed out, with a button to leave them all out.
- **After a merge**, "Purge the sources" deletes from the input folders the files that are now in the output (with a preview, the typed count, and the Recycle Bin as an option on Windows).
- **Long runs**: progress is always visible at the top, errors and warnings in the log panel at the bottom. Pause, resume or stop at any time. Runs are saved: one cut by a stop or by switching the computer off is listed under Runs and resumes where it was.
- **English and Italian** (switch at the top right; the default follows the browser language). Folders and options are remembered.
- **Config page**: the owner, comment and video language written into photo and video copies, the time zone, how dates in file names are read, and on Windows a switch for the diagnostic log.

## Merge folders

Fold several folders into one, keeping every distinct file exactly once. The output can be one of the inputs.

```bat
:: preview
onefold.exe --input D:\FILES --input E:\old-disk --output D:\FILES

:: do it
onefold.exe --input D:\FILES --input E:\old-disk --output D:\FILES --execute
```

- Files already in the output are never moved, renamed, overwritten or deleted. Inputs are never modified.
- A file is skipped only if the same bytes are already in the output (checked on the full content).
- New files go to `<EXT>\<YEAR>\<original name>` (undated: `UNDATED`); a name already taken gets `_01`, `_02`...
- Every copy is written to a temporary file, read back and verified, and only then given its final name. If the run is interrupted, run it again.
- The output keeps a `.onefold` folder with a cache: the second run on the same folders takes seconds.

Choosing what and how:

- **Naming per family** with `-name`: `-name all=ext-year+prefix` puts the folder of each file in front of its name (`TXT\2021\[Desktop] nota.txt`), `-name doc=documents+prefix:2` uses the folder two levels up for documents, `-name img=photo-timestamp` renames photos by date. Families: `img` (photos and images), `video`, `doc`, `music`, `arch` (archives), `rest` (everything else, also unknown extensions and files without one). `-naming` takes any preset or your own template (`onefold naming` lists them).
- **Left out by default**: empty files, hidden files, system, cloud-sync and developer folders, OS droppings (`Thumbs.db`, `desktop.ini`), temporary and partial files (`.tmp`, downloads in progress, Office `~$` lock files) and shortcuts (`.lnk`, `.url`). Each rule has a name: `-keep shortcuts` keeps them, `-skip ...` turns one on. `-keep-dotfiles` also merges hidden and empty files (an empty file counts once per name: `.nomedia`, `__init__.py`). `--list` shows every file left out and why.
- **Exclusions**: `-exclude-path D:\COMPS\COMP1\DRIVERS` leaves out a file or a folder, `-exclude-glob "*.data"` or `-exclude-glob "**/node_modules/**"` a pattern (both repeatable). The summary points out folders that look like driver or installer packages and prints the `-exclude-path` flags for them.
- **Settings files**: `-settings FILE` reads the settings exported by the GUI (flags given on the line win); `-print-settings` prints the settings a command line makes.
- **Extra safety**: `-verify-uncached` reads each copy back from the disk instead of the operating system's cache (slower; proves the disk, not only the write).
- **Reports**: `--list` prints every planned copy, `--report plan.json` writes the plan to a file, `--perf` prints timings.

## Empty the sources after a merge

Once a merge has run, the input files whose content is now in the output can be deleted, together with the folders they leave empty:

```bat
:: the merge writes what it settled into the report
onefold.exe --input C:\Users\me\Desktop --output D:\STUFF --execute --report D:\desktop.json

:: preview: lists the files, deletes nothing
onefold.exe purge-sources -from D:\desktop.json

:: do it: type the number of files the preview listed (-trash: to the Recycle Bin instead)
onefold.exe purge-sources -from D:\desktop.json -execute -expect 123
```

- Only files whose copy in the output is read again and found identical right before are deleted; files that failed to copy, files left out by the filters and everything in the output are never touched.
- Folders left empty go too, also when only `desktop.ini` or `Thumbs.db` remain in them; the input folder itself stays.
- Every deletion is written to a journal in the output's `.onefold` folder. Deleted files cannot be restored, except with `-trash` (Windows, local fixed drives only: on a network share or a network or removable drive Windows would delete them for good, so onefold refuses to start).

## Reorganize a folder in place

Bring a folder organized by older tools, or by hand, to a naming pattern, moving files within the same disk:

```bat
:: preview
onefold.exe rearrange D:\FILES

:: do it (a journal is written in D:\FILES\.onefold first)
onefold.exe rearrange -execute D:\FILES

:: undo the moves of a run, or finish an interrupted one
onefold.exe rearrange -undo D:\FILES\.onefold\rearrange-<date>.jsonl
onefold.exe rearrange -resume D:\FILES\.onefold\rearrange-<date>.jsonl
```

- Files are only renamed or moved (same bytes, same dates). Confirmed duplicates (checked on the full content) are deleted; `-dupes report` keeps them.
- `-naming` chooses the pattern: ready-made layouts by extension and year, by date, documents by author, music by artist and album, photos by device or timestamp, videos by resolution, ... or your own, e.g. `-naming "{EXT}/{year|\"UNDATED\"}/{month} - {month-name}/{stem-norm}.{ext}"`.
- Three name variants: `{stem}` is the name untouched; `{stem-norm}` (used by the presets) removes the markers left by older tools and copies but keeps an `[origin] ` prefix; `{stem-clean}` removes that prefix too. Append `-clean` to any preset name to use `{stem-clean}`.
- Collision suffixes of older tools (`-- frev.1`, ` (1)`, `_01` when the original is next to it) are cleaned; new collisions get `_01`, `_02`...
- Flags go before the folder: `onefold rearrange -execute D:\FILES`, not after it.

In `merge` and `rearrange` photos and videos are files like the others: copied or moved unchanged and named by the pattern. Canonical photo and video names belong to the archive below; its scope and tree presets are refused here unless you add `-allow-mixed`.

## Photo and video archive

The archive works in steps that can run on different machines: each computer indexes its own disks, the indexes are planned together anywhere, and each computer then copies its share.

### First run

```bash
# 1. index each machine locally (read-only); -host names the machine in the index
onefold catalog -j 8 -host laptop -o laptop.jsonl /photos /videos
onefold catalog -j 8 -host nas    -o nas.jsonl    /volume1/photo

# 2. put the indexes together, find duplicates, choose the winners
onefold plan -o plan.jsonl -report plan.md laptop.jsonl nas.jsonl

# 3. review: what would be copied, from where, how much (writes nothing)
onefold apply --with-stats plan.jsonl | less

# 4. on each machine, copy its winners into the archive
onefold apply --execute --only-host laptop --target /golden plan.jsonl
```

What ends up in the archive:

- photos: `PICTURES/<FORMAT>/<year>/<PREFIX>_<YYYYMMDD>_<HHMMSS>.<ext>` (`PICTURES/JPG/`, `PICTURES/HEIC/`, `PICTURES/CR2/`...; the RAW and the JPG of the same shot are both kept); PREFIX tells the source: `IMG` phones, `DSC` cameras, `SNPSD`/`PS`/`EDT` edits;
- videos: `VIDEOS/<year>/VID_<YYYYMMDD>_<HHMMSS>.<ext>` (`-video-tier` adds a resolution folder);
- each photo and video copy carries the capture date and the device, plus your name and a comment if you configured them (see [Configuration](#configuration)). Camera RAW files and mpg/mpeg videos are copied byte for byte, never modified;
- the archive scope: jpg, jpeg, heic, heif, avif, png, webp, tif, tiff, camera RAW (cr2, cr3, nef, arw, dng, raf, orf, rw2, ...) and video formats (mp4, mov, mkv, avi, 3gp, m4v, mpg, mpeg, wmv, webm, flv). GIF, BMP and graphics files are not photos and stay out.

Stamping needs exiftool (photos) and ffmpeg (videos): install them before the first `apply` (on Windows: `onefold tools install all`). Without them `apply --execute` refuses to start, because a later run would not stamp copies that already exist; `-allow-unstamped` copies them unchanged anyway and reports how many.

To keep the original bytes and use no extra disk space, add `-link`: each archive name becomes a hard link of its source file (same volume only, on NTFS, APFS, ext4, btrfs and similar file systems). Nothing is stamped, no tool is needed and the sources are not touched, but the archive and the sources then share their bytes: editing a file in either place changes both, so treat the archive as read-only (deleting a source later is safe: the archive name keeps the data).

Useful plan options:

- `-verify-full` reads every candidate duplicate in full before dropping it (run it where the files are readable);
- `-video-tolerance 1` treats re-encoded videos whose length changed by up to a second as the same video;
- `-filename-seq` keeps the counter of date-only names (`IMG-20200101-WA0007.jpg` -> `IMG_20200101_100000_WA0007.jpg`) instead of numbering them `_01`, `_02`.

### Adding files later

New photos (a phone dump, a disk found later) go into the same archive without indexing the archive again:

```bash
# what the archive holds, from the plans applied to it (and their indexes)
onefold golden-index -o golden-index.jsonl -manifest laptop.jsonl -manifest nas.jsonl plan.jsonl

# only what the archive does not hold yet; names already used there get a _01 suffix
onefold catalog -host laptop -o phone.jsonl /phone-dump
onefold match -index golden-index.jsonl -o new.jsonl -report match.json phone.jsonl
onefold apply --execute --target /golden new.jsonl

# next time, index every applied plan: plan.jsonl new.jsonl ...
```

`match` counts as already in the archive the same bytes and the same shot without more pixels. A sharper copy of a photo already in the archive is listed as an upgrade in the report and never written: the archive only grows.

### Review and housekeeping

- `onefold similar -o pairs.json plan.jsonl` lists photos that look alike (a resized copy, a scan of a print, a burst) for you to review; with `-docs` it also groups documents with the same text (the same report as docx and PDF, a file saved twice). It deletes and drops nothing.
- The copies left behind (the duplicates the plan did not choose) stay where they are. `onefold reclaim -target /golden -script reclaim.sh -o reclaim.json plan.jsonl` re-reads every exact duplicate against the file the archive copy came from and writes a script that deletes the verified ones; read it, then run it yourself (PowerShell on Windows, sh elsewhere; `-ps` chooses). Lower-resolution and other variants are only listed.
- A copy for a disk you put away: `onefold backup -execute /golden /mnt/cold` (same paths, only adds, every copy read back, a content manifest in `/mnt/cold/.onefold`); later `onefold backup -check /mnt/cold` reads every file again and names those that changed or went missing.

### Moving the archive to another machine

```bash
# first copy of a big staging tree over the LAN: a script you run (rsync, or robocopy on Windows); never replaces a file
onefold seed -from ./staging -to user@nas:/golden -o seed.sh plan.jsonl

# incremental upload over SFTP; re-run to resume (--key optional with a running ssh-agent)
onefold push --host nas --user me --key ~/.ssh/id_ed25519 ./staging /golden
```

### Other files in the archive

Other files can enter an archive on purpose, under `FILES/` and the other `-layout` trees, with `-allow-mixed`:

```bash
# every file (system/cloud/dev folders, system/unknown extensions and files > 50G are skipped by default;
# -no-sweep disables all of that, -skip-dev-dirs=false only one rule)
onefold catalog -preset all -allow-mixed -host laptop -o a.jsonl /data

# only documents and archives
onefold catalog -preset document,archive -allow-mixed -host nas -o b.jsonl /share

# plan with a layout, full-content verification of duplicates and a report
onefold plan -o plan.jsonl -layout document -verify-full -report report.md a.jsonl b.jsonl

# plain-text copies of documents next to the archive
onefold export-text -target /golden-text plan.jsonl
```

Layouts for other files: `dedupe` (default: `FILES/<EXT>/<year>/<name>`, undated in `0000`), `timeline`, `document`, `music`, `device`, or any `onefold naming` preset or template. To bring other files together without an archive, use `merge`.

## Configuration

What onefold writes into copies, and how it reads dates, is set once per computer (or on the GUI's Config page):

```bash
onefold config set artist "Your Name"     # Artist/Copyright in photo and video copies; none by default
onefold config set signature "My archive" # comment; default "Onefold v<version>", "-" for none
onefold config set language eng           # language of the video streams; none by default
onefold config set timezone Europe/Rome   # IANA zone of every date; default: $TZ, else the computer's zone
onefold config set date-order dmy         # names like 03-04-2020: dmy (3 April) or mdy (March 4); unset = not used
onefold config set names-language it      # month names in folders: en (default) or it
onefold config set photo-date-fallback on # JPEGs without a capture date: EXIF CreateDate, else ModifyDate
onefold config                            # what is set and where the file lives
```

- Dates in names and folders are wall clocks in one time zone. Indexes and plans record their zone, and `plan` and `apply` refuse files made in another zone instead of shifting every date: set the same zone on every machine that works on one archive.
- `plan -artist`, `apply -signature` and `apply -lang` override the configuration for one run (`-` for none).
- Turn `photo-date-fallback` on before the first index of an archive, not later: it changes the name of those photos, and an archive built without it would get them again under the new name.

## Optional tools

No external program is required: metadata of photos (jpg, heic, png, webp, tiff, camera RAW), videos (mp4/mov/3gp, mkv/webm, flv), documents (including Word, Excel and PowerPoint 97-2003), archives (zip, 7z, rar, iso, tar) and audio is read natively, also on Windows.

Optional helpers, used when found on PATH or next to the executable (`tools\` folder): `ffprobe`, `ffmpeg` and `exiftool` (needed to stamp photo and video copies), `pdfinfo`, `pdftotext` (PDF text), `antiword`, `7z`, `unrar`. `onefold doctor` shows what is available.

On Windows (x64), `onefold tools install all` (or the Doctor page of the GUI) downloads exiftool, ffmpeg/ffprobe, the Xpdf tools and 7-Zip from their official sources. Each file has a fixed version and is installed only if its SHA-256 matches the one written in onefold. The tools go into a onefold folder, nothing is installed system-wide, and an interrupted download resumes. On Linux use your package manager (for example `apt install libimage-exiftool-perl ffmpeg poppler-utils p7zip-full`).

## Windows notes

- The first pass over files Windows has not read yet is slow: Microsoft Defender scans each file the first time it is opened (on 5,400 program files the first index took 21 s, a second one 0.25 s). If you trust the tool, you can exclude the onefold process from real-time scanning (administrator PowerShell, your decision): `Add-MpPreference -ExclusionProcess "C:\path\to\onefold.exe"`; `Remove-MpPreference -ExclusionProcess "C:\path\to\onefold.exe"` undoes it. onefold never changes this setting itself.
- Long paths (over 260 characters) are supported.
- If a double-click seems to do nothing, turn on the diagnostic log (`onefold config set diag-log on`) and read `onefold-gui.log` next to the exe.
- Explorer shows the version of release builds in the file properties.

## Documentation

| Document | For |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | how onefold works, in depth: file formats, dedup and naming rules, every mode, the GUI, the platform layer, tests, how to change it safely |
| [LLMS.md](LLMS.md) | contributors and coding agents: build, layout, invariants, conventions in short |
| [TODOS.md](TODOS.md) | open work that needs a decision or a person |
| [CHANGELOG.md](CHANGELOG.md) | what changed in each version |
| [CONTRIBUTING.md](CONTRIBUTING.md), [CONTRIBUTORS.md](CONTRIBUTORS.md) | how to test or change onefold, who did |
| [SECURITY.md](SECURITY.md) | how to report a vulnerability privately |
| `onefold help`, `onefold naming`, `onefold doctor` | flags, naming patterns, your platform and tools |

## Build from source

Go (see `go.mod`), no C compiler:

```bash
make build      # ./onefold for this system
make test       # the test suite
make linux      # dist/onefold-linux-amd64
make windows    # dist/onefold-windows-amd64.exe (with version information)
make release    # all targets, LICENSE, third-party notices and SHA256SUMS
```

The GUI frontend is prebuilt and embedded; Node is needed only to change it (`make web`, `make web-e2e`).

Versions follow [Semantic Versioning](https://semver.org); the current one is in [`VERSION`](VERSION), the history in [CHANGELOG.md](CHANGELOG.md). `onefold version` prints the version of a binary: a build that is not an exact release says so (`v0.0.1-alpha+dev.<commit>`). Every release tag is built and tested on Linux and Windows by GitHub Actions and published with its checksums; versions with a suffix such as `-alpha` are marked as pre-releases.

## Contributing

onefold is in alpha and looks for **developers** who want to work on it regularly (Go, Svelte, macOS and Windows internals, binary file formats) and for **alpha testers** on every OS, especially macOS and large archives. Start with [CONTRIBUTORS.md](CONTRIBUTORS.md) (what is wanted) and [CONTRIBUTING.md](CONTRIBUTING.md) (how). Code contributions are made under a [Contributor License Agreement](CLA.md); everyone follows the [Code of Conduct](CODE_OF_CONDUCT.md). Security problems: [SECURITY.md](SECURITY.md).

## License

Copyright (c) 2026 Maurizio Fonte <fonte.maurizio@gmail.com>. Licensed under the [GNU Affero General Public License v3.0 or later](LICENSE) (`AGPL-3.0-or-later`): you may use, study, modify and share it, also commercially; whoever distributes a modified version, or lets users reach it over a network, must offer its source under the same license. Release binaries come with `THIRD_PARTY_NOTICES.txt`, the licenses of the components they include.
