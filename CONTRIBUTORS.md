# Contributors

onefold exists because people keep their photos and documents scattered over many machines and deserve one clean, verified copy of each. If you want to help, you are welcome here.

## Maintainer

- Maurizio Fonte ([@mauriziofonte](https://github.com/mauriziofonte)): author and maintainer.

## Code contributors

Your name goes here with your first merged pull request (see [CONTRIBUTING.md](CONTRIBUTING.md); contributions are made under the [CLA](CLA.md)).

## Alpha testers

Your name goes here with your first useful test report (open an issue with the **Alpha test report** template; say in it how you want to be credited, or that you prefer not to be).

---

## Wanted: developers

onefold is a Go project with a Svelte GUI, and it is looking for people who want to work on it regularly, not only one-off fixes. Areas where help changes the most:

| Area | What it involves | Skills |
|---|---|---|
| macOS platform | no-replace renames on APFS/HFS+, the Trash, Finder launch, testing on real Macs | Go, macOS internals |
| Windows platform | long paths, Recycle Bin, Defender behaviour, the WebView2 window | Go, Win32 |
| File-format readers | fuzzing and hardening the pure-Go parsers (EXIF, ISO-BMFF, Matroska, RAR, OLE, PDF) | Go, binary formats |
| Robustness | crash-safety and scale tests: a million files, power cuts mid-copy, hostile names | Go, testing |
| GUI | accessibility, keyboard navigation, translations | Svelte, a11y, a language besides English and Italian |
| Documentation | guides for real-world cases (phone backups, NAS, old drives) | clear writing |

Start with an issue labelled `good first issue`, or open one saying what you would like to work on.

## Wanted: alpha testers

You do not need to write code. You need a computer, some folders worth tidying, and a backup of them. Most useful right now:

- **macOS** users (Apple Silicon and Intel), especially with external drives formatted as exFAT or HFS+.
- **Windows** users with large photo folders, OneDrive folders, network drives or a NAS.
- **Linux** users with archives on USB disks, NAS shares or mixed file systems.
- Big archives: hundreds of thousands of files, hundreds of GB, many duplicates from phone backups and old computers.

What to try, roughly in order: `onefold doctor`; the GUI guided setup; `merge` of two folders (preview, then execute); `rearrange` of a copy of a messy folder; the photo archive (`catalog`, `plan`, `apply`). Tell us how long it took, what was confusing, and anything that looked wrong, however small.
