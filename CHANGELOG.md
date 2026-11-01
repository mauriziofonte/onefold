# Changelog

All notable changes to onefold are listed here, newest first. Versions follow [Semantic Versioning](https://semver.org): `MAJOR.MINOR.PATCH`, with a pre-release suffix (`-alpha`, `-beta.1`, `-rc.1`) before a version is declared stable. While the major version is 0, any release may change commands, flags or file formats; the file formats and naming rules that decide the names in an existing archive are kept stable anyway, and a change to them is called out here.

The version being prepared is in the `VERSION` file; a release is the tag `v<VERSION>` (see section 5.4 of `ARCHITECTURE.md`).

## [0.0.2-alpha] - 2026-11-01

- The version is shown where people look for it: `onefold --version` and `-V` (with the commit and its date), the first line of `onefold help` and of the GUI's start message, and in the GUI an About card on the Doctor page (version, build, platform, license, source address), reached from the version next to the logo.
- Statistics (`onefold stats`, and the Statistics view in the GUI): what fills whole volumes or chosen folders, by file family, category, extension, year, size and folder, the largest files, and how much space duplicates waste, with the groups of copies and the pairs of folders sharing the most files (one click opens Merge on them). Duplicates are found on sampled content by default, or confirmed on the whole content with `-dups full`. Read-only; `-json` writes everything.
- New logo: an icon (two overlapping squares, their overlap darker: many folders, one copy) and the "onefold" wordmark in DM Sans, in `assets/brand` (SVG, a 512 px PNG and a social preview image), used for the Windows exe icon, the GUI favicon and header, and the README. The GUI accent is a muted teal (`brand` colors in `app.css`) instead of indigo.
- Licensing: onefold stays AGPL-3.0-or-later for everyone and is also offered under a commercial license (`LICENSING.md`). The name and logos are covered by a trademark policy (`TRADEMARKS.md`).
- macOS: `merge -verify-uncached` reads each copy back past the cache (`msync(MS_INVALIDATE)`, then `F_NOCACHE`) instead of falling back to the cached read.
- macOS: `purge-sources -trash` (and the GUI option) moves the merged sources to the Trash, with Finder's Put Back, instead of deleting them. It uses the system Trash API without cgo, through the new dependency `github.com/ebitengine/purego`.
- Linux: `purge-sources -trash` (and the GUI option) moves the merged sources to the Trash of the freedesktop.org specification, which desktop file managers and `trash-cli` restore from, on every distribution.
- macOS: a folder typed in another case or Unicode normalization than the one stored on disk (`/users/me/pics` for `/Users/me/Pics`) is recognized as the same folder by the output-inside-input check, exclusions and purge.
- macOS: files are published with `renamex_np(RENAME_EXCL)`, one atomic call that refuses to replace, instead of the link-and-remove fallback.
- macOS droppings on drives (`._name` AppleDouble files, `Icon\r`, `.Spotlight-V100`, `.Trashes`, `.fseventsd` and similar) are never treated as content, also with the dotfile filters off.
- macOS: release binaries for Apple Silicon and Intel (`onefold-darwin-arm64`, `onefold-darwin-amd64`); `onefold gui` opens the browser (`open`); the state lives in `~/Library/Application Support/onefold`; tools installed with Homebrew are found even when onefold is not started from a shell.
- Windows 11 24H2 and later: a double-click on onefold.exe no longer flashes a console window or a Windows Terminal tab before the GUI opens.
- Measured: right after a reboot, the first catalog of 5,384 files took 52 s with Microsoft Defender scanning and 3 s with onefold excluded (README, Windows notes).
- Fixed: among byte-identical photos or videos with no sub-second time, the copy kept could change from one run to the next (it followed the order the catalog workers finished in); it is now the one in the folder given first, then the one with a clean name, then the oldest, then the shortest path.
- Fixed: the exiftool readers of a catalog run were never stopped and kept running, polling, after onefold exited (Linux and macOS; on Windows they ended with it).
- Fixed: the `seed` rsync script failed with the rsync macOS ships (no `--info=progress2`); it now falls back to `--progress`.
- Fixed: two data races in the GUI: a running job updating its summary while live updates were sent to the browser, and the end of a preview logging its status while Execute had already started the next stage.
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
