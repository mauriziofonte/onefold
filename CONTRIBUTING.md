# Contributing to onefold

onefold needs two kinds of help: people who write code with us, and alpha testers who run it on real machines and tell us what broke. Both are listed in [CONTRIBUTORS.md](CONTRIBUTORS.md).

Everything here assumes you have read the [Code of Conduct](CODE_OF_CONDUCT.md). Security problems go through [SECURITY.md](SECURITY.md), never a public issue.

## Testing onefold (no code needed)

1. Download the latest release for your OS, check it (`sha256sum -c SHA256SUMS`), run `onefold doctor`.
2. Try a mode on **copies** of folders, or on folders you have a backup of. The tool never touches sources unless you ask, but an alpha is an alpha.
3. Open an issue with the **Alpha test report** template, even when everything worked: knowing which OS, file systems and archive sizes ran clean is as useful as a bug.

Never attach personal photos, documents or full file listings. Describe the files, or rename paths in log excerpts.

## Contributing code

### Before you start

- For anything bigger than a small fix, open an issue first and say what you plan to do: a short agreement saves a rejected pull request.
- Issues labelled `good first issue` are self-contained and come with pointers.
- Read [LLMS.md](LLMS.md) (two pages: build, layout, invariants, conventions). Before a non-trivial change read the relevant sections of [ARCHITECTURE.md](ARCHITECTURE.md), and always its section 3 (guarantees) and section 28 (changing onefold safely).

### Contributor License Agreement

Every contributor signs the [onefold Individual CLA](CLA.md) once, before the first pull request is merged. A bot comments on your first pull request with a link; signing takes one click with your GitHub account. The CLA lets the maintainer keep the project's licensing options open (onefold is AGPL-3.0-or-later today) while you keep the copyright of your work; every contribution stays available under an open-source license. This is what makes the dual licensing in [LICENSING.md](LICENSING.md) possible.

### Build and test

You need Go (the version in `go.mod` or newer; CI pins the exact toolchain in `.github/workflows/ci.yml`). No C compiler, no Node, unless you change the GUI.

```bash
make build    # ./onefold
make test     # the whole suite; must stay green
make vet      # go vet for this OS
make lint     # staticcheck (pinned) for linux, windows and darwin
make race     # the race detector (needs cgo and a C compiler)
```

For the end-to-end test to run instead of skipping, install exiftool, ffmpeg and ffprobe (`apt install libimage-exiftool-perl ffmpeg`, `brew install exiftool ffmpeg`, or on Windows `onefold tools install all`). A skipped `TestE2E` is not a pass.

GUI changes: edit `internal/gui/web/src`, then `make web` (rebuilds the committed `dist/`) and `make web-e2e` (Playwright).

### Every operating system

Linux, Windows and macOS must behave the same. If your change touches paths, renames, time, Unicode names or console output:

- run `GOOS=windows go vet ./...` and `GOOS=darwin go vet ./...`;
- if you have the machine, run the tests there (on Windows: `GOOS=windows go test -c ./<pkg>`, then run the `.exe` from the package folder);
- CI runs the full suite on Ubuntu, Windows and macOS (Apple Silicon and Intel) for every pull request.

Never add a fallback that silently behaves differently on one OS.

### Rules that protect people's files

A pull request that breaks one of these is not merged, however useful it is:

- Sources are read-only. Only `rearrange -execute` and `purge-sources -execute` delete user files, after re-reading the bytes that justify it, with a journal, a dry-run default and an explicit confirmation. Do not add a deletion path; write a script the user runs instead (as `reclaim` does).
- Writes go to a temporary name and are published with `fsx.RenameNoReplace`, never `os.Rename`: nothing is ever overwritten.
- Frozen contracts (media naming and winner rules, golden tree presets, media kinds) are pinned by tests. A red pinned test means the change is wrong. New behaviour goes behind an option whose default keeps them green.
- Every external program goes through `internal/tools`; all file-type knowledge lives in `internal/kinds/types.tsv`.
- `CGO_ENABLED=0`: static binaries on every OS.
- Tests use synthetic data in temporary directories, never personal files.

### Style

- `gofmt`; staticcheck clean (a suppression is an inline `//lint:ignore CHECK reason`).
- Every source file starts with the 4-line project header (`TestSourceHeaders` checks it).
- Comments explain why. Code, comments, docs and commit messages in English.
- Commits: one logical change each, one line, `<feat|fix|chore|test|docs|ci|build>: <subject>`, at most 72 characters.
- Docs stay true in the same pull request: `README.md` for users, `LLMS.md` (short) and `ARCHITECTURE.md` (deep) for contributors, `CHANGELOG.md` under `## [Unreleased]` for anything a user would notice.

### Pull requests

Fill in the template: it is the checklist of `ARCHITECTURE.md` 28.3. Keep pull requests small and focused; a reviewer should be able to check every changed line. CI must be green on every OS before review.
