# Packages and publishing

Each release of the Osano (Unofficial) provider publishes the provider plugin to GitHub Releases and
an SDK for each Pulumi language to that language's package registry. This page lists what is
published, how the versions fit together, and how to verify what you install. The last section
summarizes, for maintainers, how the release workflow publishes each package;
[RELEASE_GUIDE.md](RELEASE_GUIDE.md) has the release procedure.

The first release is `v0.1.0` (2026-09-25).

## Published packages

| Language | Registry | Package | Install |
| --- | --- | --- | --- |
| Node.js | npm | [`@jflavan/pulumi-osano`](https://www.npmjs.com/package/@jflavan/pulumi-osano) | `npm install @jflavan/pulumi-osano` |
| Python | PyPI | [`pulumi-osano`](https://pypi.org/project/pulumi-osano/) | `pip install pulumi-osano` |
| Go | Go module proxy | [`github.com/jflavan/pulumi-osano/sdk/go/osano`](https://pkg.go.dev/github.com/jflavan/pulumi-osano/sdk/go/osano) | `go get github.com/jflavan/pulumi-osano/sdk/go/osano@v0.3.0` |
| .NET | NuGet | [`Community.Pulumi.Osano`](https://www.nuget.org/packages/Community.Pulumi.Osano) | `dotnet add package Community.Pulumi.Osano` |
| Java | Maven Central | [`io.github.jflavan.pulumi:pulumi-osano`](https://central.sonatype.com/artifact/io.github.jflavan.pulumi/pulumi-osano) | See [Java](#java) |
| Provider plugin | GitHub Releases | [`pulumi-resource-osano`](https://github.com/jflavan/pulumi-osano/releases) | Downloaded automatically; see [Provider plugin](#provider-plugin) |

Import names and requirements, as of `0.3.0`:

| Language | Import | Requires |
| --- | --- | --- |
| Node.js | `import * as osano from "@jflavan/pulumi-osano";` | `@pulumi/pulumi` `^3.142.0`; Node.js 22 or later, which current `@pulumi/pulumi` releases require |
| Python | `import pulumi_osano as osano` | Python 3.10 or later, `pulumi>=3.231.0,<4.0.0`, `packaging>=24.0` (the package is built from `pyproject.toml`) |
| Go | `import "github.com/jflavan/pulumi-osano/sdk/go/osano"` | Go 1.26.6 or later, `github.com/pulumi/pulumi/sdk/v3` v3.264.0 or later (`go get` raises your requirement to it) |
| .NET | `using Community.Pulumi.Osano;` | .NET 8 or later, tested with .NET 8 and .NET 10 (`0.2.x` targets `net6.0`, which NuGet resolves for any later framework; from `0.3.0` the package targets `net8.0`), `Pulumi` `[3.76.1, 4.0.0)` |
| Java | `import io.github.jflavan.pulumi.osano.*;` | Java 11 or later, `com.pulumi:pulumi` |

### Java

Maven:

```xml
<dependency>
    <groupId>io.github.jflavan.pulumi</groupId>
    <artifactId>pulumi-osano</artifactId>
    <version>0.3.0</version>
</dependency>
```

Gradle (Groovy or Kotlin DSL):

```groovy
implementation("io.github.jflavan.pulumi:pulumi-osano:0.3.0")
```

The package declares `com.pulumi:pulumi` only as a runtime dependency, so your program must depend on
`com.pulumi:pulumi` itself to compile. Programs created from Pulumi's Java templates
(`pulumi new java`) already do.

## Provider plugin

Each [GitHub release](https://github.com/jflavan/pulumi-osano/releases) carries the provider plugin
as `pulumi-resource-osano-vX.Y.Z-<os>-<arch>.tar.gz` for `darwin`, `linux`, and `windows` on `amd64`
and `arm64`, an SBOM (`<archive>.sbom.json`) for each archive, and `checksums.txt` with the SHA-256
checksum of every archive and SBOM.

You do not normally install the plugin yourself. Every SDK records the plugin's download location,
`github://api.github.com/jflavan/pulumi-osano`, and Pulumi downloads the plugin version that matches
the SDK the first time you run `pulumi preview` or `pulumi up`.

Install it by hand for a Pulumi YAML program, or before working without network access, at the same
version as your SDK:

```bash
pulumi plugin install resource osano 0.3.0 --server github://api.github.com/jflavan/pulumi-osano
```

The download uses the GitHub API. If Pulumi reports that the GitHub rate limit is exceeded, set
`GITHUB_TOKEN` to a GitHub token and run the command again. See
[troubleshooting](troubleshooting.md#provider-plugin-not-found-or-not-downloaded) for other plugin
download failures.

## Versions

- A release is tagged `vX.Y.Z`, and every package in it has the same version: the provider plugin,
  npm, PyPI, NuGet, and Maven Central packages are all `X.Y.Z` (PyPI uses the PEP 440 spelling of a
  prerelease version), and the Go module is tagged `sdk/go/osano/vX.Y.Z`.
- The SDK version chooses the plugin version, so pin the SDK to decide which provider version a
  program uses. See [Pinning pre-1.0 releases](UPGRADE.md#pinning-pre-10-releases); note that
  `npm install` records a `^` range unless you pass `--save-exact`.
- Every SDK embeds its version, the Go SDK included, so a Go program requests the matching plugin
  even when it uses the SDK through a `replace` directive.
- The provider is pre-1.0. Under semantic versioning a `0.x` minor release may contain breaking
  changes; read the [CHANGELOG](../CHANGELOG.md) before upgrading.
- The Go tag points at a release-only commit that contains the Go SDK generated for that release.
  The `sdk/` directories on `main` hold the development SDKs, so depend on a released version
  (`@vX.Y.Z` or `@latest`), not on a branch.
- A prerelease version (for example `X.Y.Z-alpha.N`) is published to npm under the matching `alpha`,
  `beta`, or `rc` dist-tag, never `latest`, and GoReleaser marks its GitHub release as a prerelease.

### Working from a clone

The schema and SDKs committed to this repository carry the development version `0.1.0-alpha.0+dev`,
which is never published, and the repository examples reference those SDKs rather than the published
packages. Every SDK in the clone, including the Go SDK used through a `replace` directive, requests
the plugin at that version. To run an example from a clone, build and install the local plugin as
described in [End-to-End Workflow, section 1](end-to-end-workflow.md#1-choose-keys-and-install). To
run one against the published packages, see
[Run against the published packages](../examples/quickstart/README.md#run-against-the-published-packages).

## Verifying what you install

### Provider plugin archives

Every archive and SBOM on a release has a SHA-256 checksum in `checksums.txt` and a GitHub build
provenance attestation signed by this repository's release workflow. To check the Linux amd64
archive of `v0.3.0`:

```bash
gh release download v0.3.0 -R jflavan/pulumi-osano \
  -p 'pulumi-resource-osano-v0.3.0-linux-amd64.tar.gz*' -p checksums.txt
sha256sum -c --ignore-missing checksums.txt
gh attestation verify pulumi-resource-osano-v0.3.0-linux-amd64.tar.gz --owner jflavan
```

With `--owner jflavan`, `gh attestation verify` accepts an attestation from any workflow in a
`jflavan` repository, and its output names the signing workflow
(`.github/workflows/release.yml@refs/tags/v0.3.0` in `jflavan/pulumi-osano`). To require the release
workflow itself:

```bash
gh attestation verify pulumi-resource-osano-v0.3.0-linux-amd64.tar.gz \
  --repo jflavan/pulumi-osano \
  --signer-workflow jflavan/pulumi-osano/.github/workflows/release.yml
```

The same commands verify an SBOM. To scan an SBOM for known vulnerabilities, use a scanner such as
[grype](https://github.com/anchore/grype):

```bash
grype sbom:./pulumi-resource-osano-v0.3.0-linux-amd64.tar.gz.sbom.json
```

### npm

In a project that depends on the package, `npm audit signatures` verifies the npm registry signature
of every installed package, and the provenance attestation of each package that has one.

`0.1.0` has a registry signature but no provenance statement: it was published by hand from the
package the release workflow built, because npm cannot set up trusted publishing for a package that
does not exist yet. Later versions are published by the release workflow with trusted publishing and
npm provenance, which links each version to the workflow run that built it.

### PyPI

The wheel and the source distribution are published with trusted publishing and carry PyPI publish
attestations naming the repository `jflavan/pulumi-osano` and the workflow `release.yml`. PyPI shows
them on each file's page under **Download files**, and the integrity API returns them:

```bash
curl -s -H 'Accept: application/vnd.pypi.integrity.v1+json' \
  https://pypi.org/integrity/pulumi-osano/0.3.0/pulumi_osano-0.3.0-py3-none-any.whl/provenance
```

### NuGet

nuget.org signs every package with its repository signature. To check a downloaded package:

```bash
dotnet nuget verify --all community.pulumi.osano.0.3.0.nupkg
```

The output names `NuGet.org Repository by Microsoft`. The package is published from the release
workflow through NuGet trusted publishing, which uses a short-lived API key instead of a stored one.

From `0.3.0`, the release workflow also attests the build provenance of the exact `.nupkg` it
pushed and attaches that file to the GitHub release as `Community.Pulumi.Osano.X.Y.Z.nupkg`.
nuget.org re-signs every package it receives, so the copy it serves has a different digest and
verifies only with `dotnet nuget verify` above; the copy on the GitHub release verifies like a
plugin archive:

```bash
gh release download vX.Y.Z -R jflavan/pulumi-osano -p 'Community.Pulumi.Osano.X.Y.Z.nupkg'
gh attestation verify Community.Pulumi.Osano.X.Y.Z.nupkg --owner jflavan
```

The two files hold the same entries with the same contents; the nuget.org copy adds only the
`.signature.p7s` repository signature.

### Maven Central

Every file of the Java package has a detached `.asc` signature made with the release signing key:

- User ID `John Flavan (pulumi-osano release signing)`, RSA 4096
- Fingerprint `5277 E261 0B7E 7021 6871  969A 4809 7CF9 4C3F 74F3`
- Published on `keyserver.ubuntu.com` and `keys.openpgp.org` (keys.openpgp.org serves the key
  without its user ID, because the key has no email address)

To check the `0.3.0` jar:

```bash
base=https://repo1.maven.org/maven2/io/github/jflavan/pulumi/pulumi-osano/0.3.0
curl -sO "$base/pulumi-osano-0.3.0.jar"
curl -sO "$base/pulumi-osano-0.3.0.jar.asc"
curl -s 'https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x5277E2610B7E70216871969A48097CF94C3F74F3' | gpg --import
gpg --verify pulumi-osano-0.3.0.jar.asc pulumi-osano-0.3.0.jar
```

`gpg` reports `Good signature from "John Flavan (pulumi-osano release signing)"` and prints the
primary key fingerprint. It also warns that the key is not certified with a trusted signature unless
you have certified it yourself; compare the fingerprint with the one above.

### Go

`go get` checks the module against the Go checksum database (`sum.golang.org`) and records its
hashes in `go.sum`. `go mod verify` checks that the copies in the module cache still match them.

## Notes on 0.1.0

These do not affect how the packages work:

- The npm package `0.1.0` has no provenance statement (see [npm](#npm)).
- The NuGet packages `0.1.0` to `0.2.1` were built in the Debug configuration, have no package
  tags, and target `net6.0`; `0.3.0` and later are Release builds that target `net8.0` and carry
  tags and a build provenance attestation.
- The npm `package.json` declares no `main` field; Node.js loads `index.js`, and TypeScript reads
  the declared `index.d.ts` types.

## Pulumi Registry

The provider is not listed in the [Pulumi Registry](https://www.pulumi.com/registry/) yet.
`docs/_index.md` and `docs/installation-configuration.md` are prepared for a listing, which the
[release guide](RELEASE_GUIDE.md#list-the-package-in-the-pulumi-registry) describes.

## How each package is published

Pushing a `vX.Y.Z` tag runs [`.github/workflows/release.yml`](../.github/workflows/release.yml),
which builds and tests the provider, regenerates and builds every SDK, and then publishes. A manual
run of the workflow is always a dry run and never publishes.

| Job | Publishes | Authentication |
| --- | --- | --- |
| `publish` | GoReleaser builds the archives, SBOMs (Syft), and `checksums.txt` and creates the GitHub release; `actions/attest-build-provenance` then attests every file listed in `checksums.txt`. | `GITHUB_TOKEN`; the attestations are signed with the job's OIDC identity (`id-token: write`). |
| `publish_sdks` (npm) | `.github/scripts/publish-npm.sh` runs `npm publish --provenance` with the `latest`, `alpha`, `beta`, or `rc` dist-tag. | npm trusted publishing (OIDC). |
| `publish_sdks` (PyPI) | `pypa/gh-action-pypi-publish` uploads the wheel and source distribution with publish attestations. | PyPI trusted publishing (OIDC). |
| `publish_sdks` (NuGet) | `dotnet nuget push` uploads the `.nupkg`; `actions/attest-build-provenance` then attests the pushed file, and `gh release upload` attaches it to the GitHub release. | NuGet trusted publishing: `nuget/login` exchanges the OIDC token for a short-lived API key for the `NUGET_USERNAME` account; the attestation is signed with the job's OIDC identity. |
| `publish_java_sdk` | Gradle `publishToSonatype closeAndReleaseSonatypeStagingRepository` uploads the signed publication to Maven Central through the Central Portal. | A Central Portal user token (`MAVEN_CENTRAL_USERNAME`, `MAVEN_CENTRAL_PASSWORD`); the release key signs the artifacts (`JAVA_SIGNING_KEY`, `JAVA_SIGNING_KEY_ID`, `JAVA_SIGNING_PASSWORD`). |
| `publish_go_sdk` | `pulumi/publish-go-sdk-action` commits the generated Go SDK on a release-only commit and pushes the `sdk/go/osano/vX.Y.Z` tag, which the Go module proxy serves. | `GITHUB_TOKEN` with `contents: write`. |

`publish` runs after `build_sdks` and `verify` (lint, e2e compilation, script tests, and the
engine-level pipeline suite), `publish_sdks` and `publish_java_sdk` run after `publish`, and
`publish_go_sdk` runs after `publish_sdks`, so a failed check holds back every package and a failed
npm, PyPI, or NuGet step also holds back the Go tag. What each job does
when a failed release is re-run is described in
[Re-run a partially failed release](RELEASE_GUIDE.md#re-run-a-partially-failed-release).

The one-time setup (repository secrets, the npm, PyPI, and NuGet trusted publishers, the Maven
Central namespace, and the signing key) is recorded in
[RELEASE_GUIDE.md, One-time setup](RELEASE_GUIDE.md#one-time-setup) and has been in place since
`v0.1.0`. The release guide also covers the dry run, cutting a release, and what to check
afterwards; [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md) is the short version.
