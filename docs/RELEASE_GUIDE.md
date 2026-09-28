# Release Guide

This guide explains how a release of the Osano Pulumi provider works and gives the commands for each
step. [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md) is the short list to follow while releasing.

## How a release works

Pushing a `vX.Y.Z` tag runs [`.github/workflows/release.yml`](../.github/workflows/release.yml),
which:

1. builds and tests the provider and regenerates the schema and every SDK (`prerequisites`,
   `build_sdks`), and runs lint, the e2e compilation, the script tests, and the engine-level
   pipeline suite (`verify`); nothing is published unless every one of these passes;
2. builds the provider binaries with GoReleaser, creates the GitHub release with the archives,
   checksums, and SBOMs, and attests their build provenance (`publish`);
3. publishes the Node.js SDK to npm, the Python SDK to PyPI, and the .NET SDK to NuGet
   (`publish_sdks`, all with trusted publishing; the pushed `.nupkg` gets a build provenance
   attestation and is attached to the GitHub release), and the Java SDK to Maven Central (`publish_java_sdk`, signed with the release
   signing key below);
4. pushes the `sdk/go/osano/vX.Y.Z` tag for the Go SDK (`publish_go_sdk`).

[PUBLISHING.md](PUBLISHING.md) lists every package a release publishes, with install commands and
how to verify each one.

The version always comes from the tag. The committed schema and SDKs keep the development version
from the Makefile (`PROVIDER_VERSION ?= 0.1.0-alpha.0+dev`), and the workflow stamps the real version
at build time, so a release needs no version bump in any file. Do not commit SDKs regenerated with
a release version: CI regenerates them with its own version and only tolerates differences in the
version-stamped files.

## One-time setup

This setup was completed for `v0.1.0` and is in place. The sections below record the current values
and how each piece was set up; repeat a step only to rotate a credential or to recreate a publisher.

### Repository secrets

Add these under **Settings > Secrets and variables > Actions > Repository secrets**. `GITHUB_TOKEN`
is provided automatically.

| Secret | Used by | Value |
| --- | --- | --- |
| `NPM_TOKEN` | Not used | Not set and not needed: `@jflavan/pulumi-osano` has an npm trusted publisher. `release.yml` still passes it as `NODE_AUTH_TOKEN`, which npm reads only if the OIDC exchange fails. Leave it unset. |
| `NUGET_USERNAME` | `publish_sdks` (NuGet login) | The nuget.org account name (profile name, not the email address) that owns the trusted publishing policy. |
| `MAVEN_CENTRAL_USERNAME` | `publish_java_sdk` | The username half of a Maven Central Portal user token. |
| `MAVEN_CENTRAL_PASSWORD` | `publish_java_sdk` | The password half of the same user token. |
| `JAVA_SIGNING_KEY` | `publish_java_sdk` | The ASCII-armored GPG private key that signs the Maven artifacts. |
| `JAVA_SIGNING_KEY_ID` | `publish_java_sdk` | The ID of that key: its last 8 hexadecimal characters, the form Gradle's signing plugin expects. |
| `JAVA_SIGNING_PASSWORD` | `publish_java_sdk` | The passphrase of that key. |

The public half of the signing key must be on a public key server so Maven Central can verify the
signatures. The current key is `John Flavan (pulumi-osano release signing)`, RSA 4096, fingerprint
`5277 E261 0B7E 7021 6871  969A 4809 7CF9 4C3F 74F3` (`JAVA_SIGNING_KEY_ID` is `4C3F74F3`), with no
expiry date. It is published on `keyserver.ubuntu.com` and `keys.openpgp.org`; keys.openpgp.org
drops the user ID because the key has no email address.

If the Maven Central secrets are missing, `publish_java_sdk` fails with
`Task 'publishToSonatype' not found`: the generated `sdk/java/build.gradle` registers the Sonatype
repository only when `PUBLISH_REPO_USERNAME` (from `MAVEN_CENTRAL_USERNAME`) is set.

### Trusted publishers

All three trusted publishers are active since `v0.1.0`. The table records their values; recreate a
publisher with exactly these values if it is ever removed.

Every trusted publisher points at this repository and workflow. The values are case-sensitive and
must match exactly; leave the environment empty, because `release.yml` does not use a GitHub
environment.

| Registry | Where | Settings |
| --- | --- | --- |
| npm | npmjs.com > `@jflavan/pulumi-osano` > Settings > Trusted publishing | GitHub Actions; Organization or user `jflavan`; Repository `pulumi-osano`; Workflow filename `release.yml`; Environment empty; **npm publish** allowed (not only `npm stage publish`) |
| PyPI | pypi.org > Your projects > `pulumi-osano` > Manage > Publishing | PyPI project name `pulumi-osano`; Owner `jflavan`; Repository name `pulumi-osano`; Workflow name `release.yml`; Environment name empty |
| NuGet | nuget.org > Trusted Publishing > Create policy | Repository owner `jflavan`; Repository `pulumi-osano`; Workflow file `release.yml`; Environment empty. The `NUGET_USERNAME` secret names the policy owner. |

Maven Central does not support trusted publishing. It needs a verified `io.github.jflavan`
namespace on central.sonatype.com (the Java SDK's group `io.github.jflavan.pulumi` is inside it), the
user token, and the signing key above.

### npm: bootstrap, then trusted publishing

The bootstrap is done. `0.1.0` was published by hand on 2026-09-25 from the CI-built package. The
npm trusted publisher (repository `jflavan/pulumi-osano`, workflow `release.yml`, **npm publish**
allowed) now publishes every later version from `release.yml` with a provenance statement; `0.1.0`
itself has none. The steps below are kept for reference. They are needed again only for a brand-new
npm package.

npm cannot configure a trusted publisher for a package that does not exist yet, so the very first
version of `@jflavan/pulumi-osano` has to be published some other way; every later release uses
trusted publishing. npm 11.5+ first tries to exchange the job's GitHub OIDC token, and only falls back
to `NODE_AUTH_TOKEN` (the `NPM_TOKEN` secret, which is not set, read through the `.npmrc` that
`actions/setup-node` writes) when that exchange fails. Once a trusted publisher exists, its token
replaces the `.npmrc` token, so an empty, missing, or leftover `NPM_TOKEN` cannot break trusted
publishing.

`v0.1.0` was bootstrapped this way, without any token. npm is restricting tokens that bypass 2FA
for direct publishing, so prefer this over an `NPM_TOKEN`:

1. Push the `vX.Y.Z` tag. With no trusted publisher and no `NPM_TOKEN`, the npm step of
   `publish_sdks` fails with an authentication error before PyPI and NuGet run, and
   `publish_go_sdk` is skipped. The GitHub release and SDK artifacts are already built.
2. Download the Node.js SDK the run built and publish that exact package from a maintainer machine:

   ```bash
   gh run download <run-id> -R jflavan/pulumi-osano -n nodejs-sdk.tar.gz -D /tmp/npm-bootstrap
   mkdir /tmp/npm-bootstrap/sdk && tar -zxf /tmp/npm-bootstrap/nodejs.tar.gz -C /tmp/npm-bootstrap/sdk
   npm login --auth-type=web          # signs in through the browser, including 2FA
   cd /tmp/npm-bootstrap/sdk/bin && npm publish --access public
   ```

   This first version has no provenance statement; later versions published by the workflow do.
3. On npmjs.com, open the package's **Settings > Trusted publishing** and add the trusted publisher
   from the table above. Trusted publishers created after 3 September 2026 allow only
   `npm stage publish` by default: also allow **npm publish**, because the workflow publishes
   directly.
4. Wait a few minutes, until `npm view @jflavan/pulumi-osano@X.Y.Z version --prefer-online` prints
   the version, then re-run the failed jobs of the release run. The npm step skips the version that
   is now on npm, PyPI and NuGet publish, and `publish_go_sdk` pushes the Go SDK tag. A re-run that
   starts too soon can miss the new version and fail the npm step again (see
   [Re-run a partially failed release](#re-run-a-partially-failed-release)).
5. Optionally, under **Publishing access**, select **Require two-factor authentication and disallow
   tokens**. Trusted publishing keeps working because it does not use a token.

If you bootstrap with an `NPM_TOKEN` instead, delete the secret and revoke the token once the
trusted publisher exists. If a later release fails with an authentication error, check the trusted
publisher values first: npm reports a failed OIDC exchange only in verbose logs and then falls back
to the (absent) token.

## Dry run

A manual run of the release workflow is always a dry run. There is no input that makes it publish:
every publishing job runs only for a tag push, and the dry-run jobs have read-only permissions and no
registry credentials.

Start one from **Actions > release > Run workflow**, or with the GitHub CLI:

```bash
gh workflow run release.yml --ref main
gh run watch
```

Run it from a branch (`main` or a release branch), not a tag: the version step does not support a
manual run on a tag. The run uses the version `pulumi/provider-version-action` computes for a
branch: the next minor version after the latest GitHub release as an alpha, for example
`0.2.0-alpha.1790300000` from `main` while `v0.1.0` is the latest release (from other branches the
short commit hash is appended, for example `0.2.0-alpha.1790300000+abc1234`).

A dry run:

- builds and tests the provider, regenerates and builds every SDK, and runs the `verify` job
  exactly like a release, including the worktree-clean checks;
- runs GoReleaser with `--snapshot`, which builds every release archive, checksum, and SBOM without
  creating a GitHub release, checks the archive names that `pulumi plugin install` downloads, and
  uploads them as the `dry-run-provider-archives` artifact (kept for 7 days);
- runs `npm publish --dry-run` for the Node.js SDK with the same script, npm version, and dist-tag
  logic as a release;
- runs `twine check` on the Python distributions, lists the `.nupkg`, and builds the Maven
  publication into the runner's local Maven repository (`publishToMavenLocal`, unsigned);
- reports, for npm, PyPI, NuGet, Maven Central, and the Go SDK tag, whether that version is already
  published, which is what a re-run of a release would skip.

It never publishes to npm, PyPI, NuGet, or Maven Central, never pushes a tag, never creates a GitHub
release, and never attests provenance.

## Cut the release

1. Merge everything for the release into `main` and wait for `build` and `CodeQL Advanced` to pass.
2. Run a dry run from `main` and wait for it to pass.
3. In a release PR, move the `## [Unreleased]` entries of [CHANGELOG.md](../CHANGELOG.md) into a
   `## [X.Y.Z] - YYYY-MM-DD` section dated with the day you will tag, add the compare link at the
   bottom, and merge.
4. Tag the merge commit on `main` and push the tag:

   ```bash
   git checkout main
   git pull
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

5. Watch the run: `gh run watch` (or **Actions > release**).

Never move or re-push a tag that a release has published from. If a release needs a code change, fix
it on `main` and release the next patch version.

## Re-run a partially failed release

Use **Re-run failed jobs** on the failed run (or `gh run rerun <run-id> --failed`). It re-runs only
the failed jobs and the jobs that depend on them, and they reuse the artifacts the successful jobs
already uploaded. Every publishing step skips work that the registry already reports as done:

| Job | On re-run |
| --- | --- |
| `publish_sdks`: npm | Skips when `@jflavan/pulumi-osano@X.Y.Z` is already on npm (`npm view`). |
| `publish_sdks`: PyPI | `skip-existing: true` skips files that are already uploaded. |
| `publish_sdks`: NuGet | `--skip-duplicate` skips a version that is already on nuget.org. |
| `publish_java_sdk` | Skips when `io/github/jflavan/pulumi/pulumi-osano/X.Y.Z/pulumi-osano-X.Y.Z.pom` exists on `repo1.maven.org`. |
| `publish_go_sdk` | `pulumi/publish-go-sdk-action` skips the commit and push when the `sdk/go/osano/vX.Y.Z` tag exists, and only refreshes the Go module cache. |

Three cases need care:

- **npm lag.** The npm step looks the version up with `npm view`, which can miss a version published
  a few minutes earlier. The step then tries to publish that version again and fails; npm never
  accepts the same version twice, so the published package does not change. Before re-running soon
  after an npm publish, including a manual bootstrap publish, wait until
  `npm view @jflavan/pulumi-osano@X.Y.Z version --prefer-online` prints the version. If the npm step
  still fails this way, re-run the failed jobs again.
- **Maven Central lag.** A released version usually appears on `repo1.maven.org` 10 to 30 minutes
  after its Central Portal deployment shows PUBLISHING, sometimes longer. Before re-running
  `publish_java_sdk` soon after a Java publish, check the deployment on the
  [Central Portal deployments page](https://central.sonatype.com/publishing/deployments). If it is
  published or still publishing, wait until the version appears on `repo1.maven.org` before
  re-running. Otherwise the job uploads a duplicate deployment, which Maven Central rejects, and the
  job fails without changing the published version.
- **The `publish` (GoReleaser) job failed.** GoReleaser does not replace assets that already exist
  on a release (`replace_existing_artifacts` is off). If it failed after creating the GitHub
  release, delete that release but keep the tag, for example `gh release delete vX.Y.Z --yes`, then
  re-run the failed jobs. Do not use **Re-run all jobs** after `publish` succeeded: GoReleaser would
  fail on the existing release.

`v0.1.0` took four run attempts. The npm step failed with `ENEEDAUTH` in attempts 1 and 2: in
attempt 1 the package did not exist yet, and attempt 2 started less than two minutes after the
manual bootstrap publish, so its `npm view` lookup did not see `0.1.0` yet and the step tried to
publish again. `publish_java_sdk` failed in attempts 1 to 3 with `Task 'publishToSonatype' not found`
until the Maven Central secrets were added. Attempt 3 skipped npm and published PyPI, NuGet, and the
Go SDK tag, and attempt 4 published Maven Central. No attempt published a version twice.

## After the release

1. Check the published packages: the GitHub release assets, npm, PyPI, NuGet, Maven Central, and
   `go list -m github.com/jflavan/pulumi-osano/sdk/go/osano@vX.Y.Z`.
   [PUBLISHING.md](PUBLISHING.md) links each package page and gives the verification commands
   (`gh attestation verify`, `npm audit signatures`, PyPI attestations, the Maven signature). Right
   after a publish, npm's local metadata cache can show a stale 404: use
   `npm view @jflavan/pulumi-osano@X.Y.Z version --prefer-online`. A Maven Central version reaches
   `repo1.maven.org` 10 to 30 minutes after the Central Portal deployment shows PUBLISHING.
2. Check that the plugin installs from the release:

   ```bash
   pulumi plugin install resource osano X.Y.Z --server github://api.github.com/jflavan/pulumi-osano
   ```

3. Not done yet: the provider is not in the Pulumi Registry. Open the listing PR once, as described
   below; nothing blocks it since `v0.2.1`. After it merges, later releases are picked up
   automatically.
4. Review the GitHub release notes, which GoReleaser generates from the commit messages grouped by
   conventional-commit type. Edit the release to add a short summary and links to any new examples.

### List the package in the Pulumi Registry

Once the provider is listed, the registry checks for new releases twice a day and publishes their
docs automatically, so this is needed once, after the first release. The process is described in
[Adding a new package](https://github.com/pulumi/registry/blob/master/docs/adding-a-new-package.md).
The registry reads everything from the latest release tag, including `docs/_index.md` and
`docs/installation-configuration.md`. All of the following has been true since `v0.1.0`:

- `provider/cmd/pulumi-resource-osano/schema.json` sets `publisher` (`John Flavan`), `logoUrl`
  (`assets/logo.png` on `main`), `displayName`, `pluginDownloadURL`, and `keywords` with
  `category/infrastructure` and `kind/native`. It has no `version`, so the registry takes the
  version from the tag.
- `docs/_index.md` (required) and `docs/installation-configuration.md` (optional) start with YAML
  front matter and use only absolute links.
- Both Osano API clients send the `pulumi-osano/<version>` user agent.
- `vX.Y.Z` is a published GitHub release, not a draft or prerelease, and the plugin installs with the
  command above.

The registry publishes the pages of the latest release, so the listing shows what the latest
release documents.

Then:

1. Fork [pulumi/registry](https://github.com/pulumi/registry) and add one entry at the end of the
   `include` list in `community-packages/package-list.json`:

   ```json
   {
     "repoSlug": "jflavan/pulumi-osano",
     "schemaFile": "provider/cmd/pulumi-resource-osano/schema.json"
   }
   ```

2. In the same PR, add the new publisher to
   `tools/resourcedocsgen/pkg/publishers/publisher-names.json`. The key must equal the schema's
   `publisher`, and the value is the publisher's slug:

   ```json
   "John Flavan": "john_flavan"
   ```

3. Open the PR. Say that the provider is community maintained and give `@jflavan` as the contact.
   Automated checks post a fact sheet (docs generation, plugin install, npm, PyPI, and Go installs,
   publisher, doc lint). Fix anything flagged in this repository, release again if the fix must be
   in a release, and comment `/check` on the PR to re-run the checks. A Pulumi maintainer reviews
   and merges it.
4. After it merges, confirm the package appears at `https://www.pulumi.com/registry/packages/osano/`
   with the logo, the overview, and the Installation & Configuration page.

To change the registry's search terms, category, or logo later, change the provider's keywords,
publisher, or logo in `provider/provider.go`, regenerate the schema, and release. Do not edit the
registry's generated package files.
