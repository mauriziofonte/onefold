## What and why

<!-- What this changes for users or contributors, and why. Link the issue it closes. -->

## Checklist (ARCHITECTURE.md 28.3)

- [ ] I signed the CLA (the bot asks on the first pull request)
- [ ] `make test` green; `TestE2E` ran (not skipped) where I could run it
- [ ] `gofmt -l .` empty; `go vet` for `GOOS=linux`, `windows`, `darwin`; `make lint` clean
- [ ] Paths, renames, time, Unicode or console output changed: tested on Windows / macOS, or said below where only CI ran it
- [ ] GUI, run control, report sink or engine hooks changed: `make web`, `make web-e2e`
- [ ] No new way to delete, move or overwrite user files; frozen-contract tests untouched and green
- [ ] Docs true in this change: `README.md`, `LLMS.md`, `ARCHITECTURE.md`, `CHANGELOG.md` (`## [Unreleased]`)
- [ ] Tests use synthetic data only

## How I verified it

<!-- Commands run, OSes, anything left unproven. -->
