# TODOS

Open work that needs a person (a decision, administrator rights, a desktop, or a tool install). Each item is self-contained: read it, read the files it names, do it. General project rules for agents: `LLMS.md`. Run `make test` before and after any change; it must stay green.

Common facts:

- onefold is a Go CLI + local GUI that dedups photo/video archives and other files and builds a canonical "golden" tree (`README.md`).
- Hard rules (`LLMS.md`): sources are read-only (only exceptions: `rearrange` duplicate deletion, `purge-sources`); the media naming and winner rules are frozen and pinned by `TestMediaSynthRegression` (`internal/plan/synth_test.go`) and `TestGenericPlanRegression`; external transport binaries (rsync, ssh) are never executed by onefold; dependencies are bumped on purpose only (Dependabot pull requests, merged by a person) and `govulncheck` is a blocking CI job.
- Windows-native is the primary target: changes touching paths, rename, time or console must also pass the Windows test binaries (`GOOS=windows go test -c ./<pkg>` run on Windows from the package directory).

| ID | Item | Blocked on | Effort once unblocked |
|---|---|---|---|
| T4 | Check the double-click launch with Windows Terminal as default terminal | a person at a Windows desktop | small (manual check) |

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
