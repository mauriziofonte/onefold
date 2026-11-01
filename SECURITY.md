# Security policy

onefold handles files people cannot replace. A bug that loses, overwrites or leaks a file is a security bug for this project, even when no attacker is involved.

## Reporting a vulnerability

Please report privately, not in a public issue:

1. **GitHub private vulnerability reporting** (preferred): the **Report a vulnerability** button on the [Security tab](https://github.com/mauriziofonte/onefold/security) of the repository.
2. **E-mail**: fonte.maurizio@gmail.com, subject starting with `[onefold security]`.

Include the onefold version (`onefold version`), the OS, the exact command or GUI steps, and the smallest input that shows the problem. Never send personal photos or documents: describe them, or build a synthetic file that triggers the same behaviour.

You get an acknowledgement within 7 days. Once the problem is confirmed, a fix is prepared privately, released, and the advisory is published with credit to the reporter (unless you prefer to stay anonymous).

## Supported versions

onefold is in pre-release (`0.x`). Only the latest release receives fixes; please reproduce on it before reporting.

## In scope

- **Data safety**: any way to make onefold delete, overwrite, truncate or corrupt a file outside the documented deleters (`rearrange -execute` duplicates, `purge-sources -execute`), or delete with them a file whose content is not safely elsewhere. See "Safety" in `README.md` and section 3 of `ARCHITECTURE.md`.
- **The local GUI server**: access from another local user or from a web page in the same browser (token and cookie handling, DNS rebinding, cross-site requests), path traversal through its API, resource exhaustion.
- **Parsers of untrusted files**: a crafted file (on a USB stick, in an archive) that crashes onefold, makes it loop, exhaust memory, or read or write outside the intended paths. Covered formats include JPEG/TIFF/HEIF EXIF, ISO-BMFF (mp4, mov, heic), Matroska/WebM, FLV, RIFF, RAR, 7z, zip, ISO 9660, OLE2 (doc, xls, ppt), PDF, RTF and the manifest, plan and journal files onefold reads back.
- **SFTP push** (`push`): host key verification, credential handling, writing outside the destination.
- **Pinned tool downloads** (`onefold tools install`): checksum bypass, extraction outside the tools folder.
- **Release integrity**: binaries that do not match the published checksums.

## Out of scope

- Attacks that need administrator or root rights on the machine running onefold, or write access to the onefold binary.
- Behaviour of the optional external tools themselves (exiftool, ffmpeg, poppler, 7-Zip): report those to their projects; onefold's handling of their output is in scope.
- Findings from automated scanners without a demonstrated impact.
