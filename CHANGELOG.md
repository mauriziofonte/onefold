# Changelog

All notable changes to onefold are listed here, newest first. Versions follow [Semantic Versioning](https://semver.org): `MAJOR.MINOR.PATCH`, with a pre-release suffix (`-alpha`, `-beta.1`, `-rc.1`) before a version is declared stable. While the major version is 0, any release may change commands, flags or file formats; the file formats and naming rules that decide the names in an existing archive are kept stable anyway, and a change to them is called out here.

The version being prepared is in the `VERSION` file; a release is the tag `v<VERSION>` (see section 5.4 of `ARCHITECTURE.md`).

## [Unreleased]

- Fixed: a folder named through a symbolic link or a Windows short name (`C:\Users\JOHNDO~1`) and through its real name was treated as two folders: an output not created yet inside an input could go unnoticed, `-exclude-path` and GUI exclusions typed through a link did not apply, and `purge-sources` refused every file when the output was named differently from the report. Paths are now compared in one canonical spelling.
- `apply -execute -link`: a byte-original golden tree made of hard links of the sources (same volume, no copy, no stamping, sources untouched).
- Security: `golang.org/x/crypto` v0.58.0 (SSH and SFTP push), `golang.org/x/text` v0.43.0 and `github.com/ulikunitz/xz` v0.5.15 (7z listing) replace versions with known advisories; building from source now needs Go 1.26 or newer.

## [0.0.1-alpha] - 2026-10-31

First public pre-release.

- Merge folders (`merge`): every distinct file of several folders copied once into an output, named by presets or templates per kind of file, verified copies, never overwriting; optional verified deletion of the merged sources (`purge-sources`, Recycle Bin on Windows).
- Reorganize a folder in place (`rearrange`): journaled renames and moves, duplicates confirmed on their full content, resume and undo.
- Photo and video archive (`catalog`, `plan`, `apply`): indexes made where the files live, duplicates and variants of the same shot found across machines, one winner per shot and format, canonical names, capture date, device, owner and comment written into the copies.
- Growing an archive without indexing it again (`golden-index`, `match`), review lists of look-alike photos and same-text documents (`similar`), verified deletion scripts for leftover copies (`reclaim`), a cold-storage copy with an integrity check (`backup`), transfer helpers (`push` over SFTP, `seed` for rsync or robocopy), plain-text copies of documents (`export-text`).
- Local GUI (Windows window, browser elsewhere): every mode with previews, plan review and exclusions, pause, resume and resumable runs, a guided setup, English and Italian.
- Pure-Go readers for every supported format; optional external tools, downloadable on Windows with pinned checksums (`onefold tools install all`).
