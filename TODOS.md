# TODOS

Open work that needs a person (a decision, administrator rights, a desktop, or a tool install). Each item is self-contained: read it, read the files it names, do it. General project rules for agents: `LLMS.md`. Run `make test` before and after any change; it must stay green.

Common facts:

- onefold is a Go CLI + local GUI that dedups photo/video archives and other files and builds a canonical "golden" tree (`README.md`).
- Hard rules (`LLMS.md`): sources are read-only (only exceptions: `rearrange` duplicate deletion, `purge-sources`); the media naming and winner rules are frozen and pinned by `TestMediaSynthRegression` (`internal/plan/synth_test.go`) and `TestGenericPlanRegression`; external transport binaries (rsync, ssh) are never executed by onefold; `go 1.22` is the language directive and pinned dependencies are only bumped on purpose.
- Windows-native is the primary target: changes touching paths, rename, time or console must also pass the Windows test binaries (`GOOS=windows go test -c ./<pkg>` run on Windows from the package directory).

| ID | Item | Blocked on | Effort once unblocked |
|---|---|---|---|
| T1 | Bump dependencies flagged by govulncheck | owner decision (may raise the `go` directive) | small to medium |
| T2 | Byte-original golden profile (`apply-golden`) | owner decision between 3 options | medium (option B) to large (option A) |
| T3 | Split the first-catalog cost between Microsoft Defender and the OS cache | Windows administrator rights, reboots | small (measurement + docs) |
| T4 | Check the double-click launch with Windows Terminal as default terminal | a person at a Windows desktop | small (manual check) |
| T5 | README demo GIFs of the CLI | install `vhs` and `ttyd` | small to medium |

---

## T1. Bump the dependencies govulncheck flags

**State.** CI has a job `vulncheck` in `.github/workflows/ci.yml` that runs `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...` with `continue-on-error: true` (report only, the run stays green). A local run on 2026-10-31 found 13 vulnerabilities whose vulnerable code is reachable from onefold:

| Module | Pinned (`go.mod`) | Fixed in | Reached through |
|---|---|---|---|
| `golang.org/x/crypto` | v0.31.0 | >= v0.56.0 (11 advisories, GO-2025-3487 ... GO-2026-6355) | `internal/remote` (SSH/SFTP push: `ssh.Dial`, `sftp.NewClient`) |
| `golang.org/x/text` | v0.21.0 | >= v0.39.0 (GO-2026-5970) | text normalization |
| `github.com/ulikunitz/xz` | v0.5.12 (indirect, via `github.com/bodgit/sevenzip`) | >= v0.5.15 (GO-2025-3922) | 7z listing (`internal/meta`), pinned tool install (`internal/tooldl`) |

**Why blocked.** The project pins `go 1.22` and its dependencies on purpose; newer `golang.org/x/*` releases may require a newer `go` directive in their own `go.mod` (not verified). Raising the directive or a pin is the owner's call.

**Decision needed.** Bump (recommended) and accept a newer `go` directive if required, or keep the pins and accept the advisories.

**Steps once decided.**

1. `go get golang.org/x/crypto@latest golang.org/x/text@latest github.com/ulikunitz/xz@latest` (or the minimum fixed versions above), `go mod tidy`.
2. If `go.mod`'s `go` line had to change, check every place that names the Go version: `go.mod`, `.github/workflows/ci.yml` and `release.yml` (`go-version`), `LLMS.md`, `README.md` build section.
3. `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...` must report no reachable vulnerability.
4. `make test`, `GOOS=windows go vet ./...`, Windows test binaries of `internal/remote`, `internal/meta`, `internal/tooldl`.
5. Remove `continue-on-error: true` from the `vulncheck` job (and its comment) so CI fails on new advisories; check the workflow with actionlint and zizmor (`scripts/check-workflows.sh` does both).
6. Regenerate `THIRD_PARTY_NOTICES.txt` content check: `make notices` must succeed (it reads the licenses of the new versions).

**Done when.** govulncheck is clean, `vulncheck` is a blocking CI job, all tests green on Linux and Windows.

---

## T2. Byte-original golden profile (`apply-golden`)

**State.** `apply --execute --target D <plan>` builds the golden tree as copies: each winner is copied under its canonical name and photos/videos are "silvered" (canonical date, device, owner, comment written into the copy with exiftool/ffmpeg). The original design also had a profile that keeps the original bytes. Nothing is implemented.

**Why blocked.** The obvious version moves source files, which breaks the rule "sources are read-only", and the golden would no longer hold silvered copies. Both are owner decisions.

**Options (decide one).**

| Option | What it does | Sources | Extra disk | Main risk |
|---|---|---|---|---|
| A. Move | `apply-golden --execute --target D`: per winner, rename within the volume (copy + verify + delete across volumes) to `D/<canonRel>`, journaled like `rearrange` (`.onefold/apply-golden-<ts>.jsonl`, `-resume`, `-undo`); optional `-backup B` copies and verifies each winner first | moved (a new exception to read-only sources) | none on the same volume | a bug moves user data; undo depends on the journal |
| B. Hard links (recommended) | `apply --execute --link --target D`: one hard link per winner at `D/<canonRel>` (same volume only, `os.Link`, refused across volumes), no silvering, mtime untouched | untouched (names added in the target only) | none | golden and source share bytes: editing either changes both; the consumer (e.g. a photo library) must treat the tree as read-only |
| C. Status quo | silvered copies only | untouched | one copy | none |

**Steps for B.**

1. `internal/materialize/materialize.go` `ApplyWith`: a link path (`os.Link(a.SrcPath, tmpName)` then the existing no-replace publish, or `os.Link` straight to the final name with `fsx.ErrExists` handling); skip silvering and do not count `unsilvered` (byte-original by request).
2. `cmd/onefold/main.go` `cmdApply`: flag `-link`; refuse it with a clear error when source and target are on different volumes (`os.Link` returns `EXDEV` / `ERROR_NOT_SAME_DEVICE`); the missing-tools refusal (`materialize.MissingTools`) does not apply with `-link`.
3. GUI (optional): `internal/gui/runners.go` `ApplyParams.Link` + a checkbox in `internal/gui/web/src/views/Golden.svelte`, then `make web` and `make web-e2e`.
4. Tests: link created, same inode (Linux `syscall.Stat_t.Ino`, Windows file index), source untouched, idempotent re-run, cross-volume refusal; Windows test binary on NTFS.
5. Docs: `README.md` workflow section, `LLMS.md`.

**Steps for A.** Write down the new exception to "sources are read-only" first (README first paragraph, `LLMS.md` guarantees), then reuse the journal/undo machinery of `internal/rearrange` and the verified-copy of `internal/merge` (`merge.CopyVerified`); test cross-volume moves on Windows.

**Done when.** The chosen option is implemented and documented, or C is recorded as the decision.

---

## T3. Microsoft Defender vs OS cache on the first catalog (Windows)

**State.** The first `catalog` of files Windows has not read recently is slow because of on-access scanning. Measured on 2026-10-31 without administrator rights (real-time protection on), `onefold.exe catalog -preset all -allow-mixed -no-sweep -j 8 -perf` over `C:\Program Files\Microsoft Office\root\Office16` (5384 files, 2.0 GB): first run 21.5 s wall, `open` 114 s cumulative (average 21 ms, max 12.7 s); second run 0.25 s, `open` average 0.06 ms. The README documents the optional process exclusion (`Add-MpPreference -ExclusionProcess`) as the user's choice; onefold never changes Defender settings.

**Why blocked.** Telling Defender's cost apart from the OS file cache needs the exclusion on and off and cold caches: administrator rights and reboots.

**Steps (administrator PowerShell, a non-personal tree such as the one above).**

1. Reboot (cold cache). Run the catalog command above with `-perf`; save the output.
2. `Add-MpPreference -ExclusionProcess "<full path>\onefold.exe"`.
3. Reboot. Run the same command; save the output.
4. `Remove-MpPreference -ExclusionProcess "<full path>\onefold.exe"`.
5. Compare the `open` lines (count, cumulative seconds, average, max) and the wall time.

**Done when.** The two measurements are recorded next to the existing one in the README paragraph about Defender (one sentence: the share of the first-run cost due to scanning), no code change expected.

---

## T4. Double-click launch with Windows Terminal as default terminal

**State.** With no arguments, `cmd/onefold/main.go` `main` opens the GUI when the exe was started by a double-click and prints the usage when started from a terminal. The test is `ownsConsole()` (`cmd/onefold/launch_windows.go`: only this process is attached to the console) or `parentIsExplorer()` (`cmd/onefold/diag_windows.go`); then `detachConsole()` (`FreeConsole`) closes the console window. With Windows Terminal as the default terminal, Windows hosts the console in a Terminal tab; the code is written for it but was never checked by a person. The WebView2-missing fallback to the browser is proven by `TestWindowFallsBackToBrowser` (`internal/gui/window_windows_test.go`); this item is only about the launch.

**Why blocked.** It needs a person at a Windows desktop: a real double-click opens a GUI window that stays open.

**Steps.**

1. Windows Settings > Privacy & security > For developers > Terminal: "Windows Terminal".
2. `onefold.exe config set diag-log on` (writes `onefold-gui.log` next to the exe).
3. Double-click `onefold.exe` in Explorer. Expected: the GUI window opens; the Terminal tab closes (or shows the process ended); `onefold-gui.log` has a line `launch: GUI (ownsConsole=... parentExplorer=...)`.
4. Close the GUI. Open Windows Terminal, run `onefold.exe` with no arguments. Expected: the usage text, no GUI; the log has `launch: no arguments from a terminal -> usage`.
5. Repeat 3-4 with Terminal = "Windows Console Host".
6. `onefold.exe config set diag-log off`.

**Done when.** All four launches behave as expected; if one does not, the log lines tell which test (`ownsConsole`, `parentIsExplorer`) misfired: fix it in `main.go` and record the case in a comment.

---

## T5. README demo GIFs of the CLI

**State.** The GUI demo exists: `make web-demo` (`scripts/gui-demo.sh`, `internal/gui/web/demo/merge.spec.js`, `internal/gui/web/playwright.demo.config.js`) drives the real binary on a made-up sandbox and writes `assets/demo/gui-merge.gif`, shown in `README.md`. CLI GIFs are missing.

**Why blocked.** They are recorded with `vhs` (charmbracelet), which needs `ttyd` and `ffmpeg`; `vhs` and `ttyd` are not installed on the workstation.

**Steps once installed.**

1. Create `scripts/tapes/` with one `.tape` per GIF and a hidden setup that makes a made-up sandbox in `/tmp` with fixed dates (never personal folders); a `render.sh` that builds `onefold` from the checkout, puts it on `PATH` for the tapes and runs `vhs` on each tape into `assets/demo/`.
2. Suggested tapes: `merge.tape` (`onefold merge -input A -input B -output O`, then `-execute`), `golden.tape` (`catalog` -> `plan` -> `apply --with-stats` -> `apply --execute`), `match.tape` (`golden-index` -> `match` -> `apply` the sub-plan).
3. In each tape `Wait` for the prompt before typing, `Sleep` after results; keep each GIF under about 1 MB (width 960-1200, a short run).
4. Add a `make cli-demo` target, reference the GIFs in `README.md` next to the commands they show.
5. Optional, once the repository is on GitHub: a workflow with `charmbracelet/vhs-action` (pinned by commit) that renders on changes and uploads the GIFs as an artifact.

**Done when.** The GIFs are in `assets/demo/`, linked from the README, reproducible with one make target.
