# LLMS.md: onefold for coding agents

onefold is a single static Go binary that deduplicates photo/video archives and generic files spread over several hosts, elects one winner per asset and materializes a canonical-named "golden" tree (photos/videos re-stamped with canonical metadata, everything else byte-copied). It also merges folders (`merge`), deletes the merged sources afterwards on request (`purge-sources`) and reorganizes a tree in place (`rearrange`). Human docs: `README.md`. How every part works, in depth (file formats, dedup and naming rules, modes, GUI, platform, tests, safe-change recipes): `ARCHITECTURE.md`; read it before a non-trivial change. Open work needing a person: `TODOS.md`.

## Build and test

| Task | Command |
|---|---|
| test (must stay green) | `make test` (= `go test ./...`) |
| vet | `make vet` (CI vets `GOOS=linux`, `windows`, `darwin`) |
| lint, race | `make lint` (staticcheck, pinned, every target OS), `make race` (`go test -race`, needs cgo) |
| build | `make build` -> `./onefold`; `make linux`, `make windows`, `make linux-arm64` -> `dist/`; `make release` = the three + `THIRD_PARTY_NOTICES.txt` (`make notices`) + `SHA256SUMS` (`DIST=<dir>` to build elsewhere); `dist/` and `./onefold` are gitignored build outputs |
| Windows resources | `make winres` (= `go generate ./cmd/onefold`) rewrites the committed `cmd/onefold/rsrc_windows_amd64.syso` (icon + manifest); `TestSysoInSync` fails when it is stale |
| smoke | `./onefold version`, `./onefold doctor` |
| version | `VERSION` (SemVer without `v`) is the single source; `make version` prints what a build gets: `v<VERSION>` on a clean checkout of tag `v<VERSION>`, else `v<VERSION>+dev.<commit>[.dirty]`. Releasing: bump `VERSION`, add the `## [<version>] - <date>` section to `CHANGELOG.md`, commit, tag `v$(cat VERSION)`, push the tag (`ARCHITECTURE.md` 5.4); `TestVersionFile` checks both files |
| GUI frontend | `make web` (Node, `internal/gui/web/package.json` engines) rebuilds the committed `internal/gui/web/dist`; `make web-e2e` runs the browser end-to-end test |

- CI (`.github/workflows/ci.yml`, also weekly): gofmt, vet for Linux, Windows and macOS, `go test` on Ubuntu (with exiftool/ffmpeg, so the end-to-end test runs), on Windows (tools from `onefold tools install all`) and on macOS arm64 and Intel (Homebrew tools; report-only until macOS parity lands); it fails when `TestE2E` skips. Plus `go test -race` with a coverage report (artifact `coverage`), staticcheck for every target OS, blocking govulncheck. Release (`.github/workflows/release.yml`): a tag `v<VERSION>` (`vMAJOR.MINOR.PATCH` or a pre-release `vX.Y.Z-suffix`) on a commit of `main`, equal to `VERSION` and with a `CHANGELOG.md` section, runs the CI, `make release`, checks checksums, versions and notices, then publishes the GitHub release (a pre-release when the version has a suffix) with the notes of its CHANGELOG section. Actions are pinned by commit; `.github/dependabot.yml` proposes updates of the action pins and of the Go modules. Before pushing a workflow change: `actionlint` (config `.github/actionlint.yaml`), `zizmor --persona=pedantic .github/`, and an `act` run of the Linux jobs.
- Go: `go 1.26.0` directive (raised from 1.22 to take the security fixes of `golang.org/x/crypto`, `golang.org/x/text`, `github.com/ulikunitz/xz`), toolchain pinned in the workflows (`go-version`), `CGO_ENABLED=0` (static, no cgo). Raising the directive or bumping a dependency is still a deliberate change (Dependabot proposes them, a person merges); do not run `go mod tidy` unprompted. `govulncheck` is a blocking CI job: a reachable advisory fails the build.
- External programs (`ffprobe ffmpeg exiftool pdfinfo pdftotext antiword 7z unrar`) are optional and called only through `internal/tools`; every format has a pure-Go reader. `ONEFOLD_NO_EXTERNAL=1` disables them. A missing tool must never fail a run. On Windows x64 `onefold tools install` fetches pinned builds (`internal/tooldl`: URL + size + SHA-256 per version, never "latest") into `<state dir>/tools`, searched after PATH and the exe dir. The E2E test skips without `exiftool ffmpeg ffprobe`: a skip is not a pass.
- Windows-native is the primary target, Linux parity is mandatory: changes to paths, renames, time handling or console output must also pass the tests built with `GOOS=windows go test -c` and run on Windows (E2E: `ONEFOLD_BIN=<windows exe>`). Never add a fallback that silently behaves differently on Windows (time zones: `time/tzdata` is embedded, never fall back to UTC).

## Layout

| Path | Role |
|---|---|
| `cmd/onefold` | CLI: `merge rearrange purge-sources catalog plan golden-index match similar reclaim backup seed apply export-text push naming doctor tools gui hash index version`; `e2e_test.go` builds the binary |
| `cmd/bakeoff` | throwaway benchmark, not product |
| `internal/kinds` | file-type registry: `types.tsv` (one row per extension) + presets, skip lists, magic sniff |
| `internal/meta` | metadata extraction (time, device, dims, title/author...) |
| `internal/catalog` | indexer: rooms -> manifest JSONL; `Filters` = the scope with its defaults, shared by the CLI flags and the GUI; skip rules by name (`Rules`: `-skip`/`-keep`), exclusions by path and pattern, the reason of every excluded file, driver/installer package folders |
| `internal/plan` | dedup + winner election + canonical names -> plan JSONL; full-content verification |
| `internal/golden` | golden index built from the applied plans (+ manifests), never from the tree; incremental `match`: new files planned against it -> dupes, upgrades (reported, never applied), sub-plan of new assets with names free in the tree |
| `internal/similar` | `similar`: look-alike golden photos (difference hash) and, with `-docs`, documents with the same extracted text, listed for a manual review; never drops anything |
| `internal/reclaim` | `reclaim`: verifies the exact-duplicate drops of an applied plan and writes a deletion script for a person to run; deletes nothing itself |
| `internal/backup` | `backup`: cold-storage copy of a tree with a content manifest (only adds, verified copies); `backup -check`: audit by reading every file again |
| `internal/seed` | `seed`: the rsync or robocopy script that copies a materialized staging tree to its destination (never replacing); onefold runs neither |
| `internal/naming`, `internal/names` | naming templates, presets, `_NN` collision resolver, file families and their naming (`Compose`: preset + file-name mode per family, `[folder]` prefix); name primitives |
| `internal/merge`, `internal/rearrange` | merge engine; in-place rearrange with journal, resume, undo |
| `internal/settings` | the merge/rearrange settings schema: GUI job JSON = `-settings` file = what the CLI flags fill; engine options and the equivalent command line |
| `internal/purge` | deletes the merged input files whose content is in the output, then their emptied folders (journaled, Recycle Bin option on Windows) |
| `internal/materialize`, `internal/textexport`, `internal/remote` | golden copy + metadata stamping; text sidecars; pure-Go SFTP push |
| `internal/sparsehash`, `internal/scan` | XXH128 sparse/full digests; `index` throughput probe |
| `internal/report`, `internal/planreport`, `internal/perf` | progress output (stdout = results, stderr = progress); plan report; `-perf` telemetry |
| `internal/gui` | local GUI: HTTP server on 127.0.0.1 (session token), job manager with runs persisted in the per-user state dir, guided setup (quick scan + real preview on a sample, recipes), WebView2 window on Windows / browser elsewhere |
| `internal/gui/web` | Svelte 5 + Vite frontend (English/Italian); `dist/` is committed and embedded, so Go builds never need Node; `TestWebDistInSync` fails when it is stale |
| `internal/runctl` | pause/stop gate the engine waits on between units of work |
| `internal/fsx`, `internal/tools`, `internal/testfix` | no-replace rename; optional tool registry; test fixture builders |
| `internal/tooldl`, `internal/statedir` | pinned downloads of the optional tools (Windows); per-user durable state dir |
| `internal/config` | per-user config `<state dir>/config.json` (`onefold config`, GUI Config view): owner, comment and video stream language stamped on media copies, Windows diagnostic log |
| `internal/winres` | Windows resources of the exe: icon (the GUI logo, rasterized in Go) and manifest (per-monitor DPI awareness, long paths), written as a `.syso` |
| `testdata/fixtures/` | real-file test corpus: never edit the files, update test expectations |
| `testdata/media-synth/` | synthetic photos and videos (`scripts/gen-media-synth.sh`) pinning the media naming and winner rules (`TestMediaSynthRegression`) and the golden match |

Pipeline: `catalog` (on each host, read-only) -> `plan` (anywhere) -> `apply` (dry-run by default; `--execute --target D` on the host holding the sources; refuses to start when exiftool/ffmpeg are missing for the media still to write, unless `-allow-unstamped`) -> optional `export-text`, `push`. Videos whose re-encode shifted the duration: `plan`/`match -video-tolerance SEC` (default 0 = rounded-duration rule); files dated by a name without a time of day: `-filename-seq` keeps the name's counter instead of `_NN`. Later additions: `golden-index` (from the applied plans) -> `match` (new manifests -> sub-plan) -> `apply`. `onefold gui` (or double-clicking the exe on Windows) runs the same modes from a page.

## Invariants (break one = data loss or a broken archive)

- Sources are read-only. Only writers: the manifest/plan/report files, the target tree of `apply`/`export-text`, the `push` destination. One exception: `purge-sources` (and the GUI purge after a merge) deletes the input files of an executed merge whose copy in the output is read again in full and equal right before, journaled in `<output>/.onefold/purge-*.jsonl`, after a typed count (`-expect N`); never anything under the output, never a room root. Second exception: `rearrange -execute` renames inside its root and deletes duplicates confirmed on the full content (keeper and duplicate re-read right before), journaled in `<root>/.onefold/rearrange-*.jsonl` (`-resume`, `-undo`).
- `merge` only adds files to the output: it never moves, renames, overwrites or deletes existing files, and removes only its own `*.onefold-tmp`. Duplicates are skipped only after a full-content check.
- `rearrange` renames within one volume and deletes only duplicates re-verified on the full content right before deletion; every run is journaled first (`.onefold/rearrange-*.jsonl`). `*.onefold-mv` files are data in flight: never delete them.
- Every command that writes defaults to dry-run; writes are atomic (a onefold temp name, then a no-replace rename), idempotent and never replace an existing file; `push` never overwrites a remote file and never uploads onefold temp files.
- A photo or video that cannot be silvered (exiftool/ffmpeg missing or refusing the file) is still copied, byte-faithful, and counted as unsilvered: a later run skips it, so it is never stamped afterwards.
- Identity (hashes) is always computed on the original bytes.
- GUI: a preview writes nothing; execute runs exactly the confirmed preview (a resume only a subset of it); the server listens on 127.0.0.1 only and requires the session cookie; runs live in a durable state dir, never a temp dir.
- Two naming regimes, never mixed: the golden archive (`catalog`/`plan`/`apply`) takes the media scope (preset `media`) and names photos and videos canonically; `merge` and `rearrange` name every file with a naming template, never with preset `media` nor the golden tree presets (`library*`, `-layout` aliases). The GUI and its server refuse a mix; the CLI does too unless `-allow-mixed`. `-naming` takes preset names and templates; only `-layout` takes the historical aliases. `-name FAMILY=PRESET[+clean|+raw|+prefix[:N]]` (families `img video doc music arch rest`) selects the file types and names each family; `rest` includes unknown extensions and files without one. `-settings FILE` reads the same JSON the GUI exports; flags given on the line override it; `-print-settings` prints the result. `-exclude-path P` (a file or a folder with all it holds) and `-exclude-glob G` (pattern on the path inside each input: `*`, `**`, `?`, `[abc]`, no separator = any name) leave files out; the GUI adds them from the rows of a previewed plan and re-plans the job on its cataloged records without scanning again. Folders that look like driver or installer packages are reported (`packages:`), never excluded on their own.
- Media scope = the golden scope: photo kind (jpg jpeg heic heif avif png webp tif tiff, camera RAW) and video kind (every container); one winner per shot and per format; RAW and mpg/mpeg are byte-copied, never stamped.
- Frozen contracts, pinned by tests: media scope, media naming and winner rules (`TestMediaSynthRegression` on the synthetic photos and videos of `testdata/media-synth`, generated by `scripts/gen-media-synth.sh`; `TestMediaPlanRegression`, runs only where a private `census-final/` fixture is present: it is not part of the repository), golden-tree presets (`TestGenericPlanRegression`), photo/video kinds and canonical extensions (`TestMediaKindsFrozen`, `TestCanonicalFrozen`). A red one means the change is wrong, not the test.

## Conventions

- All extension knowledge lives in `internal/kinds/types.tsv` (8 tab-separated columns). No extension literal or extension switch elsewhere (`TestChokePoint`); names of the older tools this code was ported from must not appear in code identifiers, CLI text, README or test names.
- Extraction is best-effort: metadata errors degrade `timeSource`, never abort a catalog.
- Plans and manifests are read strictly (`plan.Read`, `catalog.LoadManifest`): a malformed line fails the command instead of applying a partial plan. Reuse sources (`-reuse`, caches) skip unreadable lines: those files are processed again.
- Plan and manifest times are wall-clock strings in the canonical zone (`meta.Zone()`): parse them there, never in the host zone. The zone is per process: config `timezone`, else `$TZ`, else the host zone (Windows: registry + ICU, by user region), set by `config.Apply` (CLI start, every GUI job). Manifests, caches and plans start with a header line `{"onefold":"manifest|plan","zone":"<IANA>"}`; `catalog.LoadManifest`, `catalog.LoadManifestByPath` and `plan.Read` refuse a missing header or another zone (caches are then rebuilt). Tests that compare times pin the zone (`meta.SetZone`, or `timezone` in the scratch config for binary runs).
- Every manifest record has a host (`catalog -host`, default the machine name); `plan.LoadManifests` refuses records without one. There is no path heuristic.
- Names written from templates are NFC; name comparisons (collisions, case-fold) use `names.FoldKey` (NFC + lower case), never `strings.ToLower` alone. Month names follow config `names-language` (`{month-name}`); file-name dates `XX-XX-YYYY` are read only when unambiguous, else per config `date-order`. A JPEG without `DateTimeOriginal` takes its EXIF `CreateDate`, else `ModifyDate`, only with config `photo-date-fallback` on (default off: file name, then file time; the other photo formats always use `CreateDate`). Copy-name markers (`names.copyWords`) cover the Explorer/Finder strings of the main languages.
- Tests and experiments use synthetic or non-personal data only, in a scratch directory.
- Nothing about a user is hardcoded: owner, comment, language, time zone, date order and month-name language come from `internal/config` (command-line flags override it, `-` clears a value); tests that run the binary set `ONEFOLD_STATE_DIR` to a scratch dir so a developer's own config never leaks in.
- Never commit build outputs (`dist/`, `./onefold`) or personal data (manifests, plans, logs with archive paths).
- License: AGPL-3.0-or-later (`LICENSE`); every source file starts with the project header (name, author, copyright, `SPDX-License-Identifier: AGPL-3.0-or-later`): new files get it too. Dependencies keep their own licenses (`make notices`).
- Comments say why, in full: no references to planning notes outside the repository (decision or contract ids, ADR numbers; `TestSourceHeaders` refuses them).
- Code, comments, docs and commit messages in English. Commits: one line, `<feat|fix|chore|test|docs|ci|build>: <subject>`, max 72 chars.
- Contributors: `CONTRIBUTING.md` (how), `CLA.md` (signed once per contributor through the CLA bot), `SECURITY.md` (private reports), `.github/pull_request_template.md` (the 28.3 checklist of `ARCHITECTURE.md`).
