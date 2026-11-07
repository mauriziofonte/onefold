# Packaging

What a release builds besides the bare binaries, and the steps that need the owner's accounts. Everything in this folder is used by `make release` and `.github/workflows/release.yml`; nothing here is published by itself.

| Package | Built by | Where it goes |
|---|---|---|
| `onefold-darwin-arm64-app.zip`, `onefold-darwin-amd64-app.zip` (`onefold.app`) | `make darwin-app` (`internal/pack`) | release assets |
| `onefold-linux-{amd64,arm64}.{deb,rpm,apk}` | `make packages` with [nfpm](https://nfpm.goreleaser.com) from `nfpm.yaml` (binary, `linux/onefold.desktop`, the icons of `assets/brand`) | release assets |
| `<binary>.cdx.json` | `cyclonedx-gomod bin` in the release workflow | release assets |
| provenance | `actions/attest-build-provenance` over `SHA256SUMS` | GitHub attestations: `gh attestation verify FILE --repo mauriziofonte/onefold` |
| Homebrew formula, Scoop manifest, winget manifests | `go run internal/pack/gen_manifests.go` from the release's `SHA256SUMS` | the `package-manifests` artifact of the release run, submitted by hand (below) |

Local builds: `make release` needs `nfpm` on the `PATH` (`go install github.com/goreleaser/nfpm/v2/cmd/nfpm@<the version in release.yml>`).

## Steps that need the owner

None of these has been done. Each one is free for an open-source project.

### Homebrew tap

1. Create the public repository `mauriziofonte/homebrew-onefold` with a `Formula/` folder.
2. After a release, download the `package-manifests` artifact of its workflow run and commit `homebrew/onefold.rb` as `Formula/onefold.rb`.
3. Users install with `brew install mauriziofonte/onefold/onefold`. The formula installs the release binary (no build) and `brew test onefold` runs `onefold version`.

```bash
gh repo create mauriziofonte/homebrew-onefold --public --description "Homebrew tap of onefold"
gh run download <run id> --repo mauriziofonte/onefold --name package-manifests --dir manifests
```

### Scoop bucket

1. Create the public repository `mauriziofonte/scoop-onefold` (a bucket is a repository with a `bucket/` folder).
2. Commit `scoop/onefold.json` of the artifact as `bucket/onefold.json`. Its `checkver` and `autoupdate` let Scoop's tooling follow later releases.
3. Users install with `scoop bucket add onefold https://github.com/mauriziofonte/scoop-onefold` and `scoop install onefold`; the manifest adds a Start menu shortcut that opens the GUI.

### winget

1. Fork `microsoft/winget-pkgs`, copy `winget/manifests/m/MaurizioFonte/onefold/<version>/` of the artifact to the same path, and validate it on Windows: `winget validate --manifest <that folder>`, then `winget install --manifest <that folder>`.
2. Open the pull request; the winget bots check the manifests and the download. Later versions can go through `wingetcreate update MaurizioFonte.onefold --version <v> --urls <exe url>`.
3. Users install with `winget install MaurizioFonte.onefold`.

### SignPath (signed Windows exe)

[SignPath Foundation](https://signpath.org) signs open-source projects for free after an application.

1. Apply at signpath.org with the repository; SignPath reviews the project and its build.
2. In SignPath, create the project with an artifact configuration for a single PE file (the workflow uploads `onefold-windows-amd64.exe` alone as the artifact `windows-exe-unsigned`) and a release signing policy that trusts this repository's `release.yml`.
3. In the repository settings (Secrets and variables > Actions) add the variables `SIGNPATH_ORGANIZATION_ID`, `SIGNPATH_PROJECT_SLUG`, `SIGNPATH_POLICY_SLUG` and the secret `SIGNPATH_API_TOKEN`:

```bash
gh variable set SIGNPATH_ORGANIZATION_ID --repo mauriziofonte/onefold --body <organization id>
gh variable set SIGNPATH_PROJECT_SLUG --repo mauriziofonte/onefold --body onefold
gh variable set SIGNPATH_POLICY_SLUG --repo mauriziofonte/onefold --body release-signing
gh secret set SIGNPATH_API_TOKEN --repo mauriziofonte/onefold
```

From the next tag on, the `sign` job submits the exe, waits for the signature, checks it with `osslsigncode` and the release publishes the signed exe (the checksums, the SBOM, the manifests and the provenance are made after it). Without the variables the job is skipped and the exe is published unsigned, as before. A failed signing stops the release.

### macOS notarization

Notarizing `onefold.app` and the binaries needs a paid Apple Developer account (99 USD a year): there is no free path. Until then the README's Install section explains the first start past Gatekeeper; the Homebrew formula avoids it, since Homebrew does not mark what it downloads.
