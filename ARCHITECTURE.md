# onefold architecture

This document explains how onefold works, from the problem it solves down to the file formats it writes and the order of every check it makes. It is written for a reader with no prior context: a developer or a coding agent that has just cloned the repository. It describes the code as it is; when this document and the code disagree, the code wins and this document must be fixed in the same change.

Companion documents:

| Document | For |
|---|---|
| `README.md` | users: what onefold does, how to run it |
| `LLMS.md` | coding agents: build commands, layout, invariants, conventions in short form |
| `ARCHITECTURE.md` (this file) | everyone who changes the code: how every part works and why |
| `TODOS.md` | open work that needs a person (a decision, rights, an install) |
| `CHANGELOG.md`, `VERSION` | released versions; the version being prepared (section 5.4) |
| `onefold help`, `onefold naming`, `onefold doctor` | the binary's own reference: flags, naming grammar, platform and tools |

## Contents

1. [What onefold is](#1-what-onefold-is)
2. [Vocabulary](#2-vocabulary)
3. [Guarantees](#3-guarantees)
4. [Repository layout](#4-repository-layout)
5. [Build, toolchain and release](#5-build-toolchain-and-release)
6. [Files onefold reads and writes](#6-files-onefold-reads-and-writes)
7. [The file-type registry](#7-the-file-type-registry)
8. [Identity: sparse and full hashes](#8-identity-sparse-and-full-hashes)
9. [Time, zones and dates](#9-time-zones-and-dates)
10. [Metadata extraction](#10-metadata-extraction)
11. [Catalog: the indexer](#11-catalog-the-indexer)
12. [Plan: dedup and winner election](#12-plan-dedup-and-winner-election)
13. [The naming engine](#13-the-naming-engine)
14. [Apply: materializing the golden tree](#14-apply-materializing-the-golden-tree)
15. [Growing the golden tree: golden-index and match](#15-growing-the-golden-tree-golden-index-and-match)
16. [Review and maintenance tools](#16-review-and-maintenance-tools)
17. [Merge](#17-merge)
18. [Purge of merged sources](#18-purge-of-merged-sources)
19. [Rearrange](#19-rearrange)
20. [Settings files](#20-settings-files)
21. [User configuration](#21-user-configuration)
22. [The GUI](#22-the-gui)
23. [Progress, reports and telemetry](#23-progress-reports-and-telemetry)
24. [Platform layer](#24-platform-layer)
25. [Windows specifics](#25-windows-specifics)
26. [Command-line reference](#26-command-line-reference)
27. [Testing](#27-testing)
28. [Changing onefold safely](#28-changing-onefold-safely)
29. [Known limits](#29-known-limits)

---

## 1. What onefold is

onefold is a single static Go binary (Linux amd64 and arm64, Windows amd64) for people whose files are spread over several computers and disks: the same photo on a laptop, a phone backup, two external drives and a NAS, under different names, sometimes resized, sometimes renamed by tools that added `(1)` or a date prefix. It finds what is the same, keeps one copy of every distinct thing, and puts that copy under a predictable name.

It has three families of work, each with its own naming rules (section 13.8, "naming regimes"):

| Family | Commands | What it produces | Touches the sources? |
|---|---|---|---|
| Golden archive (photos and videos, optionally other files) | `catalog` -> `plan` -> `apply`; later `golden-index` -> `match` -> `apply` | a "golden" tree with one file per asset, canonical names (`PICTURES/JPG/2021/IMG_20210506_070809.jpg`), canonical metadata written into the copies | no |
| Folder merge | `merge`, then optionally `purge-sources` | one output folder holding every distinct content of N input folders exactly once, named by a template | `merge`: no. `purge-sources`: deletes merged inputs on request |
| In-place reorganization | `rearrange` | the same folder, files renamed/moved to a template, confirmed duplicates deleted | yes, by design, journaled and undoable (moves) |

Around them: review tools (`similar`, `reclaim`), a cold-storage copy with an audit (`backup`), transfer helpers (`push` over SFTP, `seed` scripts for rsync/robocopy), text sidecars of documents (`export-text`), a local GUI (`gui`) that runs every mode with previews, pause, resume and a guided setup, and a per-user configuration (`config`).

Design principles, in priority order:

1. No data loss. Nothing is deleted or overwritten unless the user asked for that exact operation, and even then only after re-reading the bytes that justify it.
2. Robustness. Every write is atomic and idempotent; an interrupted run is finished by running it again.
3. Parity. Linux and Windows-native behave the same; every external tool is optional and has a pure-Go fallback.
4. Speed. Sparse hashing, one read per small file, parallel workers, caches; never at the expense of 1-3.

The program works on metadata it extracts itself, so the computers do not need to see each other: each host catalogs its own disks into a manifest (a JSONL file), the manifests are planned together anywhere, and each host materializes its share of the plan.

---

## 2. Vocabulary

These words have one meaning everywhere in the code, the docs and the GUI.

| Term | Meaning |
|---|---|
| room | one source directory given to `catalog`, `merge` (an input) or `rearrange` (the root). A room's root is never pruned by a skip rule |
| record | one cataloged file: path, size, hashes, metadata (`catalog.Record`), one line of a manifest |
| manifest | the JSONL output of `catalog`: a header line, then one record per file |
| host | the label of the machine that cataloged a record (`catalog -host`, default the machine name). Every record has one; `apply --only-host` uses it |
| asset | one logical capture or file after dedup: all its copies and variants together (`plan.Asset`) |
| winner / golden | the member of an asset that is kept; the golden tree is the tree winners are materialized into |
| drop | every other member of an asset, with the reason it lost (`exact-dupe`, `lower-res`, `smaller`, `variant`). Drops are recorded in the plan and never deleted by the planner |
| plan | the JSONL output of `plan`: a header line, then one action per asset (winner, canonical path, drops) |
| action | one line of a plan (`plan.Action`) |
| canonRel | the canonical relative path of an action in the golden tree |
| silvering | writing canonical metadata (date, device, owner, comment, stream language) into the golden copy of a photo or video |
| unsilvered | a photo or video copy published without that metadata (tool missing, or the tool refused the file): counted and reported |
| shot | the capture identity of a photo or video, independent of its bytes: time + sub-second + format (photos), time + duration (videos) |
| sparse hash | XXH128 over sampled blocks plus the size: the identity of a file, computed with a handful of reads |
| full hash / content hash | XXH128 over every byte; confirms a sparse match before anything is skipped or deleted |
| canonical zone | the one IANA time zone in which every time string of a process is a wall clock (section 9) |
| keeper | the file whose content another one duplicates: rearrange keeps it, purge re-reads it before deleting a source |
| settled | after an executed merge: every input file whose content is now in the output, with the output file holding it |
| skip rule | a named, switchable reason a file or folder is left out of a catalog (`system-dirs`, `junk`, `transient`...) |
| family | a group of file types with its own naming in merge and rearrange: `img`, `video`, `doc`, `music`, `arch`, `rest` |
| stem mode | what happens to a file name in merge/rearrange: `norm`, `clean`, `raw`, `prefix[:N]` |
| naming regime | golden (canonical names, media scope) versus template (naming engine, every file generic); never mixed |
| preview | a dry run: computes everything, writes nothing to the trees |
| gate | the pause/stop control the engine waits on between units of work (GUI) |
| job | one GUI run of a mode (preview, then execute), persisted on disk and resumable |
| state dir | the per-user durable directory of the GUI jobs, the configuration and the downloaded tools |

---

## 3. Guarantees

Every guarantee below is enforced by code and pinned by tests. Breaking one is a bug, not a trade-off.

### 3.1 Sources are read-only

The only files onefold writes are:

| Writer | Writes |
|---|---|
| `catalog -o` | the manifest |
| `plan -o`, `plan -report` | the plan, the report |
| `apply --execute --target D` | files under D only |
| `export-text --target T` | `.txt` sidecars under T only |
| `push` | the remote destination only |
| `merge -execute` | new files in the output only, plus `<output>/.onefold/` |
| `golden-index -o`, `match -o/-report`, `similar -o`, `reclaim -o/-script`, `seed -o/-list` | the named files |
| `backup -execute SRC DST` | new files in DST, plus `DST/.onefold/` |
| GUI | its state dir; and what the mode it runs writes |
| `config set` | `<state dir>/config.json` |

Two operations delete source files, both only when asked explicitly, both dry runs by default:

- `rearrange -execute` renames files inside its root and deletes duplicates confirmed on their full content (keeper and duplicate re-read right before the deletion). Every run writes an fsync'ed journal first; moves can be undone.
- `purge-sources -execute -expect N` deletes the input files of an executed merge whose copy in the output is read again in full and found equal right before, after the user typed the number of files the preview listed. It is journaled. With `-trash` on Windows the files go to the Recycle Bin.

`reclaim` never deletes: it writes a script the user may run.

### 3.2 Writes are atomic, idempotent and never replace

- Every file is built under a temporary name in its destination directory and published with a rename that refuses to replace an existing file (`fsx.RenameNoReplace`: `renameat2(RENAME_NOREPLACE)` on Linux, `MoveFileEx` without `MOVEFILE_REPLACE_EXISTING` on Windows). A destination that appeared meanwhile is never overwritten; the action counts as skipped.
- `apply` and `export-text` skip a destination that exists (silvering changes the size, so the test is existence, not size). Running them again finishes an interrupted run.
- `merge` never moves, renames, overwrites or deletes an existing output file; it deletes only its own `*.onefold-tmp` leftovers.
- `push` never overwrites a remote file: same size = skipped (resume), different size = conflict (listed, exit 1).
- `backup` never replaces a file of the copy: another file under the same path is a conflict.

### 3.3 Duplicates are proven, not guessed

A sparse-hash match is a candidate. Before a file is skipped as "already present" (merge), deleted (rearrange, purge) or listed as safe to delete (reclaim), its full content is read and compared. Unreadable members are never treated as duplicates.

### 3.4 Identity comes from the original bytes

Hashes are always computed on the original files, never on silvered copies (silvering changes the bytes). That is why the golden tree is never cataloged again to know what it holds: `golden-index` reads the plans that were applied (section 15).

### 3.5 Dry run by default

`apply`, `merge`, `rearrange`, `purge-sources`, `backup` do nothing without `--execute`/`-execute`. The GUI always runs a preview first; Execute runs exactly the confirmed preview (a fingerprint comparison), and a resume only a subset of it.

### 3.6 Frozen contracts

The media naming and winner rules decide the names of golden files. Once an archive exists, a different rule would materialize the same asset again under another name. They are frozen and pinned:

| Contract | Pinned by |
|---|---|
| media scope: which extensions are photos and videos | `TestMediaKindsFrozen` (`internal/kinds`) |
| canonical extension aliases (`jpeg->jpg`...) | `TestCanonicalFrozen` (`internal/kinds`) |
| media naming and winner rules | `TestMediaSynthRegression` (`internal/plan/synth_test.go`, on `testdata/media-synth`) |
| golden tree presets for other files | `TestGenericPlanRegression` (`internal/plan/regression_test.go`, on `internal/plan/testdata/generic-synth.jsonl`) |
| a private census plan (sorted md5) | `TestMediaPlanRegression`, skipped where the private fixture is absent |

New behaviour that would change those results is added behind an option whose default keeps them green (examples: `-video-tolerance`, `-filename-seq`, config `photo-date-fallback`).

### 3.7 No external transport binaries, optional tools

onefold never executes rsync, ssh or scp: `push` speaks SFTP in pure Go, `seed` writes a script for the user. Every other external program (`exiftool ffprobe ffmpeg pdfinfo pdftotext antiword 7z unrar`) is optional, called only through `internal/tools`, and every format has a pure-Go reader. `ONEFOLD_NO_EXTERNAL=1` disables them all; a missing tool never fails a run, except `apply --execute` on media, which refuses to start without exiftool/ffmpeg unless `-allow-unstamped` (section 14.4).

### 3.8 Times are wall clocks of one zone

Every time in manifests and plans is a wall-clock string in the canonical zone of the process (section 9). Manifests and plans carry that zone in a header line and are refused by a process running in another zone.

### 3.9 Local-only GUI

The GUI server listens on 127.0.0.1 only, needs a per-session cookie obtained through a random token link, refuses foreign `Host` headers (DNS rebinding) and cross-site writes.

---

## 4. Repository layout

```
.
├── cmd/
│   ├── onefold/            the CLI: main.go dispatch, one file per command family
│   └── bakeoff/            throwaway benchmark of metadata libraries (not product)
├── internal/
│   ├── kinds/              file-type registry (types.tsv), presets, skip lists, magic sniff
│   ├── meta/               metadata extraction: dispatch, dates, zone, devices, native readers
│   ├── sparsehash/         sparse and full XXH128 digests
│   ├── scan/               throughput probe behind `onefold index`
│   ├── catalog/            indexer: walk, filters, skip rules, exclusions, manifests, headers
│   ├── plan/               dedup, winner election, canonical names, plan files
│   ├── names/              name primitives: sanitize, _NN, legacy markers, copy names, tables
│   ├── naming/             naming templates, presets, families, collision resolver
│   ├── golden/             golden index and incremental match
│   ├── materialize/        apply: copy, silver, publish
│   ├── textexport/         export-text: plain-text sidecars, pure-Go extractors
│   ├── similar/            look-alike photos (dHash) and same-text documents
│   ├── reclaim/            verified drop list and deletion script
│   ├── backup/             cold-storage copy and audit
│   ├── seed/               rsync/robocopy seed scripts
│   ├── remote/             pure-Go SFTP push
│   ├── merge/              merge engine
│   ├── rearrange/          in-place rearrange with journal, resume, undo
│   ├── purge/              verified deletion of merged sources
│   ├── settings/           merge/rearrange settings schema (CLI, files, GUI)
│   ├── config/             per-user configuration
│   ├── runctl/             pause/stop gate
│   ├── report/             progress bars and heartbeat logs
│   ├── planreport/         plan -report (Markdown/JSON)
│   ├── perf/               -perf telemetry
│   ├── fsx/                no-replace rename, temp names, uncached reads
│   ├── tools/              optional external tool registry
│   ├── tooldl/             pinned tool downloads (Windows)
│   ├── statedir/           per-user state directory
│   ├── winres/             Windows exe resources (icon, manifest, version)
│   ├── gui/                local GUI server, job manager, guided setup
│   │   └── web/            Svelte frontend (dist/ committed and embedded)
│   └── testfix/            test-only fixture builders
├── testdata/
│   ├── fixtures/           real-file corpus (documents, archives, media, sheets...)
│   └── media-synth/        synthetic photos/videos pinning the media rules
├── VERSION, CHANGELOG.md   version being prepared; release notes
├── scripts/                fixture generators, workflow check, GUI demo
├── assets/demo/            README demo GIF
├── .github/                CI and release workflows
├── Makefile, go.mod, go.sum
└── README.md, LLMS.md, ARCHITECTURE.md, TODOS.md, LICENSE
```

### 4.1 Packages

| Package | Role | Main entry points |
|---|---|---|
| `cmd/onefold` | parses commands and flags, wires packages into the pipelines, prints summaries (stdout) and progress (stderr), sets exit codes | `main`, `cmdCatalog`, `cmdPlan`, `cmdApply`, `cmdMerge`, `cmdRearrange`, `cmdPurgeSources`, `cmdGoldenIndex`, `cmdMatch`, `cmdSimilar`, `cmdReclaim`, `cmdBackup`, `cmdSeed`, `cmdGUI`... |
| `internal/kinds` | the only source of file-type knowledge | `Lookup`, `KindOf`, `Format`, `Group`, `NormalizeExt`, `PresetExts`, `SniffFile` |
| `internal/meta` | one file -> its canonical metadata | `ExtractWith`, `Zone`, `SetZone`, `CanonicalDevice`, `VendorSlug`, `DeduceFromFilename` |
| `internal/sparsehash` | file identity | `HashFile`, `HashFileWhole`, `HashBytes`, `FullFile` |
| `internal/catalog` | rooms -> records / manifest | `Build`, `Collect`, `Walk`, `Filters`, `LoadManifest` |
| `internal/plan` | records -> assets -> actions | `PlanWith`, `BuildActionsWith`, `VerifyGroupsWith`, `Read`, `Write`, `ShotKey` |
| `internal/names` | pure string functions on names | `SanitizeName`, `WithNN`, `ParseLegacy`, `IsCopyName`, `FoldKey` |
| `internal/naming` | templates, presets, families, collisions | `Resolve`, `Layout`, `Compose`, `Assign`, `VarsOfCtx` |
| `internal/golden` | what the golden tree holds, matching new files | `Build`, `Load`, `Match` |
| `internal/materialize` | plan actions -> golden files | `ApplyAll`, `ApplyWith`, `MissingTools`, `ExifBatch` |
| `internal/textexport` | text sidecars | `Export`, `Extract` |
| `internal/similar` | look-alike review lists | `Find`, `FindDocs`, `DHash` |
| `internal/reclaim` | verified drops, deletion script | `Check`, `Script` |
| `internal/backup` | cold copy and audit | `Run`, `Check` |
| `internal/seed` | rsync/robocopy scripts | `Collect`, `Rsync`, `Robocopy` |
| `internal/remote` | SFTP push | `Dial`, `Client.UploadTree` |
| `internal/merge` | merge engine | `Run`, `CopyVerified`, `WriteCache` |
| `internal/rearrange` | in-place rearrange | `Run`, `RecoverWith` |
| `internal/purge` | delete merged sources | `Plan`, `Run`, `ReadReport` |
| `internal/settings` | merge/rearrange settings | `Merge.Options`, `Rearrange.Options`, `Load`, `Args` |
| `internal/config` | per-user config | `Load`, `SaveTo`, `Apply`, `Override` |
| `internal/runctl` | pause/stop | `New`, `Gate.Wait` |
| `internal/report` | progress output | `New`, `Reporter`, `Printf`, `SetSink` |
| `internal/planreport` | plan report | `Build`, `Write` |
| `internal/perf` | telemetry | `Start`, `End`, `Print` |
| `internal/fsx` | filesystem primitives | `RenameNoReplace`, `TempFor`, `IsTemp`, `OpenUncached` |
| `internal/tools` | external tools | `Path`, `Command`, `Output`, `StartTree` |
| `internal/tooldl` | pinned downloads | `Catalog`, `Installer.Install` |
| `internal/statedir` | state dir location | `Dir` |
| `internal/winres` | exe resources | `Build`, `BuildVersion` |
| `internal/gui` | GUI server and jobs | `New`, `Server.Serve`, `OpenWindow` |
| `internal/testfix` | test fixtures | `Zip`, `Docx`, `PDF`, `RAR5`... |

### 4.2 Dependencies between packages

Leaves: `kinds`, `fsx`, `perf`, `report`, `runctl`, `statedir`, `testfix`, `winres`.

| Package | Imports (internal) |
|---|---|
| `names` | kinds |
| `sparsehash` | perf |
| `tools` | perf, statedir |
| `tooldl` | fsx |
| `meta` | kinds, perf, tools |
| `catalog` | fsx, kinds, meta, names, perf, runctl, sparsehash |
| `naming` | catalog, kinds, meta, names |
| `plan` | catalog, kinds, meta, names, naming, perf, runctl, sparsehash |
| `golden` | catalog, kinds, names, plan |
| `merge` | catalog, fsx, names, naming, perf, plan, report, runctl |
| `rearrange` | catalog, fsx, merge, names, naming, perf, plan, report, runctl, sparsehash |
| `settings` | catalog, merge, naming, rearrange |
| `purge` | kinds, merge, report, runctl, sparsehash |
| `materialize` | fsx, kinds, meta, plan, runctl, tools |
| `textexport` | fsx, kinds, meta, plan, tools |
| `similar` | kinds, plan, runctl, textexport |
| `reclaim` | plan, runctl, sparsehash |
| `backup` | fsx, merge, runctl, sparsehash |
| `seed` | plan |
| `config` | fsx, meta, naming, statedir |
| `gui` | almost everything above |
| `cmd/onefold` | every package except the ones reached only indirectly |

`go list -deps ./internal/<pkg>` is the authority; the rule that matters is that `meta` and `kinds` know nothing of the pipeline, and that only `cmd/onefold` and `gui` know every mode.

---

## 5. Build, toolchain and release

### 5.1 Toolchain

- Go module `github.com/mauriziofonte/onefold`, `go 1.22` language directive. Builds use `CGO_ENABLED=0`: the binaries are static, no C toolchain is needed. CI and releases pin the Go toolchain version in the workflows.
- Pinned dependencies are bumped on purpose only: several newer versions require a newer `go` directive (see `TODOS.md` for the open bump).
- Node is needed only to rebuild the GUI frontend. The built frontend (`internal/gui/web/dist`) is committed and embedded with `go:embed`, so `go build` and `go test` never need Node. `TestWebDistInSync` fails when `dist` is older than the sources.

Direct dependencies and what they are for:

| Module | Used for |
|---|---|
| `github.com/zeebo/xxh3` | XXH128 digests |
| `github.com/charlievieth/fastwalk` | parallel directory walk |
| `github.com/evanoberholster/imagemeta` | EXIF/XMP of JPEG, HEIF, TIFF, RAW |
| `github.com/abema/go-mp4` | only `cmd/bakeoff` |
| `github.com/dhowden/tag` | audio tags |
| `github.com/bodgit/sevenzip` | 7z listing, 7-Zip installer unpacking |
| `github.com/richardlehane/mscfb`, `msoleps` | OLE2 compound files (doc/xls/ppt) |
| `github.com/pkg/sftp`, `golang.org/x/crypto/ssh`, `github.com/skeema/knownhosts` | SFTP push |
| `github.com/vbauerster/mpb/v8`, `github.com/rs/zerolog`, `golang.org/x/term` | progress bar, heartbeat logs, TTY detection |
| `github.com/jchv/go-webview2` | the Windows GUI window |
| `golang.org/x/sys`, `golang.org/x/text` | OS calls; Unicode normalization |

### 5.2 Make targets

| Target | Does |
|---|---|
| `make version` | prints the version a build of this tree gets (5.4) |
| `make build` | `./onefold` for the host, with that version |
| `make test` | `go test ./...` |
| `make vet` | `go vet ./...` |
| `make linux`, `make windows`, `make linux-arm64` | `dist/onefold-<os>-<arch>[.exe]` with the version of 5.4; `make windows` regenerates the exe resources with the build version (section 25.4) and restores the committed file afterwards |
| `make release` | the three builds, `THIRD_PARTY_NOTICES.txt` (`make notices`), `SHA256SUMS`; `DIST=<dir>` to build elsewhere |
| `make notices` | the Go standard library license, the license of every linked module, the licenses bundled in the GUI |
| `make winres` | `go generate ./cmd/onefold`: rewrites `cmd/onefold/rsrc_windows_amd64.syso` (committed) |
| `make web` | rebuilds `internal/gui/web/dist` (Node) |
| `make web-e2e` | Playwright end-to-end test of the GUI against a freshly built binary |
| `make web-demo` | records `assets/demo/gui-merge.gif` (section 22.11) |
| `make workflows-check` | actionlint, zizmor and `act` runs of the workflows on a clean clone |
| `make dist-clean` | removes build outputs |

`./onefold` and `dist/` are build outputs, ignored by git.

### 5.3 Continuous integration and release

- `.github/workflows/ci.yml` (push to `main`, pull requests, called by the release): matrix Ubuntu + Windows; Linux installs exiftool, ffmpeg, poppler, p7zip, antiword from apt; Windows installs the pinned tools with `go run ./cmd/onefold tools install all`, which also proves the download pins. Steps: gofmt (Linux), `go vet` for both GOOS, `go test -count=1 ./...`, then `TestE2E` again with `-v`: the job fails unless it PASSED (a skip is not a pass). A separate `vulncheck` job runs govulncheck (report-only for now, see `TODOS.md`).
- `.github/workflows/release.yml` (tag `v<VERSION>` on a commit of `main`, section 5.4): runs CI, then `make release`, checks that no tracked file changed, verifies checksums, versions (`go version -m`, `onefold version`) and notices, and publishes the GitHub release with the binaries, `LICENSE`, `THIRD_PARTY_NOTICES.txt` and `SHA256SUMS`.
- Actions are pinned by commit; `.github/dependabot.yml` keeps the pins current. Workflows run only on github.com (`if: github.server_url == 'https://github.com'`), so mirrors on other forges stay quiet.

### 5.4 Versions and releasing

onefold follows [Semantic Versioning](https://semver.org): `MAJOR.MINOR.PATCH`, with a pre-release suffix (`-alpha`, `-beta.1`, `-rc.1`) before a version is stable. While the major version is 0 anything may change, but the file formats and naming rules that decide names in an existing archive (section 3.6) stay stable and any change to them is called out in `CHANGELOG.md`.

| Piece | Role |
|---|---|
| `VERSION` | the version being prepared, one line without `v` (`0.0.1-alpha`): the single source of truth |
| `CHANGELOG.md` | one `## [<version>] - <date>` section per release, newest first; the release notes are taken from it |
| tag `v<VERSION>` | marks the released commit on `main` |
| `Makefile` | `VERSION` variable: a clean checkout of the tag `v<VERSION>` builds as `v<VERSION>`; any other tree as `v<VERSION>+dev.<commit>[.dirty]`, so a development binary never passes for the release; `make VERSION=...` overrides; `make version` prints it |
| `-ldflags -X main.version=...` | the version inside the binary: `onefold version`, the default comment stamped into media copies (`Onefold v<version>`), the Windows file version (`major.minor.patch.0`, the full string in FileVersion/ProductVersion), the GUI header |
| `TestVersionFile` (`cmd/onefold/version_test.go`) | `VERSION` is valid SemVer and `CHANGELOG.md` has its section |
| `.github/workflows/release.yml` | runs on tags `vX.Y.Z` and `vX.Y.Z-*`; refuses a tag that is not `v` + `VERSION`, not on `main`, or without a CHANGELOG section; publishes as a pre-release when the version has a suffix |

A plain `go build` (without `make`) has the version `dev`.

Releasing:

1. Set the next version in `VERSION` (for example `0.0.2-alpha`, `0.1.0`, `1.0.0-rc.1`) and add its `## [<version>] - <date>` section to `CHANGELOG.md` (what changed for users; call out any change to archive names or file formats).
2. `make test`, commit (`chore: release v<version>`), push `main`.
3. `git tag v$(cat VERSION) && git push origin v$(cat VERSION)`: the release workflow tests, builds, checks and publishes.
4. After the release, development builds read `v<version>+dev.<commit>` until `VERSION` moves to the next version.

---

## 6. Files onefold reads and writes

### 6.1 Header line

Manifests, merge/rearrange caches, plans and golden indexes start with one JSON header line naming what the file is and the canonical zone its times are in:

```json
{"onefold":"manifest","zone":"Europe/Rome"}
{"onefold":"plan","zone":"Europe/Rome"}
{"onefold":"golden-index","zone":"Europe/Rome"}
```

`catalog.ParseHeader` recognizes it (an object with the `onefold` key). `catalog.CheckZone` refuses a file without header or in another zone than the process: the user sets the same zone (`onefold config set timezone ...`) or writes the file again. Caches in another zone are silently rebuilt.

### 6.2 Manifest record

One JSON object per line after the header (`catalog.Record`):

```json
{"room":"/photos","path":"/photos/DCIM/IMG_0001.jpg","size":3145728,
 "hash":"9f1c...32 hex","fullHash":false,"host":"laptop","mtimeNs":1620284889000000000,
 "contentHash":"",
 "meta":{"kind":"photo","time":"2021-05-06 07:08:09","timeSource":"metadata","subSec":true,
         "subSecNs":250000000,"device":"Apple iPhone 12","canonDevice":"Apple iPhone 12",
         "category":"smartphone","prefix":"IMG","width":4032,"height":3024,"durationSec":0,
         "fileCategory":"raster_image"}}
```

| Field | Meaning |
|---|---|
| `room` | the room path as given |
| `path` | absolute path on the cataloging host |
| `size` | bytes |
| `hash` | sparse digest (32 hex chars); for an empty file under `-keep-dotfiles`: `empty\|<folded name>` |
| `fullHash` | true when `hash` is a full-content digest (small or text files) |
| `host` | label of the cataloging machine; merge uses `output`/`input`, rearrange `root` |
| `mtimeNs` | modification time, Unix nanoseconds: reuse key and generic tie-break |
| `quick` | cataloged with `-quick` (generic metadata skipped) |
| `contentHash` | full XXH128 when known (full-hashed, verified, copied) |
| `meta.kind` | `photo video image audio document text archive other` |
| `meta.time` | `YYYY-MM-DD HH:MM:SS` wall clock of the canonical zone, or `""` |
| `meta.timeSource` | `metadata`, `filename`, `filemtime`, `none` |
| `meta.subSec`, `meta.subSecNs` | photo captured with a sub-second, and its value in ns |
| `meta.device` | photo/image: raw `Make Model`; video: vendor slug |
| `meta.canonDevice` | canonical device name (photo/image) |
| `meta.category`, `meta.prefix` | photo category and file-name prefix (`smartphone/IMG`, `camera/DSC`, `snapseed/SNPSD`, `photoshop/PS`, `editing/EDT`, `enlargement/ENL`, `""/IMG`); video `VID` |
| `meta.width`, `meta.height`, `meta.durationSec` | pixels; seconds (video) |
| `meta.fileCategory` | registry category of the extension |
| `meta.title`, `author`, `album`, `tagYear`, `track`, `disc`, `pages`, `entries` | generic metadata (documents, audio, archives) |
| `meta.extMismatch`, `meta.sniffExt` | the magic bytes disagree with the extension; the extension they imply when unambiguous |

Fields marked omitempty are absent in older manifests, which load and plan unchanged. The order of records is not deterministic (parallel workers).

Readers: `catalog.LoadManifest` (plan input) fails on any malformed line and on a missing or foreign header; `catalog.LoadManifestByPath` (reuse sources: `-reuse`, caches) skips unreadable lines (those files are processed again) and refuses another zone.

### 6.3 Plan action

One JSON object per line after the header (`plan.Action`):

```json
{"id":"9f1c...","kind":"photo","srcHost":"laptop","srcPath":"/photos/DCIM/IMG_0001.jpg","srcSize":3145728,
 "canonName":"IMG_20210506_070809.jpg","canonRel":"PICTURES/JPG/2021/IMG_20210506_070809.jpg",
 "time":"2021-05-06 07:08:09","device":"Apple iPhone 12","canonDevice":"Apple iPhone 12",
 "category":"smartphone","isEdit":false,"isHDR":false,"artist":"Jane Doe",
 "drops":[{"host":"nas","path":"/backup/IMG_0001 (1).jpg","reason":"exact-dupe","size":3145728}],
 "subSecNs":250000000,"width":4032,"height":3024}
```

| Field | Meaning |
|---|---|
| `id` | sparse hash of the winner |
| `kind`, `srcHost`, `srcPath`, `srcSize` | the winner |
| `canonName`, `canonRel` | its canonical file name and relative path |
| `time`, `device`, `canonDevice`, `category` | what silvering writes |
| `isEdit`, `isHDR` | the winner is an edit / an HDR output |
| `artist` | owner stamped on media copies ("" = none) |
| `drops` | every other member: host, path, reason, size |
| `subSecNs`, `durationSec`, `width`, `height` | the winner's metadata; written when the plan is self-contained (CLI and GUI default), omitted when zero |

`plan.Read` is strict: blank lines are ignored, any other unreadable line fails the whole read (a plan applied with lines missing would silently leave assets out), the header must name the current zone.

### 6.4 Golden index entry

One line per golden asset after the header (`golden.Entry`):

```json
{"id":"9f1c...","kind":"photo","canonRel":"PICTURES/JPG/2021/IMG_20210506_070809.jpg",
 "time":"2021-05-06 07:08:09","subSecNs":250000000,"width":4032,"height":3024,"size":3145728,
 "canonDevice":"Apple iPhone 12","srcPath":"/photos/DCIM/IMG_0001.jpg","hashes":["7a2e...|jpg"]}
```

`hashes` holds the byte keys of the drops known from the manifests (section 15).

### 6.5 Files in an output or root (`.onefold/`)

| File | Written by | Content |
|---|---|---|
| `<output>/.onefold/merge-cache.jsonl` | merge (and rearrange in its root) | header + every record of inputs, output and new copies, with content hashes; reused when path, size and mtime are unchanged |
| `<root>/.onefold/rearrange-<ts>.jsonl` | rearrange `-execute` | journal: header (root, naming, hash parameters, keepEmptyDirs), deletes (path, keeper, content hash), moves (src, tmp, dst, size, hash); fsync'ed before any change |
| `<output>/.onefold/purge-<ts>.jsonl` | purge-sources `-execute` | audit: `start`, `delete`, `fail`, `junk`, `rmdir`, `end` events, fsync'ed |
| `<dst>/.onefold/backup-<ts>.jsonl` | backup `-execute` | one `{rel, size, content}` per copied or present file |

Timestamps in these names are `yyyyMMdd-HHmmss.nnnnnnnnn`.

### 6.6 Temporary names

| Pattern | Who | Notes |
|---|---|---|
| `<dst>.<random36>.onefold-tmp` | merge copies | removed by the next merge into that output |
| `.<name>.onefold-tmp` | apply, export-text, backup manifest | dot-prefixed, in the destination directory |
| `.<stem>.onefold-tmp.<ext>` | apply of videos | ffmpeg picks the container from the extension |
| `<dst>.<id>.onefold-mv` | rearrange pass 1 | user data in flight: never deleted; `-resume`/`-undo` finish them |
| `<remote>.part` | push | renamed after the size check |

`fsx.IsTemp(name)` recognizes the onefold temp forms; the catalog and `push` skip them, merge removes them from its output.

### 6.7 Other files

| File | Content |
|---|---|
| `merge -report FILE` | JSON of `merge.Result`; after an execute it includes `settled` (input of `purge-sources`) |
| `plan -report FILE` | `.md` = Markdown, else JSON: totals, by kind/category/extension/year, duplicate groups, document near-duplicates, extension mismatches, golden file list with `-extensive` |
| `match -report FILE` | JSON: dupes, upgrades, counts |
| `similar -o FILE` | JSON: hashed, skipped, flat, failed, pairs, docGroups |
| `reclaim -o FILE`, `-script FILE` | JSON: safe, review, skipped; the sh or PowerShell script |
| `seed -o FILE`, `-list FILE` | the sh or cmd script; the rsync file list |
| settings JSON (`-settings`, GUI export) | section 20 |
| `<state dir>/config.json` | section 21 |
| `<state dir>/jobs/<id>/...` | section 22 |

---

## 7. The file-type registry

`internal/kinds` is the choke point of file-type knowledge. No other package keeps a list of extensions or compares extensions; they ask `kinds`. `TestChokePoint` scans every Go file (and `README.md`, `LLMS.md`) and fails on quoted dotted extension literals or extension switches outside the package, except a short per-file list of names onefold writes itself (`.jsonl` journals, `.part`, report extensions).

### 7.1 `types.tsv`

`internal/kinds/types.tsv` is embedded and parsed once. About 10,500 rows, one per extension, eight tab-separated columns:

| Column | Example | Use |
|---|---|---|
| `ext` | `jpeg` | lowercase, no dot |
| `kind` | `photo` | extraction pipeline and dedup policy |
| `category` | `raster_image` | fine taxonomy (`-category`, `{category}`, manifest `fileCategory`) |
| `group` | `IMAGE` | layout family, uppercase (`{group}`, naming selectors, presets) |
| `format` | `jpeg` | parser dispatch (meta, textexport) and magic family |
| `canonical` | `jpg` | harmonized extension (`NormalizeExt`, `{ext-norm}`, plan photo folder) |
| `flags` | `common,personal` | `system`, `common`, `personal`, `transient`, `shortcut` |
| `description` | `JPEG image` | human text (`-` = none) |

Lines starting with `# ` (hash and space) are comments; `##` and `###` are real extensions. A malformed row panics at init. A duplicate row silently wins over the earlier one.

### 7.2 Kinds

| Kind | Typical formats | Metadata | Dedup | Apply |
|---|---|---|---|---|
| photo | jpeg, heif, png, webp, tiff, camera RAW | EXIF/XMP: time with sub-second, device, size | bytes + shot (time, sub-second, format) | copy + exiftool stamp; RAW byte copy |
| video | isobmff, ebml, mpeg-ps, mpeg-ts, asf, flv, riff-avi | ffprobe or native readers: time, duration, size, vendor | bytes + shot (time, duration) | ffmpeg remux with metadata; mpg byte copy |
| image | gif, bmp, ico, svg, eps, psd, every other raster/vector/design format | native readers, exiftool fallback | bytes | byte copy |
| audio | mp3, flac, ogg, aiff, aac, wav, wma, m4a | tags (artist, album, title, track, year) | bytes | byte copy |
| document | pdf, ole-*, ooxml-*, odf-*, rtf, epub | per format: creation date, title, author, pages | bytes | byte copy |
| text | text, html | none | bytes | byte copy |
| archive | zip, tar, gzip, 7z, iso, rar, bzip2, xz | newest member date, member count | bytes | byte copy |
| other | everything else | none | bytes | byte copy |

The media scope (kinds photo and video) is the golden scope and is frozen:

- photo: `jpg jpeg heic heif avif png webp tif tiff` and the camera RAW extensions (group RAW: `cr2 cr3 nef arw dng raf orf rw2` and the rest);
- video: `mov mp4 mkv avi 3gp webm flv wmv mpg mpeg m4v`. Other video containers are kind `other`, group `VIDEO`.

GIF, BMP and graphics files are images, not photos: they never enter a golden archive by default.

### 7.3 Presets

`-preset` takes a comma-separated union of preset names; `kinds.PresetExts` resolves them.

| Preset | Extensions |
|---|---|
| `media` | kinds photo + video: the golden scope (default of `catalog`) |
| `all` | no whitelist (default of `merge` and `rearrange`) |
| `common` | the `common` flag |
| `personal` | the `personal` flag |
| `visual` | common photos, raster images and videos |
| `image`, `audio`, `video` | by kind |
| `document` | documents + the text groups TXT, XLS, EMAIL |
| `archive` | archives + group DISK |
| `code` | groups CODE and WEB |

`-ext` replaces the presets with an explicit list; `-category` adds every extension of the named categories. `kinds.GoldenDocumentExts` is a pinned set used by the frozen `library-documents` preset.

### 7.4 Skip lists

| List | Members | Rule |
|---|---|---|
| `SystemDirs` | `$Recycle.Bin`, `AppData`, `Program Files`, `Windows`, `Microsoft`, `htdocs`, `Steam`... | `system-dirs` |
| `CloudDirs` | Dropbox, Google Drive, GDrive, OneDrive, iCloud Drive | `cloud-dirs` |
| `DevDirs` | `vendor`, `node_modules`, `tmp`, `tests` | `dev-dirs` |
| `JunkNames` | `__MACOSX`, `.DS_Store`, `.localized`, `Thumbs.db`, `desktop.ini` | `junk` |
| `ArchiveJunkDirs` | `.git`, `.cache`, `vendor`, `node_modules`, `composer`, `tmp` | archive members ignored when dating an archive |

Directory names match a whole path component, case-insensitively.

`kinds.IsTransient` (flag `transient`: `tmp temp crdownload part partial download swp lck`... and names starting with `~$`, the Office owner files) and `kinds.IsShortcut` (flag `shortcut`: `lnk url website webloc desktop appref-ms`) back the `transient` and `shortcuts` rules.

`kinds.PackageMarkers` / `PackageKind` recognize driver packages (an `.inf` next to `.sys`, `.cat` or `.dll`) and installers (`setup.exe`, `install.exe`, `autorun.inf`, `.msi`/`.msp`/`.msu`, redistributables such as `vcredist*`) so merge and rearrange can point out such folders.

### 7.5 Magic sniff

`kinds.SniffFamily` / `SniffFile` read the first 16 bytes and name a magic family (jpeg, png, gif, pdf, zip, ole, mp4, ...). `kinds.Mismatch` compares it with the family of the extension's format. The result fills `meta.extMismatch` and, when the family maps to exactly one extension, `meta.sniffExt` (used by `plan -fix-ext` for generic kinds, and by the planner's rule that a mislabeled media file never swallows the correctly named copy).

---

## 8. Identity: sparse and full hashes

`internal/sparsehash` gives every file an identity with a constant handful of reads, so terabytes on slow disks (SMR, DRAMless QLC, network shares read where they live) can be indexed.

### 8.1 Sparse digest

1. Read the first 512 bytes; if they contain no NUL byte the file is "likely text" and is hashed in full.
2. Budget: `size * coverage% / sample` blocks (defaults: coverage 5 %, sample 4096 bytes). Fewer than 4 blocks -> full hash.
3. Otherwise read the first 2 blocks, the last 2 blocks and evenly spaced middle blocks at `round(2s + i*step)`, capped at `MaxBlocks` (default 8) blocks in total.
4. XXH128 over the sampled bytes, then the decimal size appended. Output: 32 hex characters, canonical big-endian (`Hi||Lo`).

A full hash (`fullHash: true`) is XXH128 of every byte without the size suffix.

The parameters (`-cov`, `-s`, `-blocks`) are part of the identity: changing them changes every hash, so manifests made with different parameters do not match. The rearrange journal records them and recovery restores them.

### 8.2 Whole-read path

`HashFileWhole(path, cov, sample, maxWhole)`: a file up to `maxWhole` bytes (CLI `-whole-max`, default 320 KiB) is read once, sequentially, and the identical digest is computed from memory (`HashBytes`); the buffer is returned so the magic sniff and the metadata extractors reuse it (`meta.Options.Data`). On Windows the first open of each file costs an antivirus scan, so opening each small file once matters. `TestWholeEqualsSeek` pins that both paths give the same digest.

### 8.3 Full-content verification

`FullFile(path)` reads every byte (4 MiB buffer) and returns the XXH128. `plan.VerifyGroupsWith` uses it on every group of records that share a sparse hash and are not all full-hashed: members whose content differs are split into separate groups (their hash gets a `-<16 hex>` suffix for the rest of the run), unreadable members get a unique `<hash>-unreadable<NNNNNNNN>` key so they never count as duplicates. Members that already carry a `contentHash` are not read again. Merge, rearrange and `plan -verify-full` all go through it.

---

## 9. Time, zones and dates

### 9.1 One canonical zone per process

Every time string onefold stores (`meta.time` in manifests, `time` in plans) is a wall clock `YYYY-MM-DD HH:MM:SS` in the canonical zone of the process. The zone is resolved once (`meta.ResolveZone`), in this order:

1. user config `timezone` (an IANA name);
2. the `TZ` environment variable (a zoneinfo path is reduced to its name);
3. the host zone: Linux `/etc/localtime` link or `/etc/timezone`; Windows the registry `TimeZoneKeyName`, mapped to IANA through ICU (`icu.dll` `ucal_getTimeZoneIDForWindowsID`) for the user's region, else the CLDR default mapping.

There is no silent fallback: an unknown zone is `ErrNoZone` and the commands that need times stop. `time/tzdata` is embedded, because Windows has no zoneinfo database.

`config.Apply` sets the zone (`meta.SetZone`) together with the other process-wide settings at CLI start and before every GUI job. Code that turns a stored time back into an instant parses it in `meta.Zone()`, never in `time.Local`.

Consequences:

- Manifests and plans name their zone in the header and are refused in another zone, instead of every date silently shifting.
- `apply` sets the golden file mtime to the instant of the canonical wall clock, so the same file gets the same mtime on every host whatever its own zone.
- Tests that compare times pin the zone (`meta.SetZone`, or `timezone` in a scratch config for binary runs). The suite is also run with `TZ=America/New_York` and `TZ=UTC`.

### 9.2 Time sources and the clamp

`meta.timeSource` says where a time came from: `metadata` (embedded in the file), `filename` (deduced from the name), `filemtime` (the filesystem), `none`. Every source is clamped to the years 1985 to next year; a value outside is ignored and the next source is tried. Every kind ends with file name, then file mtime.

| Kind | Metadata time |
|---|---|
| photo (JPEG) | EXIF `DateTimeOriginal` with sub-second, no zone shift (EXIF is a local wall clock). With config `photo-date-fallback` on: then EXIF `CreateDate`, then `ModifyDate` |
| photo (HEIC, HEIF, AVIF, PNG, WebP, TIFF, RAW) | `DateTimeOriginal` with sub-second, else `CreateDate` |
| video | container `creation_time` (UTC) minus the whole seconds of the duration (it is written at the end of the recording), in the canonical zone |
| AVI | exiftool `DateTimeOriginal` / `CreateDate` (digicam EXIF) |
| pdf | `CreationDate` (pdfinfo, else a pure-Go scan); zoneless dates are UTC |
| docx/xlsx/pptx | `docProps/core.xml` `dcterms:created` (UTC) |
| odt/ods/odp | `meta.xml` `meta:creation-date` (wall clock) |
| rtf | `\creatim` (wall clock) |
| doc/xls/ppt, epub | OLE SummaryInformation / OPF metadata, exiftool fallback |
| audio | only a recording date (`QuickTime:CreateDate`); an ID3 year is a release date and goes to `tagYear`, never to the time |
| image | EXIF via native readers, exiftool fallback |
| archive | the newest member that is not junk |

exiftool dates are read raw (no `-d` option, which drops zones): a zone suffix (`Z`, `+02:00`) means an instant, converted to the canonical zone; no suffix means a wall clock, taken as is.

### 9.3 Dates in file names

`meta.DeduceFromFilename(base)` tries these patterns, first match wins (`?` is an optional `. - _` or space):

| Pattern | Time |
|---|---|
| `YYYY?MM?DD?HH?MM?SS` | as written |
| `YYYY?MM?DD?WA<digits>` (WhatsApp, uppercase `WA`) | 10:00:00 |
| `YYYY?MM?DD` | 12:00:00 |
| `DD?MM?YYYY` or `MM?DD?YYYY` | 12:00:00; when both readings are valid dates and differ, config `date-order` (`dmy`/`mdy`) decides, else no date |

Invalid dates and month/day rollovers are rejected.

`meta.FilenameCounter(base)` returns the counter next to a date without a time of day (`WA0007` in `IMG-20200101-WA0007.jpg`, `0003` in `2020-01-01_0003.jpg`), used by `plan -filename-seq` (section 12.6).

### 9.4 Month names and languages

`{month-name}` in templates follows config `names-language` (`en` default, `it`); `{month-name-en}` and `{month-name-it}` are fixed. The GUI formats numbers and dates with `Intl` for its own language.

---

## 10. Metadata extraction

`internal/meta` turns one file into a `meta.Meta` (the `meta` object of a manifest record). It is read-only and best-effort: an extraction error degrades `timeSource` and leaves fields empty, it never fails a catalog.

### 10.1 Dispatch

`meta.ExtractWith(path, Options{Quick, Data})`:

1. When `Data` holds the file content (the whole-read path of the catalog), it is registered for this call, so every extractor that opens the file through `openSrc` reads from memory instead of the disk.
2. The kind of the extension decides the pipeline:

| Kind / format | Extractor |
|---|---|
| photo, format jpeg | `extractPhoto`: EXIF and XMP in one pass (`imagemeta` with an XMP parser wired into the JPEG scanner); device from EXIF make/model, else XMP `tiff:Make/Model`, else `CreatorTool`; missing dimensions from the JPEG SOF (`image.DecodeConfig`); a file without a JPEG start-of-image marker is not scanned (guards a library loop) |
| photo, other formats | `extractPhotoImage`: native readers of HEIF/AVIF (Exif item, `ispe` size), PNG (`eXIf` chunk), WebP (EXIF chunk, VP8X/VP8/VP8L size), TIFF and RAW (`imagemeta`); exiftool fallback; always full, even with `Quick` |
| video, format riff-avi | `extractAVI`: exiftool (digicam AVIs carry EXIF that ffprobe ignores) |
| video | `extractVideo`: ffprobe when present (duration, `creation_time`, first video stream size, vendor tags); else `extractVideoNative`: EBML reader (mkv, webm), FLV `onMetaData`, ISO-BMFF box reader (mp4, mov, 3gp, m4v); other containers (wmv, mpg) by file name, as ffprobe reports no creation time for them either |
| everything else | `extractGeneric`, unless `Quick`: documents, audio, images, archives (10.4) |

3. `fileCategory` = registry category; the magic sniff fills `extMismatch` / `sniffExt`.
4. No time yet: the file mtime in the canonical zone (`filemtime`), when it passes the clamp.

### 10.2 Devices

`CanonicalDevice(make, model, software, creatorTool)` folds the many raw strings one physical device appears under into one canonical name, with a category and a file-name prefix:

1. device rules: substrings of the lowercase blob of all signals that map opaque codes or firmware strings to commercial names (phone model codes to "OnePlus 9 Pro" and similar; Samsung `SM-`/`GT-` codes; category-only rules such as HDR+);
2. a known brand from make/model: camera brands -> category `camera`, prefix `DSC`; Sony cameras recognized by ILCE/DSC-/DSLR/Cyber-shot in the model; other brands -> `smartphone`, `IMG`;
3. a device present only in the XMP `CreatorTool` (`NIKON D3500 Ver.1.00`);
4. otherwise the normalized make + model, no category.

Edits override the category while the device stays underneath: `gigapixel` -> `enlargement`/`ENL`, `snapseed` -> `snapseed`/`SNPSD`; a recognized device beats the weaker signals `photoshop` (`PS`) and other editors (`editing`/`EDT`: picasa, lightroom, gimp, instagram...). Default prefix `IMG`.

Videos get a vendor slug (`VendorSlug`) from container tags, first match wins: Android make+model, `android`, GoPro (encoder, firmware, handler), Nikon, Kodak, Canon, Panasonic, Apple (handler), Google, else empty. Low-quality re-encodes lose these tags, which is why the device is not part of a video's shot identity (section 12).

The device rules were built from a census of real archives; their order is part of the frozen media contract (the category picks the prefix, the prefix is in the golden name).

### 10.3 Native readers

| Reader | File | Reads |
|---|---|---|
| ISO-BMFF box walker | `isobmff.go` | `mvhd` creation time (1904 epoch) and duration, video track size (`stsd` coded size preferred over `tkhd`), `ftyp` brands, `hdlr` name, `keys`/`ilst` and QuickTime `udta` text atoms (vendor signals); never reads `mdat` |
| EBML (Matroska, WebM) | `ebml.go` | Segment `Info`: `DateUTC` (ns since 2001-01-01 UTC), `Duration` x `TimecodeScale`; first video track `PixelWidth`/`PixelHeight`; stops at the first Cluster |
| FLV | `ebml.go` | the AMF0 `onMetaData` script tag: duration, width, height, the `creation_time` string ffmpeg writes |
| RAR4 / RAR5 header walker | `purego.go` | member names and times, seeking over packed data, never decompressing; encrypted headers -> fallback |
| 7z | `bodgit/sevenzip` | member listing |
| ISO 9660 | `purego.go` | directory records |
| zip, tar, tar.gz | stdlib | members; `.tar.gz` listed while at most 64 MB of compressed input is inflated, then the gzip header time |
| OLE2 (doc/xls/ppt) | `mscfb` + `msoleps` | SummaryInformation: title, author, dates, pages |
| OOXML, ODF, EPUB | zip + XML | core/app properties, `meta.xml`, OPF |
| RTF | own scanner | `\info` group |
| PDF | own scanner | `CreationDate`, `xmp:CreateDate`, title/author, page estimate; reads 64 KB head+tail, widened to 2 MB only when no date was found |
| EPS | own scanner | DSC `%%CreationDate` |
| audio | `dhowden/tag` | ID3, MP4, FLAC, Ogg tags |

`meta/parity_test.go` extracts the whole test corpus with and without the external tools and requires the same results, except documented cases.

### 10.4 External tools

| Tool | Used for | Called with |
|---|---|---|
| ffprobe | video metadata, preferred when present | `-v quiet -print_format json -show_format -show_streams` |
| exiftool | fallback for images, audio, legacy office, AVI; JPEG device and date only through the native reader | `-m -q -fast2 -j -api QuickTimeUTC`, environment `TZ=<canonical zone>` |
| pdfinfo | PDF metadata, preferred | `-rawdates -enc UTF-8` (poppler and xpdf both accept them) |
| 7z | fallback listing of 7z and ISO | `l -slt -phello -y`, `TZ=UTC` (7-Zip for Windows ignores `TZ`: its local offset is undone) |
| unrar | fallback listing of RAR | `lt -phello -c-`, `TZ=<canonical zone>` |

exiftool calls go through a small pool of persistent readers (`exifpool.go`): up to 4 `exiftool -stay_open True -@ -` processes per run, started on demand with the canonical zone in their environment and replaced when the zone changes. One request writes the arguments and `-executeN`, the reply ends at `{readyN}`. A reader that fails or does not answer within 2 minutes is killed and that file goes through a one-shot exiftool. This avoids exiftool's Perl start-up for each file (measured on one small JPEG: Linux 65.7 ms -> 3.0 ms per file, Windows 181.5 ms -> 12.8 ms); `TestExifReadersParity` pins that readers and one-shot give identical results on the whole corpus.

---

## 11. Catalog: the indexer

`internal/catalog` walks rooms on the host where they live, applies the scope, hashes and extracts every accepted file and emits one record per file. It never writes into a room. `catalog.Build` writes a manifest (`onefold catalog`); `catalog.Collect` returns the records in memory (merge, rearrange, GUI).

### 11.1 Pipeline

1. `fastwalk` enumerates the rooms in parallel (per directory) and only filters names and queues jobs. Enumeration never waits for the workers (an unbounded queue), so the total is known early and progress percentages are honest.
2. A pool of `-j` workers (default: number of CPUs) hashes and extracts each queued file. A separate pool is needed because fastwalk processes all files of one directory on the goroutine that listed it, and archive layouts put thousands of files in one directory.
3. Each worker: reuse check, then `sparsehash.HashFileWhole` (one read for small files), then `meta.ExtractWith` with the preloaded bytes, then the record.
4. Records are emitted under a mutex (`emit`), counted into a `Summary` and either written to the manifest (`Build`) or collected.

Reuse (`-reuse OLD.jsonl`, and the merge/rearrange caches): a previous record is copied (room and host refreshed) when the path, size and `mtimeNs` are equal, `mtimeNs` is not 0, and the old record is not a quick one while the new catalog is full.

Errors (an unreadable directory entry, a stat or hash failure) are counted and reported with their path; they never stop the catalog. A failed manifest write does.

### 11.2 Filters, in order

| Entry | Left out when (reason) |
|---|---|
| directory (never the room root) | inside a `SkipDirs` path (merge: the output inside an input; silent); name starts with `.` (`dot-dirs`); junk name (`junk`); in `-prune` (`prune`); in `SystemDirs`, `CloudDirs`, `DevDirs` with their rule on; user exclusions (`exclude`, `exclude-path`, `exclude-glob`) |
| file | name starts with `.` (`dotfile`, unless `-keep-dotfiles` in merge); onefold temp (silent); junk (`junk`); not in the extension whitelist (`type`); no extension with `noext` on (`noext`); transient (`transient`); shortcut (`shortcuts`); without a whitelist: system extension (`system-ext`), unknown extension (`unknown-ext`); user exclusions; 0 bytes (`empty`, unless `-keep-dotfiles`); outside `-min-size`/`-max-size`; over `-limit` per room (`limit`) |

The skip rules are named and switchable (`catalog.Rules`), all on by default:

| Rule | Leaves out |
|---|---|
| `system-dirs` | Windows, AppData, Program Files, `$Recycle.Bin`, Steam... |
| `cloud-dirs` | Dropbox, OneDrive, Google Drive, iCloud Drive folders |
| `dev-dirs` | `vendor`, `node_modules`, `tmp`, `tests` |
| `dot-dirs` | folders starting with `.` |
| `junk` | `Thumbs.db`, `desktop.ini`, `.DS_Store`, `__MACOSX`... |
| `system-ext` | system file types (only without an extension whitelist) |
| `unknown-ext` | extensions the registry does not know (only without a whitelist; never files without extension) |
| `noext` | files without extension |
| `transient` | temporary and partial files, Office `~$` lock files |
| `shortcuts` | `.lnk`, `.url` and the like |
| `big` | files over 50 GiB (`-max-size`, `0` = no cap) |

CLI: `-skip RULES` / `-keep RULES` (comma-separated names or `all`) switch rules on or off and win over the older boolean flags (`-skip-system-dirs=false`...). `-no-sweep` turns every rule off except `dot-dirs` and `junk` and lifts the default size cap.

User exclusions:

- `-exclude S`: a plain substring of the room-relative path (also file names).
- `-exclude-path P` (repeatable): a file, or a folder with everything in it; absolute, or relative to the current directory; component boundaries, case-insensitive on Windows.
- `-exclude-glob G` (repeatable): a pattern on the room-relative path: `*` inside a segment, `**` any number of segments, `?`, `[abc]`, `/` or `\` alike; a pattern without separator matches any file or folder name. A matching folder is pruned.

`Options.OnSkip` receives every file or directory left out with its reason; merge and rearrange list them (`-list` prints `EXCL` lines, the GUI an "Excluded" tab).

### 11.3 Scope builder

`catalog.Filters` is the scope as the user states it (JSON-serializable, the `filters` object of settings files and GUI jobs). `Filters.Options(defaultPreset)` resolves it into `catalog.Options` with the defaults shared by every front end: every skip rule on, 50 GiB cap (lifted by `-no-sweep` unless given), 320 KiB whole-read. The CLI flags (`cmd/onefold/flags.go`), the settings package and the GUI all build options through it, so a scope means the same everywhere.

Regime checks (section 13.8): `Filters.CheckGoldenScope` (every selected extension is a photo or video; `all` refused) and `Filters.CheckTemplateScope` (no preset `media`), both returning `catalog.ErrMixed`.

### 11.4 Package folders

Every file seen (also skipped ones) feeds `kinds.PackageMarkers` per directory. Folders that look like driver or installer packages and hold cataloged files are reported in `Summary.Packages` (folder, kind, files, bytes): merge and rearrange print them with ready `-exclude-path` flags, the GUI shows a banner with a button to leave them out. They are never excluded on their own.

### 11.5 Hosts and headers

`Build` writes the header (`{"onefold":"manifest","zone":...}`) and labels every record with `-host` (default `catalog.DefaultHost()`, the machine name). The planner refuses records without a host; there is no guessing from paths.

---

## 12. Plan: dedup and winner election

`internal/plan` is pure computation over records (only `-verify-full` reads files). `plan.PlanWith(records, Options)` returns a `Result` with one `Asset` per unique logical capture or file; `plan.BuildActionsWith(result, Options)` turns assets into actions with collision-free canonical paths.

### 12.1 Options

| Option | CLI | Default | Effect |
|---|---|---|---|
| `VerifyFull` | `-verify-full` | off | full-content check of every multi-member hash group before anything is dropped (needs the files readable here) |
| `Layout` | `-layout` | `dedupe` | naming of non-media files (section 13) |
| `FixExt` | `-fix-ext` | off | non-media files take the extension implied by their content when unambiguous |
| `VideoTier` | `-video-tier` | off | videos under `VIDEOS/<year>/<tier>/` |
| `Artist` | `-artist` | config `artist` | owner stamped on media copies, recorded per action |
| `SelfContained` | `-self-contained` | on (CLI, GUI); off in the zero value | write the winner's sub-second, duration and size into each action |
| `VideoTolerance` | `-video-tolerance SEC` | 0 | cluster videos of one start time whose durations differ by at most SEC |
| `FilenameSeq` | `-filename-seq` | off | keep the counter of a date-only file name in the canonical name |

The zero value of `Options` is the historical behaviour the frozen tests pin.

### 12.2 Partition

Every record goes to exactly one bucket:

| Record | Key | Bucket |
|---|---|---|
| video with a time | `time \| d<round(duration)>` | time cluster |
| photo with a time and a sub-second | `time \| s<subSecNs> \| <canonical extension>` | time cluster |
| other photo | `hash \| <canonical extension>` | bytes |
| anything else | `hash` | bytes |

`plan.ShotKey` returns the time-cluster key (or "" for byte identity); the golden match uses the same function.

Why these keys:

- A photo's sub-second distinguishes burst frames taken in the same second, so the time cluster catches the same capture saved twice (a phone original and an optimized copy) without merging distinct frames. Without a sub-second, two photos of the same second may be different pictures: they are identified by their bytes only, so a resized copy of such a photo is kept as its own asset.
- The canonical format is part of a photo's identity: the RAW and the JPG of one shot, or a HEIC and its exported JPG, are two assets (one winner per format). The same bytes under two photo formats (a PNG saved with a `.jpg` name) are also two assets.
- A video's time and rounded duration survive a re-encode; the device does not (low-quality re-encodes lose the vendor tags), so the device is not in the key.

### 12.3 Time clusters

For each time cluster:

1. Edits (category `snapseed`, `editing`, `photoshop`, `enlargement`) are pulled out: each distinct edit (by hash) becomes its own asset. Edits are never dropped in favour of the original.
2. The rest is split by device: members with the same device as the best member (highest resolution, then largest size) or with no device are variants of one capture; members with another non-empty device are a separate capture, split again recursively.
3. Each variant set elects its winner: highest resolution (width x height), then an HDR output (`_HDR` in the file name), then the largest file. Every other member is dropped with a reason: `exact-dupe` (same hash as the winner), `lower-res`, `smaller` (same resolution, smaller file), `variant` (same resolution, same or larger size, different bytes: for example the non-HDR frame next to the HDR one).

With `VideoTolerance` > 0 the video clusters of one start time are re-cut first: sorted by duration, a member opens a new cluster when it is more than the tolerance longer than the first member of the current cluster. A re-encode whose duration crossed a rounding edge then joins its original.

### 12.4 Byte groups

Each byte group is split into media members and generic members.

- Media members: one asset with the media winner rule (all members are byte-identical, so all drops are `exact-dupe`).
- Generic members: winner by room order (first appearance of the room across the manifests, in command-line order), then a clean name (`IsCopyName`: ` (1)`, `copy`, `Copy of`, `Kopie von`... in the Explorer/Finder strings of the main languages loses), then the oldest mtime (0 last), then the shorter path, then the lexical path. All drops `exact-dupe`.
- A generic group byte-identical to a media winner (a `.bak` copy of a photo) is absorbed into that asset as `exact-dupe`, unless the media winner's extension lies about its content (`extMismatch`): then both stay.

### 12.5 Full-content verification

With `-verify-full`, before partitioning, every group of records sharing a sparse hash is read in full (`VerifyGroupsWith`, parallel). Different content splits the group; unreadable members are kept apart. Merge and rearrange always verify; for `plan` it is optional because the files must be readable where the plan runs.

### 12.6 Canonical names

| Asset | `canonRel` |
|---|---|
| photo | `PICTURES/<FORMAT>/<YYYY>/<PREFIX>_<YYYYMMDD>_<HHMMSS>.<ext>`; FORMAT = canonical extension uppercase (`JPG`, `HEIC`, `TIFF`, `CR2`...); ext = the source extension lowercase |
| video | `VIDEOS/<YYYY>/VID_<YYYYMMDD>_<HHMMSS>.<ext>`, with `-video-tier`: `VIDEOS/<YYYY>/<tier>/...` (tier by the short side: `4K` >= 2160, `1080p`, `720p`, `480p`, `SD`, `unknown` without size) |
| photo/video without time | year `0000`, stamp `00000000_000000` |
| other files | rendered by the naming engine with `-layout` (default `dedupe` = `FILES/<EXT-NORM>/<YYYY or 0000>/<name>.<ext-norm>`) |

PREFIX comes from the device (`IMG`, `DSC`, `SNPSD`, `PS`, `EDT`, `ENL`). Source suffixes such as `-V001` or `_01` never leak into media names.

With `-filename-seq`, a photo or video dated by a file name without a time of day (WhatsApp, date-only names) keeps that name's counter: `IMG-20200101-WA0007.jpg` -> `IMG_20200101_100000_WA0007.jpg` instead of a `_NN` suffix given by hash order.

Collisions: actions with the same `canonRel` (compared case-insensitively, so the tree is valid on Windows) are sorted by winner hash, then source path; the first keeps the bare name, the others get `_01`, `_02`... before the extension. Distinct content is never silently dropped because of a name.

### 12.7 Results and reports

`Result` counts input files, assets, exact dupes, lower-res, smaller variants, hash-only assets, golden and reclaimable bytes, verification statistics, and keeps up to 12 multi-member examples for the summary. `plan -report FILE` writes the report (section 23.2). The plan file is written before the summary is printed, so a closed stdout pipe cannot lose it.

### 12.8 Regression fixtures

- `testdata/media-synth/` holds 24 tiny files made by `scripts/gen-media-synth.sh` with ffmpeg and exiftool (solid colours, made-up devices and dates). `TestMediaSynthRegression` catalogs them with the pure-Go readers in UTC with fixed file times and pins one line per rule: lower-res, HDR as `variant`, `smaller`, edit, device split, burst `_NN`, one winner per format, same bytes under two formats, video lower-res, file-name time, mtime fallback.
- `internal/plan/testdata/generic-synth.jsonl` holds synthetic records of every generic kind; `TestGenericPlanRegression` pins the md5 of the sorted `srcPath -> canonRel` lines for every layout, with and without `-fix-ext`.

---

## 13. The naming engine

`internal/naming` renders target paths from templates. It names generic files in plans (`-layout`) and every file in merge and rearrange (`-naming`, `-name`). `onefold naming` prints the grammar, the kinds and groups, the presets and every placeholder.

### 13.1 Grammar

| Syntax | Meaning |
|---|---|
| literal text | copied |
| `{name}` | placeholder value ("" when unknown) |
| `{a\|b\|"lit"}` | first non-empty of the alternatives (placeholders or quoted literals) |
| `<...>` | optional group: rendered only if every placeholder inside is non-empty; `<A\|B>` = first complete alternative |
| `/` or `\` | path separator; empty segments are dropped |
| `sel[,sel]=tpl;...;*=tpl` | a template per selector: `.ext` (harmonized lowercase extension), a registry group (uppercase, `PDF`), a kind (lowercase, `photo`); precedence extension > group > kind > `*`; `*=` is required |

`<`, `>` and `|` are invalid in Windows names, so they never need to be literal. Every rendered segment is sanitized (`names.SanitizeName`): `/\:*?"<>|` and control characters become `_`, whitespace collapses, leading/trailing spaces and dots are trimmed, Windows device names are defused (`CON` -> `CON_`), UTF-8-safe truncation to 200 bytes. Placeholder values are single segments: a `/` inside a title becomes `_`. Output names are NFC.

Parse errors are reported with their position: unclosed `{`/`<`, stray `>`/`|`/`}`, unknown placeholder, a map without `*=`, an unknown selector, an extension selector that is not harmonized (`.jpeg` never matches: use `.jpg`).

### 13.2 Placeholders

| Placeholder | Value |
|---|---|
| `{ext}`, `{ext-norm}`, `{ext-raw}`, `{EXT}`, `{EXT-NORM}` | extension: lowercase, harmonized, as written, uppercase, uppercase harmonized |
| `{stem}` | the file name without extension, untouched |
| `{stem-norm}` | the name without the markers of older tools and copies: a `(yyyyMMdd_HHmmss) ` prefix, ` -- frev.N`, ` (N)`; a trailing `_NN` removed only when the name without it exists in the same folder; an `[origin] ` prefix kept |
| `{stem-clean}` | `{stem-norm}` without the `[origin] ` prefix |
| `{origin}`, `{origin-1}`..`{origin-9}` | the `[origin] ` value of the name, else the parent folder (1) or the folder N levels up; a drive root is named after its letter, a UNC root after its share |
| `{parent}` | the current parent folder |
| `{year}`, `{month}`, `{day}`, `{date}`, `{time}`, `{datetime}` | from the record time; empty without one |
| `{month-name}`, `{month-name-en}`, `{month-name-it}` | month name in the configured language, English, Italian |
| `{mtime-datetime}` | file mtime in the canonical zone |
| `{name-date}` | the date of a `(...) ` name prefix |
| `{pre-2000}` | `_FORMER` before 2000 |
| `{kind}`, `{group}`, `{category}`, `{format}` | registry facts |
| `{prefix}`, `{photo-prefix}`, `{type-prefix}` | device prefix; the prefix when the image has device or software metadata, else `PIX`; a prefix per file type |
| `{size-bucket}` | a size class |
| `{device}`, `{device-slug}`, `{device-category}` | canonical device, slug, category |
| `{tier}` | video tier from the size |
| `{author}`, `{title}`, `{artist}`, `{album}`, `{album-year}`, `{track-num}` | document and audio metadata; `{track-num}` = `NN`, `DNN` when the disc is above 1 |

### 13.3 Presets

| Preset | Pattern |
|---|---|
| `ext-year` | `<EXT>/<YEAR or UNDATED>/<stem-norm>.<ext>` (default of merge and rearrange; the extension folder keeps the case as written) |
| `ext-flat` | `<EXT>/<name>` |
| `ext-size` | `<EXT>/<size bucket>/<name>` |
| `ext-era-year`, `ext-era-year-month`, `ext-era-year-size-month`, `ext-era-year-origin`, `ext-era-year-origin-mtime` | `<EXT>/[_FORMER]/<YEAR>/...` variants with month, size bucket, `[origin]` prefix, mtime prefix |
| `photo-month` | photos: `<EXT>/_PHOTOS/<YEAR>/<MM - Month>/IMG_<datetime>`; rest by era and year |
| `photo-timestamp` | photos: `<EXT>/<YEAR>/<PFX or PIX>_<datetime>`, undated `<EXT>/Uncategorized/<name>`; rest by extension and year |
| `video-tier` | videos: `<YEAR>/<tier>/<Device>_<date>_<time>` |
| `date-tree` | `<YEAR>/<MM - Month>/<DD>/<TYPE>_<HHMMSS>_<name>`, undated `undated/<name>` |
| `documents` | documents: `<GROUP>/<YEAR>/<Author>/<Title>`; rest by extension and year |
| `music` | audio: `<Artist>/<Year - Album>/<NN. Title>`; rest by extension and year |
| `devices` | photos and images: `<Category>/<Device>/<YEAR>/<name>`; rest by extension and year |
| `library`, `library-files`, `library-date-tree`, `library-documents`, `library-music`, `library-devices` | the golden tree family: rooted under `PICTURES`/`VIDEOS`/`FILES`/`TIMELINE`/`DOCUMENTS`/`MUSIC`/`DEVICES`, raw `{stem}`, undated year `0000`, harmonized extension; frozen |

Any preset + `-clean` uses `{stem-clean}` instead of `{stem-norm}`.

`-layout` aliases (golden tree): `dedupe` -> `library-files`, `timeline` -> `library-date-tree`, `document` -> `library-documents`, `music` -> `library-music`, `device` -> `library-devices`. `naming.Layout(name)` resolves aliases first; `naming.Resolve(spec)` takes preset names, preset + `-clean`, templates and selector maps.

### 13.4 File families and stem modes

Merge and rearrange name files per family:

| Family key | Files | Specific presets |
|---|---|---|
| `img` | photos and images | `photo-timestamp`, `photo-month`, `devices` |
| `video` | videos | `video-tier` |
| `doc` | groups PDF, DOC, XLS, PPT, TXT, EBOOK, EMAIL, CONTACT | `documents` |
| `music` | audio | `music` |
| `arch` | archives | |
| `rest` | everything else, unknown extensions and files without one included | |

Generic presets valid for every family: `ext-year`, `date-tree`, `ext-flat`, `ext-size` and the `ext-era-*` presets.

A choice per family is `{preset, stem, level}`; stem modes: `norm` (default, `{stem-norm}`), `clean` (`{stem-clean}`), `raw` (`{stem}`), `prefix` (clean, then `[<folder N levels up>] ` in front of the name, N = `level`, 1..9). Example with `ext-year` on `/a/MAILBOXES (mbox)/merge/posta.mbox`: level 1 -> `MBOX/2020/[merge] posta.mbox`, level 2 -> `[MAILBOXES (mbox)] posta.mbox`, level 3 -> `[a] posta.mbox`; a name already prefixed keeps its prefix (idempotent).

`naming.Compose(choices)` turns the per-family choices into one spec: a preset name when one preset covers every chosen family unchanged, else a selector map with the `rest` choice (or `ext-year`) as `*=`. The CLI (`-name img=photo-timestamp -name rest=ext-year+prefix:2`) and the GUI form share it; `naming.CatalogPreset` gives the matching catalog scope (`all` when `rest` is chosen, else the presets of the chosen families).

### 13.5 Collisions: `Assign`

`naming.Assign(items, taken)` gives every item a unique target, comparing names with `names.FoldKey` (NFC + lower case, so names that differ only in case or Unicode normalization collide, as they would on Windows and macOS):

1. Keepers: an item already at its exact target, or at `target_NN`, keeps it (re-runs move nothing).
2. Case-only: an item whose current name differs from its target (or `target_NN`) only by case takes that slot when free (`X.PDF` -> `X.pdf`), so case changes never swap files.
3. Everyone else, sorted by folded target then by a tie-breaker (content hash + path): the target, else `target_01`, `target_02`..., first free.

Reserving a path also reserves its parent directories, so a file is never placed where a folder must exist. Merge passes the raw listing of the output (files and folders) as `taken`; rearrange passes every path that is not an item (folders, out-of-scope files, files to delete).

### 13.6 Name primitives

`internal/names` is a leaf package of pure functions: `SanitizeName`, `WithNN` / `NNOf` (`_%02d` before the extension), `ParseLegacy` (the marker grammar: `(yyyyMMdd_HHmmss) ` prefix, `[origin] ` prefix, ` -- frev.N`, ` (N)`, trailing `_NN`), `IsCopyName`, `NFC`, `FoldKey`, month name tables, `VideoTier`, `SizeBucket`, `TypePrefix`. A trailing `_NN` alone can be information (`exam10_09_04`), so it is stripped only with the sibling check of `naming.VarsOfCtx`.

### 13.7 Placeholder values from a record

`naming.VarsOfCtx(record, exists)` computes every placeholder value of a record; the `exists` callback enables the `_NN` sibling rule (merge, rearrange). `naming.VarsOf` (plan) never strips `_NN`.

### 13.8 Naming regimes

Two naming regimes are never mixed:

| Regime | Commands | Scope | Names |
|---|---|---|---|
| golden | `catalog`, `plan`, `apply`, `match` | preset `media` by default | canonical media names (12.6); other files only with `-allow-mixed`, under the `library*` presets |
| template | `merge`, `rearrange` | preset `all` by default | the naming engine; photos and videos are files like the others, never renamed canonically, never silvered |

The CLI refuses preset `media` and the `library*` presets in merge and rearrange, and a scope wider than photos and videos in `catalog`, unless `-allow-mixed`. The GUI server always refuses them. Reason: a golden tree must contain only what the golden rules produce, and a merged folder must not half-follow them.

---

## 14. Apply: materializing the golden tree

`onefold apply PLAN` without `--execute` prints the dry run: one `GOLDEN` line per action (kind, host:path, canonRel, time, device, edit, HDR, silver tags) and one `DROP` line per drop; `--with-stats` adds the bytes to copy per host and per top folder. `apply --execute --target D` runs `materialize.ApplyAll` on the host that holds the sources (`--only-host` selects the actions of one host).

### 14.1 Per action

`materialize.ApplyWith(action, target, batch, silver)`:

1. `Lstat(target/canonRel)`: exists -> skipped (idempotent; silvering changes the size, so the test is existence).
2. Create the parent folders.
3. Build the file under a temp name in the destination folder:

| Kind | Method | Metadata written |
|---|---|---|
| photo | byte copy, then exiftool on the temp (`-stay_open` batch per worker, one-shot fallback), then fsync of the rewritten file | `DateTimeOriginal`, `CreateDate`, `ModifyDate`; `Artist` and `Copyright` (plan `artist`); `XMP-dc:Source` = canonical device; the signature as `Comment` (JPEG, PNG) or `UserComment` (HEIC/HEIF/AVIF, WebP, TIFF); each only when non-empty |
| camera RAW, MPEG-PS (`mpg`, `mpeg`) | byte copy | nothing (the RAW negative is never modified; MPEG-PS has no metadata slots); not counted unsilvered |
| video | `ffmpeg -c copy -map 0:v? -map 0:a?` into `.<stem>.onefold-tmp.<ext>` | `creation_time` (explicit UTC instant), `artist`, `comment`, `language` of the first video and audio stream |
| AVI | ffmpeg (exiftool cannot write RIFF) | `artist`, `comment`; the date lives in the mtime |
| everything else | byte copy | nothing |

4. Set the temp's mtime to the canonical capture time (the plan time parsed in the canonical zone).
5. Publish with `fsx.RenameNoReplace`; a destination that appeared meanwhile -> temp removed, skipped.

A photo exiftool refuses, or a video ffmpeg cannot remux (exotic codec, truncated file), is published as a faithful byte copy and counted `unsilvered` with the reason. A later run skips it because it exists, so it stays unsilvered.

External processes run bounded: killed with their children (a Windows job object, because the pinned exiftool is a launcher that starts Perl) after 30 minutes plus 1 second per 4 MiB of the file.

### 14.2 Concurrency

Photos and generic files run in a pool of `-j` workers (one exiftool batch each); videos run serially (sequential reads, no seek contention on slow disks); the two groups run concurrently. Progress counts source bytes, so a remux weighs by the size of its file.

### 14.3 Silver tags

| Tag | Source |
|---|---|
| owner (`Artist`, `Copyright`, video `artist`) | config `artist`, or `plan -artist`; recorded per action in the plan |
| comment | config `signature` (default `Onefold v<version>`), or `apply -signature`; `-` = none |
| video stream language | config `language` (ISO 639-2, e.g. `eng`), or `apply -lang`; none by default |

There is no default owner. An empty value is not written.

### 14.4 Missing tools

`materialize.MissingTools(actions, target)` names the tools (`exiftool`, `ffmpeg`) needed by the media actions still to be written (not RAW/MPEG-PS, not already in the target) that are not available. `apply --execute` refuses to start (exit 2) when the list is not empty, because those copies would stay unsilvered for good, unless `-allow-unstamped`. The GUI check shows the missing tools and enables Apply only when the user allowed unstamped copies.

### 14.5 Text sidecars

`onefold export-text --target T PLAN` writes `T/<canonRel>.txt` for every document and text action: a header (file name, host:path, size, extension, golden path, date and its source, title, author, pages) and the extracted text, cleaned (valid UTF-8, LF newlines, no NUL, at most one blank line). Atomic, idempotent, mtime = plan time.

| Format | Extractor |
|---|---|
| docx, pptx, odt, odp, epub | XML text of the document parts (slides in natural order) |
| xlsx, ods | rows, cells separated by tabs (shared and inline strings resolved) |
| doc (Word 97-2003) | pure Go: the FIB names the table stream, its piece table gives runs of cp1252 or UTF-16 text; field instructions dropped, shown results kept, cell marks -> tabs; `antiword` only when the pure-Go reader finds nothing |
| xls (Excel 97-2003) | pure Go: BIFF8 `Workbook` stream, `# <sheet>` then rows (LABELSST with the shared string table and its CONTINUE records, LABEL, NUMBER, RK, MULRK); chart and macro sheets skipped |
| ppt (PowerPoint 97-2003) | pure Go: the text atoms of the Slide and Notes containers; masters and layouts left out |
| rtf | own parser (font/color/style/info/pict groups dropped, `\par`, `\tab`, `\'hh`, `\uN`) |
| html | tags stripped, scripts and styles removed, block ends -> newlines, entities decoded |
| pdf | `pdftotext -q -enc UTF-8 -nopgbrk` (on Windows from the pinned xpdf tools) |
| text kind | the content (32 MB cap) |

Nothing extractable -> `[No text content could be extracted]`.

### 14.6 Transfer to another machine

- `onefold push --host H --user U [--key K] LOCAL REMOTE` (section 16.5) uploads a staged tree over SFTP, resumable.
- `onefold seed -from STAGING -to DEST -o SCRIPT PLAN` (section 16.4) writes an rsync or robocopy script for a fast first copy.

---

## 15. Growing the golden tree: golden-index and match

After the first golden run, new files arrive (a phone dump, an old disk found later). Planning them together with every old manifest would work but means keeping all manifests and re-planning everything; cataloging the golden tree itself would not work, because silvering changed the bytes of every media file. `internal/golden` instead indexes what the tree holds from the plans that were applied to it.

### 15.1 `golden-index`

`onefold golden-index -o INDEX [-manifest M]... PLAN...` reads the applied plans in order and writes one entry per golden asset (`golden.Build`):

- the action's id, kind, canonRel, time, sub-second, duration, size, device, edit flag, source path;
- when a plan lacks the winner's metadata (plans written before plans became self-contained), the metadata is taken from the manifests by host + path (`Joined`); without manifests such entries can only match by bytes (`Unjoined`, printed as a warning);
- the byte keys of the drops, from the manifests, so a new copy of a dropped file is recognized too;
- the same id in a later plan is kept once (`Repeated`).

The index is a JSONL file with the `golden-index` header (canonical zone). It is loaded into maps (`golden.Load`): by byte key, by shot key, by video start time, and the set of names taken in the tree. A 300,000-asset index (about 95 MB) loads in about 1.3 s (`BenchmarkLoad`), so a database is not needed.

Byte keys follow the planner: a photo is identified by its hash and canonical format (the same bytes under another photo format are another asset), any other file by its hash; a generic file with the bytes of a golden photo matches that photo, as the planner absorbs such copies.

### 15.2 `match`

`onefold match -index INDEX [-o SUBPLAN] [-report FILE] MANIFEST...` plans the new files with the normal planner (same options as `plan`: `-video-tier`, `-video-tolerance`, `-filename-seq`, `-layout`, `-fix-ext`, `-artist`) and sorts every elected asset (`golden.Match`):

| Outcome | When |
|---|---|
| dupe `same bytes` | the winner or any member has bytes the tree already holds (original or a known drop) |
| dupe `same shot, not more pixels` | an entry has the same shot key, the same device (or one of the two unknown), neither is an edit, and the new winner does not have more pixels; with `-video-tolerance` a video also matches any golden video of the same start time within that duration difference |
| upgrade `same shot, more pixels` | as above but the new file is sharper: reported in `-report`, never put in the sub-plan, because the golden tree only grows and `apply` never replaces a file |
| new | everything else: goes into the sub-plan |

Names of the new actions first follow the planner's collision rule among themselves; then any name already used in the tree (case-insensitive) moves to the next free `_NN`. This matters: `apply` skips existing names, so a new asset whose name is taken would otherwise be silently lost.

The sub-plan is applied like any plan. Afterwards the index is rebuilt from all applied plans (the old ones and the sub-plan); `apply` does not update it.

---

## 16. Review and maintenance tools

### 16.1 `similar`: look-alike photos and same-text documents

`onefold similar [-max-distance N] [-docs] [-only-host H] [-o FILE] PLAN` builds a list for a person to review; it never drops anything and never changes the plan. Perceptual hashes give too many false positives to decide alone (bursts, scans of prints, two shots of the same scene).

- Photos: every `photo` action with a standard-library decoder (JPEG, PNG, GIF) is decoded and reduced to a 64-bit difference hash (dHash: 9x8 gray cells, each bit = a cell brighter than its right neighbour; box-averaged and sampled so a 12 MP photo costs about as much as a thumbnail). Pairs within `-max-distance` bits (default 6, 0..16) are listed, closest first. Pairs are found with the pigeonhole trick: two hashes within d bits agree on at least one of d+1 disjoint bit slices, so only photos sharing a slice value are compared (`TestPairsMatchBruteForce` pins the equivalence). Flat images (no visible difference between cells) hash to zeros and are counted apart, never paired. HEIC, RAW, WebP, TIFF are skipped (no pure-Go decoder).
- `-docs`: every `document` action's text is extracted (`textexport.Extract`), normalized (lower case, every run of non-letters and non-digits one space) and hashed; groups of two or more golden documents with the same text are listed with their formats (the same report as docx and as PDF, a file saved twice). Texts under 200 characters are ignored.

### 16.2 `reclaim`: freeing the space of drops

The plan's drops stay where they are. `onefold reclaim -target G [-only-host H] [-o REPORT] [-script FILE] [-ps] PLAN` helps a person delete them without onefold deleting anything:

| List | Content |
|---|---|
| safe (in the script) | drops with reason `exact-dupe`, on the same host as their winner, whose golden copy exists in G, that are regular files with the planned size, and whose full content equals the winner's, both read again now |
| review | `lower-res`, `smaller`, `variant` drops: distinct content, a person decides |
| skipped | `exact-dupe` drops that could not be verified, with the reason (other host, golden copy missing, changed size or content, unreadable, empty file) |

The script is POSIX sh (`rm -- '...'`, `set -e`) or PowerShell (`Remove-Item -LiteralPath '...'`, stop on error; `-ps`, default on Windows), one line per safe drop with the golden name as a comment, every path quoted for its shell.

### 16.3 `backup`: cold storage

`onefold backup [-execute] SRC DST` copies a tree (typically the golden) to a disk that is then put away:

- same relative paths; only regular files; the tree's own `.onefold` folder and onefold temp files are skipped;
- the copy only grows: a file present with the same size is kept (its source content hash is recorded), a different file under the same path is a conflict, never replaced;
- every copy uses the merge copy routine (`merge.CopyVerified`: temp, hash while writing, fsync, read back and compare, source mtime, no-replace rename);
- each executed run writes `DST/.onefold/backup-<ts>.jsonl` with `{rel, size, content}` per copied or present file.

`onefold backup -check DST` reads every file of the copy again and compares it with the manifests of every run (a later entry wins): `ok`, `changed` (bit rot or an edit), `missing`, `unreadable`; files without an entry are listed as untracked. Exit 1 when anything changed, went missing or was unreadable.

### 16.4 `seed`: bulk first copy

`onefold seed -from STAGING -to DEST -o SCRIPT [-format rsync|robocopy] [-list FILE] PLAN` writes the script that copies a materialized staging tree to its destination over a fast link; onefold runs neither tool:

- only the plan's files present in STAGING are listed (missing ones counted);
- rsync: a file list plus `rsync -a --ignore-existing --partial --info=progress2 --files-from=LIST -- STAGING/ DEST/`;
- robocopy: one command per folder with its file names (chunked under the command-line limit) and `/XC /XN /XO` (changed, newer and older files excluded: only missing files are copied), `if errorlevel 8 exit /b 1`;
- both never replace a file at the destination.

### 16.5 `push`: SFTP upload

`onefold push --host H --user U [--key K] [--port 22] [--known-hosts F | --insecure-host-key] [--retries 3] LOCAL REMOTE`:

- pure Go: `golang.org/x/crypto/ssh` with ssh-agent (`SSH_AUTH_SOCK`, or the Windows OpenSSH agent pipe), `pkg/sftp` with concurrent writes, `skeema/knownhosts` for host-key algorithm negotiation; no `~/.ssh/config` parsing;
- the connection is tested (`Getwd` + stat of the target) before any byte;
- per file: an existing remote file of the same size is skipped (resume), of another size is a conflict (listed, exit 1, untouched); else upload to `<remote>.part`, check the size, set the source mtime, rename;
- local onefold temp files are never uploaded; keepalive every 30 s; on error the CLI reconnects up to `--retries` times with growing back-off.

---

## 17. Merge

`onefold merge -input A -input B ... -output O [-execute]` (shorthand: `onefold --input A --input B --output A`) folds input trees into an output so that every distinct content of the inputs exists in O, copied once, while nothing that O already holds changes. O may be one of the inputs.

### 17.1 Steps

| Step | What happens |
|---|---|
| rooms | O is always cataloged with no user filter (every file counts as present; only `O/.onefold` skipped). An input equal to O or inside O is dropped (already covered). Nested inputs are dropped. O inside an input is pruned from that input |
| catalog | inputs with the filters (default preset `all`, every skip rule on) and the cache `O/.onefold/merge-cache.jsonl`; output records use host `output`, inputs `input`; records are deduplicated by resolved path |
| verify | every multi-member sparse group is read in full (`VerifyGroupsWith`), or the cached content hash is reused; `-trust-sparse` skips this (not recommended: real sparse collisions exist) |
| decide | a group with a member in O is present (its input members are "already in output"; several members in O are reported as output duplicates, never touched); otherwise one copy, winner by input order, clean name, oldest mtime, shorter path, lexical path |
| target | the naming (default `ext-year`: `<EXT>/<YEAR or UNDATED>/<name>.<ext>`), `-naming`, `-name` families, or a template |
| collisions | `naming.Assign` against the raw listing of O (files and folders); a target whose parent exists as a file is `blocked` (reported, exit 1) |
| execute | parallel copy workers (`-copy-j`), dispatched in source-path order (neighbouring files are read together) |
| cleanup | before the first copy, only `*.onefold-tmp` files under O are removed |
| cache | rewritten after execute: every record with raw sparse hashes and content hashes, plus one record per new copy |
| settle | the settled list: every input file already present (keeper = first O member by path) and every published copy with its input duplicates (keeper = the new file) |

The second run over the same folders reuses the cache and takes seconds.

### 17.2 The copy

`copyOne` for each copy:

1. create the parent folders;
2. open the source; its size must still be the cataloged one;
3. create `<dst>.<random>.onefold-tmp` exclusively, pre-size it;
4. copy with a 4 MiB buffer while hashing the source bytes;
5. compare with the cataloged full/content hash when known (a mismatch fails the copy: the source changed);
6. fsync (`-no-fsync` skips);
7. read the temp back and compare its hash (`-no-verify-copy` skips); with `-verify-uncached` the read bypasses the OS cache (Linux: cached pages of the fsync'ed file dropped with `fadvise(DONTNEED)`; Windows: `FILE_FLAG_NO_BUFFERING` with aligned buffers), proving the media rather than the write path; a filesystem that refuses an uncached read falls back to the cached read, counted and printed;
8. set the source mtime;
9. `fsx.RenameNoReplace` to the final name.

Any failure removes the temp; the file counts as failed. Exit 1 when `missing after run` (failed + blocked + not started) is above 0.

### 17.3 Options

| Option | Effect |
|---|---|
| `-naming SPEC` | preset or template |
| `-name FAMILY=PRESET[+clean\|+raw\|+prefix[:N]]` | per-family naming; the families also select the file types |
| `-layout ALIAS` | historical aliases (golden tree ones need `-allow-mixed`) |
| `-settings FILE`, `-print-settings` | settings JSON (section 20) |
| `-list` | every planned copy (`COPY src -> target size`), every excluded entry (`EXCL rule path`), package folders (`PKG`) |
| `-report FILE` | JSON of the result; after an execute it includes `settled` |
| `-keep-dotfiles` | also merge dotfiles and 0-byte files; an empty file is identified by its name (`empty\|<folded name>`), so each distinct empty-file name exists once in O and a second merge copies nothing |
| `-trust-sparse`, `-no-verify-copy`, `-no-fsync`, `-no-cache` | safety switches; the defaults are the safest |
| `-verify-uncached` | read-back from the device (17.2) |
| `-copy-j N` | copy workers |
| catalog filter flags | section 11.2, default `-preset all` |

### 17.4 GUI hooks

The engine exposes what the GUI needs: `Options.Gate` (catalog, verify and each copy wait on it; stopped during execute -> remaining copies counted `Pending`), `Options.BeforeExecute(result)` (called with the plan before anything is written; an error aborts with nothing touched: the GUI compares the plan with the confirmed preview), `Options.StateCache` (a cache file in the GUI state dir: a preview writes nothing into O), `Options.Records` (re-plan on the records of a preview after exclusions, without walking again).

### 17.5 Test hook

`ONEFOLD_TEST_COPY_DELAY` (a Go duration such as `10ms`) holds every copy back before it starts. Only the GUI end-to-end test sets it, so pause, resume and a kill mid-copy find a copy phase still running on a fast machine.

---

## 18. Purge of merged sources

After an executed merge, the input files whose content is now in the output can be deleted: `onefold purge-sources -from REPORT [-execute -expect N] [-trash] [-list]`, where REPORT is the `-report` of the merge (its `settled` list). The GUI offers the same after a merge job ("Purge the sources").

### 18.1 Plan

Every settled item is a candidate unless refused: not an absolute path, listed twice, inside the output, its copy not inside the output, same file as its copy, outside the input folders (also the input folder itself). Candidates are sorted by path. The dry run prints them (`-list`: `DELETE` and `REFUSED` lines) and their count.

### 18.2 Execute

- `-execute` requires `-expect N`, the count of the dry run: a mismatch exits 2 before anything is touched (the folders changed since).
- A journal `<output>/.onefold/purge-<ts>.jsonl` is opened first; every event is fsync'ed. If the journal stops being writable, every remaining candidate fails and no folder is removed: no deletion without its journal line.
- Per candidate, in parallel workers: the source missing -> `gone` (a resumed run); not a regular file -> failed; source and copy are read in full and must be equal to each other and to the recorded content hash, else failed `content differs from its copy in the output: NOT deleted`; then the file is deleted, or moved to the Recycle Bin with `-trash` (Windows).
- Then the folders left empty: ancestors of deleted files strictly below their input folder, never the output or a folder holding it, deepest first; a folder holding only OS droppings (`desktop.ini`, `Thumbs.db`...) counts as empty (the droppings go with it).

### 18.3 Recycle Bin

`-trash` uses `SHFileOperationW` with `FOF_ALLOWUNDO`. It is refused off Windows, and refused on Windows when a source is on a volume without a Recycle Bin (a UNC share, or a drive whose type is not a local fixed disk: network, removable, optical, RAM disk), because Windows would delete those files for good.

---

## 19. Rearrange

`onefold rearrange [flags] ROOT` reorganizes a tree in place: every file renamed or moved to a naming, within the same volume (same bytes, same dates), confirmed duplicates deleted. Flags go before ROOT (Go's flag parsing stops at the first positional argument).

### 19.1 Steps

| Step | What happens |
|---|---|
| guard | refuses to start while any `*.onefold-mv` exists in the tree (an interrupted run): `-resume JOURNAL` first |
| catalog | the root with the filters (default `-preset all`, skip rules on); files out of scope stay where they are and keep their names reserved; `.onefold` skipped; the merge cache format is shared |
| verify | every duplicate group read in full |
| keeper | per confirmed group: already at its target, a clean name, oldest mtime, shorter path |
| targets | `naming.Resolve` (presets or templates; golden tree presets refused) -> `naming.Assign` (taken = every path that is not an item) |
| journal | `<root>/.onefold/rearrange-<ts>.jsonl`: header (root, naming, hash parameters, keepEmptyDirs), deletes (path, keeper, content hash), moves (src, tmp, dst, size, hash); fsync'ed before any change; nothing to do -> no journal |
| deletes | `-dupes delete` (default): duplicate and keeper re-read in full right before `os.Remove`; any mismatch -> not deleted, reported. `-dupes report` keeps them (they are rearranged like the rest) |
| moves | pass 1: `src -> <dst>.<id>.onefold-mv` (no-replace); pass 2: temp -> dst (no-replace); a failed pass 2 goes back to src when possible. Two passes make swaps and cycles (A->B, B->A) safe |
| folders | parents of moved or deleted files removed deepest first when empty (`-keep-empty-dirs` keeps them) |
| cache | rewritten with the new paths, deleted files dropped |

Workers: `-move-j` (default number of CPUs) for deletes and both passes.

### 19.2 Resume and undo

`onefold rearrange -resume JOURNAL` finishes an interrupted run; `onefold rearrange -undo JOURNAL` moves every file back (deletions are final). Recovery (`rearrange.RecoverWith`) decides from the filesystem: forward first finishes pending deletions (each re-read in full against its keeper, found at its path or at its move's temp or destination), then the two passes of moves. A file is touched only when its exact on-disk name, size and sparse digest match the journal. Forward also removes folders emptied by the run unless the journal says to keep them.

The journal stores the absolute root: after moving the tree or remounting it under another drive letter, resume and undo act on the old path and do nothing.

### 19.3 Limits

- Same volume only: `RenameNoReplace` returns an error across volumes.
- A target under a path that exists as a file fails at the folder creation of pass 1 and is reported (there is no upfront `blocked` check as in merge).
- Rearrange refuses the `-keep-dotfiles` scope (empty files identified by name would make two `__init__.py` "duplicates").

---

## 20. Settings files

`internal/settings` is one schema for merge and rearrange: the JSON a GUI job carries, the file `-settings FILE` reads, what the CLI flags fill and what `-print-settings` prints. Both front ends turn it into engine options here (`Merge.Options`, `Rearrange.Options`), so a setting means the same everywhere; `Args` renders it back as a command line (the GUI "Copy command").

```json
{
  "kind": "merge",
  "inputs": ["D:\\FILES", "E:\\old-disk"],
  "output": "D:\\FILES",
  "families": {
    "img": {"preset": "photo-timestamp", "stem": "norm"},
    "doc": {"preset": "documents", "stem": "prefix", "level": 2},
    "rest": {"preset": "ext-year", "stem": "norm"}
  },
  "filters": {"rules": {"shortcuts": false}, "excludeGlobs": ["**/node_modules/**"], "keepDotfiles": false},
  "verifyUncached": false,
  "workers": 8
}
```

| Key | Meaning |
|---|---|
| `kind` | `merge` or `rearrange` (optional; another kind than the command's is refused) |
| `inputs`, `output` / `root` | folders |
| `families` | per-family choice `{preset, stem, level}` (keys `img video doc music arch rest`) |
| `template` | a free template (wins over families) |
| `naming`, `clean` | preset or template (older GUI jobs, CLI `-naming`) |
| `layout` | CLI `-layout` aliases |
| `filters` | `catalog.Filters`: `preset`, `ext`, `category`, `rules`, `noSweep`, `excludePaths`, `excludeGlobs`, `prune`, sizes, `keepDotDirs`, `keepJunk`, `keepDotfiles`, older boolean fields |
| `trustSparse`, `noVerifyCopy`, `noFsync`, `verifyUncached`, `noCache`, `workers` | merge safety switches and workers |
| `reportDupes`, `keepEmptyDirs` | rearrange |

Rules: naming precedence `template` > `families` > `naming` (+`clean`) > `layout` > `ext-year`; families set the scope, so `filters.preset/ext/category` together with families is an error; with `rest` the rules `unknown-ext` and `noext` are off unless named; the regime checks refuse preset `media` and golden tree presets unless `allowMixed` (the GUI never passes it); `Load` refuses unknown keys. `TestSettingsArgsRoundTrip` pins that `Args` + CLI parsing + `-print-settings` give back the same JSON.

---

## 21. User configuration

`internal/config` holds what belongs to the user, not to the program: `<state dir>/config.json`, edited with `onefold config` or the GUI Config page.

| Key (`onefold config set KEY VALUE`) | JSON | Meaning | Default |
|---|---|---|---|
| `artist` | `artist` | owner written as Artist/Copyright into photo and video copies | none |
| `signature` | `signature` | comment written into copies; `-` = none | `Onefold v<version>` |
| `language` | `language` | ISO 639-2 language of the first video and audio stream (`eng`, `ita`) | none |
| `timezone` | `timezone` | canonical zone (IANA) | `$TZ`, else the host |
| `date-order` | `dateOrder` | `dmy` or `mdy` for ambiguous `XX-XX-YYYY` file-name dates | not used |
| `names-language` | `namesLanguage` | month names in names: `en`, `it` | `en` |
| `photo-date-fallback` | `photoDateFallback` | a JPEG without `DateTimeOriginal` takes EXIF `CreateDate`, else `ModifyDate` | off |
| `diag-log` | `diagLog` | Windows: append launch and GUI diagnostics to `onefold-gui.log` next to the exe | off |

- `onefold config` lists every key, its value and its effect, and the file path; `get`, `set`, `unset`, `path`.
- `Set` validates (no control characters in artist/signature, because exiftool argument files are line based; a three-letter language; a loadable zone; known values) and leaves the file untouched on error. `SaveTo` writes atomically.
- Command-line flags override for one run (`plan -artist`, `apply -signature`, `apply -lang`; `-` clears).
- `Config.Apply` makes the process-wide settings current (zone, date order, month names, JPEG fallback): at the start of every command that reads or writes times (`catalog`, `plan`, `apply`, `merge`, `rearrange`, `export-text`, `golden-index`, `match`, `similar`, `reclaim`, `seed`), and in the GUI at start, after a save when no job runs and before every job. A broken config file or an unknown zone stops those commands (exit 2) instead of guessing.
- `photo-date-fallback` changes the canonical name of the JPEGs it dates: turn it on before the first catalog of an archive, not later.
- Tests that run the binary set `ONEFOLD_STATE_DIR` to a scratch folder, so a developer's own config never leaks in.

---

## 22. The GUI

`onefold gui` (or a double-click on the Windows exe) starts a local web server and opens the page: in a native WebView2 window on Windows, in the default browser elsewhere. Every mode of the CLI is available with a preview-first flow, live progress, pause/resume/stop, persistent runs and a guided setup.

### 22.1 Launch

| Flag | Meaning |
|---|---|
| `-port N` | default 47115 (stable, so the page origin and its saved settings are stable); a busy port falls back to a random one |
| `-no-window` | Windows: serve and open the browser instead of the window |
| `-no-open` | open nothing, print the URL |
| `-keep` | browser mode: keep serving when no page is open (default: exit after 2 idle minutes with no job running) |
| `-state DIR` | another state dir for the GUI |

One instance per state dir: a second launch finds `gui.json` (pid + session URL), checks that the link answers and opens that instance.

On Windows, `onefold.exe` with no arguments opens the GUI when it was started by a double-click (the process is alone on its console, or its parent is `explorer.exe`): the console is freed and the window opens. From a terminal it prints the usage. Without the WebView2 runtime the window cannot be created and the default browser gets the URL (`TestWindowFallsBackToBrowser`).

### 22.2 Security

- The server binds `127.0.0.1` only.
- A 32-byte random token: `/?t=<token>` sets the `onefold_session` cookie (HttpOnly, SameSite=Strict) and redirects to `/`; any other request without the cookie gets 403.
- `Host` must be `127.0.0.1:<port>` or `localhost:<port>` (DNS rebinding); non-GET requests with a foreign `Origin` or a cross-site `Sec-Fetch-Site` get 403.
- Headers: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`.

### 22.3 State dir

`$ONEFOLD_STATE_DIR`, else `%LOCALAPPDATA%\onefold` (Windows), else `$XDG_STATE_HOME/onefold`, else `~/.local/state/onefold`; never a temporary folder, because a run must survive a reboot.

| Path | Content |
|---|---|
| `jobs/<id>/job.json` | the job (atomic, fsync) |
| `jobs/<id>/result.json` | the confirmed-plan fingerprint and the result lists |
| `jobs/<id>/log.jsonl` | the job log |
| `jobs/<id>/cache.jsonl` | merge/rearrange state cache (previews write nothing into the trees) |
| `golden/` | manifests and plans of the golden archive view |
| `gui.json` | the running instance |
| `webview2/` | the WebView2 profile (browser storage survives restarts) |
| `recipes.json` | saved guided-setup answers |
| `tools/` | downloaded external tools (section 24.3) |
| `config.json` | the user configuration |

### 22.4 Jobs

A job has an id, a kind (`merge rearrange recover index plan apply purge`), a stage (`preview`, `execute`), a status (`running paused previewed done failed stopped interrupted`), phases, params (the request), a summary, an error, a journal path, counters and live progress (label, unit, done, total, rate, ETA).

- One job at a time: another start answers 409. A tool download and a job exclude each other.
- merge, rearrange, apply and purge start with a preview; recover, index and plan run directly.
- Execute (previewed jobs only) re-plans with `BeforeExecute` and requires the plan to equal the confirmed preview's fingerprint (merge: source + target + size per copy; rearrange: moves and deletes; purge: source + keeper per candidate). Otherwise the job fails with "the folders changed since the preview" and nothing is written. A rearrange execute also needs the typed number of deletions, a purge the typed number of files.
- Continue (interrupted, stopped, failed): a preview is run again; an execute resumes: merge re-plans and the plan must be a subset of the confirmed one (done copies are present now), rearrange runs `RecoverWith` on its journal, index reuses its partial manifest, apply re-runs (idempotent).
- Pause, Resume, Stop act on the job's gate (`runctl`); the status follows and is persisted at each change. Closing the window or Ctrl+C pauses and persists the active job; at the next start it is listed as interrupted and resumable, also after a power loss.
- Retention: the newest 100 jobs; per-job log capped at 20,000 warnings and errors (counters stay exact).
- The user config is applied before every job; a plan job takes the configured owner and an apply preview the configured comment and language, recorded in the params so execute and resume stamp what the preview showed.

Exclusions after a preview: from the Plan tab a file, a folder, a pattern or "this content and its copies" can be left out. `Manager.Exclude` updates the job's filters and re-plans at once on the records of the preview's state cache (no new walk), with a new fingerprint. Removing an exclusion the preview walked with needs a new preview (those files were never cataloged).

### 22.5 API

| Route | Method | Purpose |
|---|---|---|
| `/api/info` | GET | version, OS, CPUs, language, state dir, tools, active job, path separator, host name |
| `/api/config` | GET, POST | user config, effective signature, zone and its source; POST validates, saves, applies |
| `/api/events` | GET | server-sent events: `hello`, `job` snapshots (at most every 250 ms), `log` entries, `tools` snapshots; ping every 20 s |
| `/api/log` | GET | session log ring, or a job's persisted log |
| `/api/fs` | GET | sub-folders of a path and the roots (drives, home, `/mnt/*`, `/media/*`) for the folder picker |
| `/api/naming` | GET | presets, aliases, placeholders, catalog presets, families, generic presets, stem modes, skip rules, Recycle Bin availability |
| `/api/naming/preview` | POST | the composed naming of a form and up to 6 real files of the folders with their target |
| `/api/cli` | POST | the settings as CLI arguments and a PowerShell line |
| `/api/wizard/scan` | POST | quick scan of folders (names and sizes, bounded) for the guided setup |
| `/api/wizard/preview` | POST | the engine's result on a sample of files for the current answers |
| `/api/recipes` | GET, POST, DELETE | saved guided-setup answers |
| `/api/journals` | GET | rearrange journals of known roots with their state |
| `/api/golden` | GET | manifests and plans of the state dir, hosts of a plan |
| `/api/jobs`, `/api/jobs/{id}` | GET, POST, DELETE | list, start, read, forget |
| `/api/jobs/{id}/items`, `/tree`, `/exts` | GET | result lists: pages with search, extension, folder and order; the folder tree; extension counts |
| `/api/jobs/{id}/{execute,continue,pause,resume,stop}` | POST | job actions |
| `/api/jobs/{id}/exclude` | POST | exclusions and instant re-plan |
| `/api/tools`, `/api/tools/install`, `/api/tools/cancel` | GET, POST | pinned tool downloads (Windows) |

Errors are JSON `{error}` with 400 (bad input), 404 (unknown job), 409 (busy or wrong state).

### 22.6 Engine events

While the server runs, `report.SetSink` routes every engine reporter to the active job: start/progress/done events become the job's progress and phase (reporter labels map to phases: `catalog output` -> scan output, `catalog inputs` -> scan inputs, `verify`, `merge`/`materialize` -> copy, `rearrange` -> apply, `purge` -> delete...), warnings and errors become log entries with their path. Log entries carry a translation key and arguments, or raw engine text.

### 22.7 Guided setup

The first page on a fresh install, and a button in every mode. It asks the goal (bring folders together, tidy up a folder, a photo and video archive from several computers, only find duplicates), scans the folders quickly (names and sizes, at most 6 s or 300,000 entries, with the default skip rules), and after every answer shows what would happen to a sample of the user's files, computed by the real engine (scope and rules, metadata extraction, hashing, then the golden planner or the naming with collisions and duplicate keepers). At the end it fills the form of the chosen mode and starts its preview; it also shows the equivalent PowerShell command and can save the answers as a recipe. It never runs a job by itself.

### 22.8 Frontend

`internal/gui/web`: Svelte 5, Vite, Tailwind, Lucide icons; English and Italian (the default follows the browser language). Layout: header (version, platform, tools pill, language switch), a job bar under it on every view (phases, progress, rate, ETA, errors, Pause/Resume/Stop), a sidebar (Guided setup, Merge, Rearrange, Golden archive, Runs, Journals, Config, Doctor), a collapsible log drawer. Merge and Rearrange have a Settings tab (folders, families with layout and file-name mode, live naming preview, filter rules, exclusions, safety switches) and a Plan tab (funnel, stats, package banner, paged result lists with list or folder-tree view, extension filter, order, search, exclusion menus; after a merge the "Purge the sources" card). Every confirmation is a styled dialog: native browser dialogs are a bug (the end-to-end test fails on them). Settings persist in the browser storage, migrated from older versions.

`make web` rebuilds `dist` (also `dist/THIRD_PARTY_LICENSES.md` and `dist/.source-hash`, whose algorithm equals `gui.SourceHash`); `dist` is committed and embedded.

### 22.9 Server tests

`go test ./internal/gui` covers the guard, previews and executes of every kind, the fingerprint refusal, pause/resume, interrupted jobs, rearrange confirmation and journal, purge jobs, exclusions and re-plan, list queries and trees, the config API, regime refusals, the guided setup scan and preview, the tool API, the apply tool check, and that `dist` is in sync.

### 22.10 End-to-end test

`make web-e2e` builds the binary and drives it with Playwright in a real browser: a merge with pause and resume, language switch and persisted settings, a rearrange with typed confirmation, journal undo, a power loss (SIGKILL mid-copy) and resume from Runs, the config page, the golden archive (index, plan, check, apply), the guided setup, the plan review with exclusions and instant re-plan, a Desktop merge with the folder prefix, settings export and copy command, and a source purge with the typed count. Scratch data lives in `E2E_ROOT` (default `/tmp/onefold-gui-e2e`, must not exist; it holds a mode-000 file, so remove it with `chmod -R u+rwX` first). Screenshots go to `internal/gui/web/e2e/screens/`.

### 22.11 README demo

`make web-demo` (`scripts/gui-demo.sh`) runs `internal/gui/web/demo/merge.spec.js` with `playwright.demo.config.js`: the real binary merges two made-up folders (written by the spec, dated 2023) while screenshots are taken every ~200 ms; the system ffmpeg assembles them into `assets/demo/gui-merge.gif`.

---

## 23. Progress, reports and telemetry

### 23.1 Progress (`internal/report`)

stdout carries results (summaries, TSV lists); stderr carries progress and status. `report.New` picks a mode:

| Mode | When | Output |
|---|---|---|
| bar | stderr is a terminal with a width (Linux TTY; the Windows console natively) | counters, `+` while the total still grows, percentage, smoothed speed, ETA |
| log | a pipe, a file, a background run, a 0-column pty | a heartbeat line every `-heartbeat` (default 10 s): done, total (`+` = still growing), percentage and ETA once the total is final, throughput |

Units are bytes (apply, push, merge copies, verify) or items (catalog, rearrange). `report.Printf` prints status lines above an active bar without tearing it. `Finish` prints one summary line on stdout. In the GUI a sink replaces the terminal output (section 22.6). Library code shows progress only when the caller passes a log writer, so tests stay quiet.

### 23.2 Plan report (`internal/planreport`)

`plan -report FILE` (`.md` = Markdown, anything else = JSON; `-extensive` lists everything instead of the top 50): totals with verification statistics; by kind (files, bytes, golden, dropped), category, extension and year; duplicate groups by reclaimable size; document near-duplicates (golden documents with the same title, author and page count but different bytes: listed, never dropped; `similar -docs` goes further, section 16.1); extension mismatches; with `-extensive` the golden file list.

### 23.3 Telemetry (`internal/perf`)

`-perf` (on `catalog`, `merge`, `rearrange`) prints a table of timings to stderr at exit; `-perf-out FILE` writes them as JSON. Stages include `stat`, `open`, `read:sniff512`, `read:whole`, `read:sparse`, `read:full`, `hash:sparse`, `hash:full`, `extract:<kind>`, `spawn:<tool>`, `exiftool:reader`, and merge `mkdir`, `copy`, `fsync`, `verify-copy`, `rename`; slow files are listed as outliers. Disabled telemetry costs one atomic load per probe.

---

## 24. Platform layer

### 24.1 `fsx`

- `RenameNoReplace(src, dst)`: Linux `renameat2(..., RENAME_NOREPLACE)`; Windows `MoveFileEx` without `MOVEFILE_REPLACE_EXISTING`, with `MOVEFILE_WRITE_THROUGH` and the `\\?\` prefix for long paths; where the platform call is unsupported (some filesystems, other OSes) a hard link then removal of the source, and as the last resort a stat-then-rename (not atomic: only where neither primitive exists). An existing destination returns `fsx.ErrExists`. Cross-volume renames fail (rearrange relies on it).
- Temp names: `TempSuffix = ".onefold-tmp"`, `TempFor(dst, keepExt)`, `IsTemp(name)` (section 6.6).
- `OpenUncached(path)` and `AlignedBuffer(n)`: reads that bypass the OS cache (merge `-verify-uncached`).

Anything that must not clobber uses `RenameNoReplace`, never `os.Rename`.

### 24.2 `tools`

The registry of optional programs: `exiftool ffprobe ffmpeg pdfinfo pdftotext antiword 7z unrar`.

- Lookup, cached per process: `PATH`, the exe's folder, `<exe folder>/tools`, then `<state dir>/tools` (downloads). On Windows the candidates are `.exe`, `.bat`, `.cmd` (and `exiftool(-k).exe`). `tools.Reset` clears the cache after an install.
- `ONEFOLD_NO_EXTERNAL=1` makes every tool missing (parity tests).
- `Command` returns nil when the tool is missing; on Windows the child gets no console window.
- `StartTree(cmd)` starts a tool so that a kill also ends every process it started (on Windows a job object with kill-on-close: the pinned exiftool is a launcher running Perl, and killing only the launcher left Perl holding the pipes).
- `Output(env, name, args...)` runs a tool and records a `spawn:<tool>` telemetry stage.

Every external command in product code goes through `tools`; a direct `exec.Command` elsewhere would break window hiding, `doctor` and `ONEFOLD_NO_EXTERNAL`.

`onefold doctor` prints the version, OS/arch, Go version, CPUs, the canonical zone and its source, whether external tools are disabled, each tool's path or `missing (pure-Go fallback)`, and what is pure Go versus what needs a tool.

### 24.3 `tooldl`: pinned downloads (Windows)

`onefold tools [list | install all | ID...]` (and the GUI Doctor page) downloads the optional tools on Windows x64 from their official sources, each pinned by version, URL, size and SHA-256 in `internal/tooldl/catalog.go`:

| ID | Provides | Source |
|---|---|---|
| `exiftool` | exiftool (`exiftool(-k).exe` renamed, with its `exiftool_files/` Perl runtime) | exiftool.org release files |
| `ffmpeg` | ffmpeg, ffprobe (static, LGPL build) | a month-end autobuild of a well-known Windows build project |
| `xpdf` | pdfinfo, pdftotext | the Xpdf publisher |
| `7zip` | 7z (`7z.exe` + `7z.dll`, the full build that reads iso and rar) | the official 7-Zip release, unpacked from its installer in pure Go |

Install: download into `<state dir>/tools/.download/<id>-<version>.onefold-tmp` (resumed with an HTTP Range request when a partial file exists), check size and SHA-256 (a mismatch deletes the file and installs nothing), extract only the mapped members into a staging folder (names that escape the folder are refused), remove the files of the previous install of that id, move the staged files in, rewrite `installed.json`, delete the download. Ctrl+C (or the GUI cancel) keeps the partial download for the next attempt. Other platforms get a hint to use their package manager. Updating a pin is a code change: new version, URL, size, SHA-256 and file map, proved by an install on Windows and the Windows test suites with the tools on PATH.

### 24.4 `statedir`

`statedir.Dir()`: `ONEFOLD_STATE_DIR`, else `%LOCALAPPDATA%\onefold`, else `$XDG_STATE_HOME/onefold`, else `~/.local/state/onefold`. Shared by the GUI, the downloaded tools and the user config. `onefold gui -state DIR` moves only the GUI state.

### 24.5 `runctl`

`runctl.Gate`: `Pause`, `Resume`, `Stop` (final), `Wait` (blocks while paused, returns false once stopped), `State`. Every method of a nil gate is a no-op and `Wait` returns true, so the CLI passes nil and never pauses. Engine consumers call `Wait` before each unit of work: catalog per file, verification per full read, merge per copy, rearrange per delete and pass-1 move, recovery, apply per action, purge per candidate, similar per photo, reclaim per drop, backup per file. Work already started always completes, so a pause or stop never leaves a half-written file. With N workers up to N units finish after a pause.

---

## 25. Windows specifics

Windows-native is the primary target; Linux parity is mandatory.

### 25.1 Parity rules

- Paths: compare with the OS separator in mind (`filepath.ToSlash` in tests), case-insensitive comparisons where Windows is case-insensitive (`names.FoldKey`, `fold` helpers), long paths with `\\?\`.
- Time: `time/tzdata` embedded, the Windows zone mapped to IANA through ICU (section 9.1); never a silent fallback to UTC.
- Tools: optional everywhere; the pinned downloads make them one command away on Windows.
- Console: the progress bar drives the Windows console natively; child tools run without a console window.
- Tests: every package is cross-compiled with `GOOS=windows go test -c` and run on Windows from its package folder; the CLI end-to-end test runs with `ONEFOLD_BIN=<windows exe>` and the pinned tools on PATH.

### 25.2 Launch and diagnostics

`onefold.exe` with no arguments decides between the GUI and the usage text (section 22.1). The diagnostic log is off by default; `onefold config set diag-log on` (or `ONEFOLD_DIAG=1`, for when the config itself is broken) appends to `onefold-gui.log` next to the exe (fallback `%LOCALAPPDATA%\onefold\onefold-gui.log`): start lines (version, arguments, console processes, parent process, handle types), the launch decision (`launch: GUI (ownsConsole=... parentExplorer=...)` or `launch: no arguments from a terminal -> usage`), fatal exits, GUI start, WebView2 runtime version, window lifecycle, the first 200 HTTP requests, panics with their stack. A double-click that "does nothing" is diagnosed there: no log line at all means an old build or a crash before `main`.

### 25.3 Window

The GUI window is WebView2 (`jchv/go-webview2`), with the exe icon, sized for the system DPI and clamped to 90 % of the screen. The first navigation can be refused for a few seconds on a just-bound loopback port; the window navigates again every 2 s until the page loads (at most 5 minutes). The WebView2 profile lives in the state dir so the page's storage survives restarts.

### 25.4 Exe resources

`cmd/onefold/rsrc_windows_amd64.syso` (committed, generated by `internal/winres` with `go generate ./cmd/onefold`) is a COFF object the Go linker picks up for windows/amd64 builds. It holds:

- the application icon (the GUI logo, rasterized in Go at 16 to 256 px, so no image tool is needed);
- the application manifest: per-monitor DPI awareness (v2), long-path awareness, `asInvoker`, Windows 10/11 compatibility;
- in release builds only, `VS_VERSIONINFO`: `make windows` copies the committed file aside, runs `gen_winres.go -version $(VERSION)`, builds, and restores the committed file (also when the build fails). The fixed file version is the tag's `major.minor.patch.0` (0.0.0.0 for a build that is not a tag); the strings (FileVersion, ProductVersion) carry the full version; Explorer and Task Manager show them.

`TestSysoInSync` compares the committed object with a fresh build by content (manifest bytes, decoded icon pixels), so a toolchain change alone does not require regenerating it.

### 25.5 Antivirus

Microsoft Defender scans each file the first time it is opened. On a first catalog of thousands of files this dominates: measured on 5,384 program files (2.0 GB), the first run took 21.5 s with 114 s of cumulative `open` time (average 21 ms, max 12.7 s), a second run 0.25 s. onefold opens each small file once (whole-read path, preloaded bytes for every extractor). Excluding the onefold process from real-time scanning is the user's decision, documented in the README; onefold never changes it.

### 25.6 Recycle Bin, drives, WSL

- `purge-sources -trash` only on local fixed drives (section 18.3).
- Writing targets on WSL's Windows mounts (`/mnt/<drive>`) is unreliable for large files and for ffmpeg's mp4 muxer (it seeks back): materialize to a native filesystem and transfer, or run the Windows exe.
- The loopback between WSL and Windows differs per machine: some Windows processes started from WSL cannot reach 127.0.0.1 listeners, so some GUI HTTP tests skip there.

---

## 26. Command-line reference

Go's `flag` package: `-x` and `--x` are equivalent; flags go before positional arguments. Sizes accept bytes or binary `K`, `M`, `G`, `T` suffixes with an optional `B`.

### 26.1 Commands

| Command | Purpose | Writes |
|---|---|---|
| `catalog [flags] ROOM...` | index rooms into a manifest | `-o` (default `manifest.jsonl`) |
| `plan [flags] MANIFEST...` | dedup, winners, canonical names | `-o`, `-report` |
| `apply [flags] PLAN` | dry run, or `--execute --target D` | D |
| `export-text --target T PLAN` | text sidecars of documents | T |
| `golden-index -o INDEX [-manifest M]... PLAN...` | index the golden tree from its applied plans | INDEX |
| `match -index INDEX [-o SUB] [-report F] MANIFEST...` | new files against the tree | SUB, F |
| `similar [flags] PLAN` | look-alike photos, same-text documents | `-o` |
| `reclaim -target G [flags] PLAN` | verified drops, deletion script | `-o`, `-script` |
| `backup [-execute] SRC DST`, `backup -check DST` | cold copy and audit | DST |
| `seed -from S -to D -o F [flags] PLAN` | rsync/robocopy script | F, list |
| `push [flags] LOCAL REMOTE` | SFTP upload | remote |
| `merge [flags]` (or `--input .. --output ..`) | fold inputs into an output | output |
| `purge-sources -from REPORT [flags]` | delete merged sources | deletes inputs |
| `rearrange [flags] ROOT` | reorganize in place | ROOT |
| `naming` | naming grammar, presets, placeholders | - |
| `config [list\|get\|set\|unset\|path]` | user configuration | config file |
| `doctor` | platform, zone, tools | - |
| `tools [list\|install all\|ID...]` | pinned tool downloads (Windows) | state dir |
| `gui [flags]` | the GUI | state dir |
| `hash FILE`, `index DIR`, `version`, `help` | debugging and info | - |

### 26.2 catalog flags

| Flag | Default | Meaning |
|---|---|---|
| `-o FILE` | `manifest.jsonl` | output manifest |
| `-host LABEL` | machine name | host label of every record |
| `-j N` | CPUs | workers |
| `-preset LIST` | `media` | presets (section 7.3); wider than photos and videos needs `-allow-mixed` |
| `-ext LIST`, `-category LIST` | - | explicit extensions (replace presets); add categories |
| `-allow-mixed` | off | lift the regime check |
| `-skip RULES`, `-keep RULES`, `-no-sweep` | - | skip rules (section 11.2) |
| `-skip-system-dirs`, `-skip-cloud-dirs`, `-skip-dev-dirs`, `-skip-system-ext`, `-skip-unknown-ext`, `-skip-noext` | on | older boolean forms of the rules |
| `-keep-dot-dirs`, `-keep-junk` | off | descend into dot folders; keep OS droppings |
| `-exclude S`, `-exclude-path P`, `-exclude-glob G`, `-prune NAMES`, `-prune-from FILE` | - | exclusions |
| `-min-size`, `-max-size` | -, `50G` | size bounds (`0` = no cap) |
| `-limit N` | 0 | at most N files per room |
| `-reuse OLD` | - | reuse unchanged records |
| `-quick` | off | generic kinds: file name and filesystem time only |
| `-whole-max SIZE` | `320K` | one-read threshold |
| `-cov`, `-s`, `-blocks` | 5, 4096, 8 | sparse hash parameters (part of identity) |
| `-perf`, `-perf-out FILE` | off | telemetry |

### 26.3 plan, apply, export-text

| Command | Flag | Default | Meaning |
|---|---|---|---|
| plan | `-o FILE` | - | plan |
| | `-layout NAME` | `dedupe` | naming of non-media files |
| | `-verify-full` | off | full-content check of duplicate groups |
| | `-fix-ext` | off | content extension for non-media files |
| | `-video-tier` | off | resolution tier folders |
| | `-video-tolerance SEC` | 0 | video duration tolerance |
| | `-filename-seq` | off | keep date-only file-name counters |
| | `-self-contained` | on | winner metadata in each action (`=false`: historical fields only) |
| | `-artist NAME` | config | owner; `-` = none |
| | `-report FILE`, `-extensive` | - | report |
| apply | `--execute --target D` | dry run | materialize |
| | `--only-host LABEL` | all | actions of one host |
| | `--with-stats` | off | bytes per host and folder in the dry run |
| | `-j N` | CPUs | photo and generic workers (videos serial) |
| | `-signature TEXT`, `-lang CODE` | config | silver tags; `-` = none |
| | `-allow-unstamped` | off | start without exiftool/ffmpeg |
| | `--heartbeat D` | 10s | log-mode interval |
| export-text | `--target T`, `--only-host`, `-j` | | sidecars |

### 26.4 merge, rearrange, purge-sources

Covered in sections 17.3, 19 and 18. Shared: every catalog filter flag (default `-preset all`), `-naming`, `-name`, `-settings`, `-print-settings`, `-list`, `-report`, `-allow-mixed`, `-perf`. rearrange adds `-dupes delete|report`, `-keep-empty-dirs`, `-no-cache`, `-move-j`, `-resume J`, `-undo J`. purge-sources: `-from`, `-execute`, `-expect N`, `-trash`, `-list`, `-j`, `-heartbeat`.

### 26.5 Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | runtime failure: a file failed (merge `missing after run` > 0, rearrange or purge failures, export-text errors, push failure or conflicts, apply failed actions, backup conflicts or an unclean audit) |
| 2 | usage errors, invalid flags or config, regime refusals, a purge confirmation mismatch, `apply --execute` without the tools |

### 26.6 Environment variables

| Variable | Effect |
|---|---|
| `ONEFOLD_STATE_DIR` | state dir (GUI, tools, config) |
| `ONEFOLD_NO_EXTERNAL=1` | disable every external tool |
| `TZ` | canonical zone when config `timezone` is unset |
| `ONEFOLD_DIAG=1` | Windows diagnostic log |
| `SSH_AUTH_SOCK` | ssh-agent for `push` |
| `LOCALAPPDATA`, `XDG_STATE_HOME` | state dir defaults |
| `ONEFOLD_BIN` | tests: the binary to test (cross-compiled Windows exe) |
| `ONEFOLD_TEST_COPY_DELAY` | tests: a delay before each merge copy |
| `ONEFOLD_TEST_TRASH=1` | tests: allow the Windows Recycle Bin test |
| `E2E_ROOT`, `DEMO_ROOT` | GUI tests and demo: scratch folders |

---

## 27. Testing

### 27.1 Suites

`make test` (= `go test ./...`) must stay green. Highlights per package:

| Package | Tests |
|---|---|
| `kinds` | registry loading, presets, sniff, frozen media kinds and canonical aliases, `TestChokePoint` (no extension knowledge outside `kinds`) |
| `meta` | per-format extraction, the real-file corpus (`TestCorpus`), parity with and without tools, photo formats with sub-seconds, the JPEG date fallback, RAR5 listing (native == unrar), EBML/FLV parity with ffprobe, exiftool readers == one-shot, zone resolution |
| `sparsehash` | determinism, small files, size sensitivity, whole-read == seek |
| `catalog` | default scope, rules and reasons, exclusions, sizes and limits, hosts, reuse, sources untouched, whole-read equivalence, regime scopes |
| `plan` | winner orders, distinct content never dropped, collisions, layouts, extension fixes, video tiers, verify-full, unreadable members, strict reader, `TestMediaSynthRegression`, `TestGenericPlanRegression`, self-contained actions, video tolerance, filename counters |
| `naming`, `names` | grammar, stems, presets, `Assign`, families and stem modes, marker grammar, sanitize |
| `golden` | match against a golden built from room A of the synthetic corpus, upgrades, joins with manifests, strict loading, byte keys, video tolerance; `BenchmarkLoad` |
| `materialize` | generic copies, stale temps, missing tools, photo stamping on every photo format, batch refusals, video silvering (UTC creation time, tags, streams, AVI, refused files), byte-copied media |
| `merge` | dry run writes nothing, execute, overlaps, sparse collisions never lose data, blocked parents, no-replace rename, layouts, long paths, settled and excluded lists, re-plan == preview, pause/stop hooks, `-verify-uncached`, `-keep-dotfiles` |
| `rearrange` | end to end, interrupted runs, duplicate re-verification, presets, stop then recover, refusals |
| `purge` | purge, refusals, output inside an input, report reading, journal required, Recycle Bin volume rules |
| `similar`, `reclaim`, `backup`, `seed` | review lists, scripts (the sh script is executed; on Windows the robocopy script is executed), audits |
| `textexport` | extractors, sidecars, Office 97-2003 files saved by Microsoft Office, SST continuation, Word fields |
| `gui` | section 22.9; on Windows the WebView2 fallback |
| `remote` | SFTP push against an in-process SSH/SFTP server, including size conflicts |
| `winres` | `.syso` in sync, layout, logo pixels, version info |
| `cmd/onefold` | CLI end-to-end (`TestE2E` on a synthetic two-host corpus, `TestCorpusFlow`, merge, rearrange, desktop archive with purge, settings round trip, the golden delta flow with similar, reclaim, backup and seed), `TestSourceHeaders` |

### 27.2 Fixtures

- `testdata/fixtures/`: a real-file corpus (documents, sheets, archives, media, text, XMP). Never edit the files; update the expectations.
- `testdata/media-synth/`: synthetic photos and videos (section 12.8).
- `internal/meta/testdata/photo/`: tiny photos in every photo format with EXIF, JPEGs with only `CreateDate` or `ModifyDate`; `internal/meta/testdata/archive/sample5.rar`: a RAR5 archive built from the format by `testfix.RAR5` and accepted by unrar and 7-Zip.
- `internal/textexport/testdata/`: `memo.doc`, `sheet.xls`, `slides.ppt` saved by Microsoft Office from made-up content (`scripts/gen-office-fixtures.ps1`, personal information removed).
- `internal/plan/testdata/generic-synth.jsonl`: synthetic records of every generic kind.
- `internal/testfix`: builders of zip, tar, docx, odt, rtf, pdf, png, RAR5 fixtures and files with a given mtime.

Tests and experiments use synthetic or non-personal data only, in scratch folders.

### 27.3 What the end-to-end tests need

`TestE2E` needs exiftool, ffmpeg and ffprobe; without them it skips, and CI fails on a skip. GUI end-to-end needs Node and a Chromium; `make web-e2e`.

### 27.4 Windows-native runs

For any change to paths, renames, times, console output, merge or fsx: build `GOOS=windows go test -c -o x.test.exe ./internal/<pkg>`, run it on Windows from the package folder (tests read `testdata` relative to it); for tools-dependent tests put the pinned tools first on PATH (`onefold tools install all` with a scratch `ONEFOLD_STATE_DIR`); for the CLI tests set `ONEFOLD_BIN` to a Windows build of the exe.

### 27.5 Time zones

Run the suite with `TZ=America/New_York go test ./...` and `TZ=UTC go test ./...` after any change to time handling.

---

## 28. Changing onefold safely

### 28.1 Conventions

- Every source file starts with the 4-line header (project, author, copyright, `SPDX-License-Identifier: AGPL-3.0-or-later`); `TestSourceHeaders` enforces it.
- Comments say why, in full; they do not reference planning notes outside the repository.
- No extension literals or extension switches outside `internal/kinds` (`TestChokePoint`).
- Every external command through `internal/tools`; every no-clobber publish through `fsx.RenameNoReplace`; every time string parsed in `meta.Zone()`.
- gofmt, `go vet` for Linux and Windows (`GOOS=windows go vet ./...`); CI checks both, the test suite does not.
- Code, comments, docs and commit messages in English; commits are one line, `<feat|fix|chore|test|docs>: <subject>`, at most 72 characters.

### 28.2 Recipes

**Support a new file extension.** Add a row to `internal/kinds/types.tsv` (kind, category, group, format, canonical, flags, description). If its format needs metadata, add or extend a reader in `internal/meta` keyed on the format, with a fixture and a test. A new photo or video extension changes the golden scope: that is a frozen contract (section 3.6); it needs a deliberate decision and updated pinned tests.

**Add a placeholder or a naming preset.** Placeholders: compute the value in `naming.VarsOfCtx`, list it in `Placeholders`, test it. Presets: add it to `naming.Presets` with its doc string (printed by `onefold naming`); generic presets that should be offered per family go into `GenericPresets` and the GUI's mirror in `internal/gui/web/src/lib/regime.js`. Never change a `library*` preset (frozen).

**Add a CLI command.** A `cmdX` function (in `cmd/onefold`), a `case` in `main`, a line in `usage`, and the command in `timedCommands` if it reads or writes times (so the config and zone are applied). Results to stdout, progress through `report`. An end-to-end step in `cmd/onefold/e2e_test.go`. Update `README.md`, `LLMS.md` and this file.

**Change merge or rearrange options.** Add the field to `internal/settings` (JSON tag), map it in `Options`, render it in `Args` (round trip test), add the CLI flag, and, if the GUI should offer it, the form field and the server validation.

**Change the GUI.** Edit `internal/gui/web/src`, run `make web`, commit `dist`, run `make web-e2e`. Server changes need `go test ./internal/gui`.

**Change a frozen rule (media scope, naming, winners, golden presets).** Do not change the default. Add an option whose default keeps the pinned tests green (as `-video-tolerance`, `-filename-seq`, `photo-date-fallback` do), document the effect on existing archives, and test both settings.

**Delete something.** Only the existing deleters may delete (rearrange duplicates, purge, onefold's own temps), and always after re-reading the bytes that justify it, journaled, behind a dry run and an explicit confirmation. New deletion paths need the same treatment and an explicit design decision; prefer writing a script for the user (as `reclaim` does).

### 28.3 Before saying done

`make test`; `gofmt -l .` empty; `go vet` for both OSes; the Windows test binaries of the touched packages when paths, renames, time or console output changed; `make web-e2e` when the GUI, the run control, the report sink or the engine hooks changed; the time-zone runs when times changed; the docs (`README.md`, `LLMS.md`, this file) updated when commands, files or guarantees changed.

---

## 29. Known limits

- The manifest record order is not deterministic (parallel workers); the planner's "room order" for generic winners is the order rooms first appear, which within one manifest can in rare cases differ from the command-line order.
- `-limit` keeps the first N files in walk order, which is not deterministic.
- Photos without a sub-second are identified by their bytes: a resized copy of such a photo is kept as its own asset.
- Upgrades found by `match` are reported only; replacing a golden file is not supported (the tree only grows).
- An unsilvered copy stays unsilvered: a tool installed later does not stamp files that already exist.
- `push` compares remote files by size only: a same-size file with different content is kept as is.
- `similar` cannot decode HEIC, RAW, WebP or TIFF (no pure-Go decoder); `-docs` needs `pdftotext` for PDFs.
- Two `apply` runs on the same target at the same time (CLI and GUI together) could interfere through deterministic temp names; one run per target.
- The rearrange journal stores absolute paths: a moved or remounted tree cannot be resumed or undone in place.
- The Windows double-click detection with Windows Terminal as the default terminal is written for that case but not yet checked by hand (`TODOS.md`).
- macOS builds are not produced; the code compiles, but the state dir and the browser opener are not adapted to macOS.
