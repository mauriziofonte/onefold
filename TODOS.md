# TODOS

Open work that needs a person (a decision, administrator rights, a desktop, or a tool install). Each item is self-contained: read it, read the files it names, do it. General project rules for agents: `LLMS.md`. Run `make test` before and after any change; it must stay green.

Common facts:

- onefold is a Go CLI + local GUI that dedups photo/video archives and other files and builds a canonical "golden" tree (`README.md`).
- Hard rules (`LLMS.md`): sources are read-only (only exceptions: `rearrange` duplicate deletion, `purge-sources`); the media naming and winner rules are frozen and pinned by `TestMediaSynthRegression` (`internal/plan/synth_test.go`) and `TestGenericPlanRegression`; external transport binaries (rsync, ssh) are never executed by onefold; dependencies are bumped on purpose only (Dependabot pull requests, merged by a person) and `govulncheck` is a blocking CI job.
- Windows-native is the primary target: changes touching paths, rename, time or console must also pass the Windows test binaries (`GOOS=windows go test -c ./<pkg>` run on Windows from the package directory).

| ID | Item | Blocked on | Effort once unblocked |
|---|---|---|---|
| T3 | Split the first-catalog cost between Microsoft Defender and the OS cache | Windows administrator rights, reboots | small (measurement + docs) |
| T4 | Check the double-click launch with Windows Terminal as default terminal | a person at a Windows desktop | small (manual check) |
| T5 | README demo GIFs of the CLI | install `vhs` and `ttyd` | small to medium |

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
