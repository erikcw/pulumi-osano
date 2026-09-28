# Release Checklist

Use this checklist whenever publishing a new `pulumi-osano` provider release.
[RELEASE_GUIDE.md](RELEASE_GUIDE.md) explains each step in detail.

1. **Check the one-time setup** (in place since `v0.1.0`; recheck after rotating credentials or
   changing a publisher)
   - Repository secrets exist: `NUGET_USERNAME`, `MAVEN_CENTRAL_USERNAME`,
     `MAVEN_CENTRAL_PASSWORD`, `JAVA_SIGNING_KEY`, `JAVA_SIGNING_KEY_ID`, and
     `JAVA_SIGNING_PASSWORD`. `NPM_TOKEN` is not set and not needed.
   - Trusted publishers point at owner `jflavan`, repository `pulumi-osano`, workflow
     `release.yml`, and no environment: the npm trusted publisher for `@jflavan/pulumi-osano`
     (with **npm publish** allowed), the PyPI trusted publisher for `pulumi-osano`, and the NuGet
     trusted publishing policy.
   - The Maven Central namespace `io.github.jflavan` is verified, and the signing public key
     (fingerprint `5277 E261 0B7E 7021 6871  969A 4809 7CF9 4C3F 74F3`) is on
     `keyserver.ubuntu.com` and `keys.openpgp.org`.
2. **Do not bump versions in files**
   - The release version comes from the tag. The committed schema and SDKs stay at the Makefile's
     development version (`0.1.0-alpha.0+dev`); the release workflow stamps the real version.
3. **Run the quality gates on `main`**

   ```bash
   make codegen
   make lint
   make test_all        # tests, e2e compilation, script tests, and the pipeline suite
   make build_sdks
   make build_examples
   git diff --exit-code
   ```

   - Also run `make provider` so the version-stamped Go provider compiles, and `make vulncheck`
     to check the dependencies against the Go vulnerability database.
   - The final diff check proves schema, SDKs, and copied package READMEs are current.
   - Confirm the latest `CodeQL Advanced` run on `main` succeeded and that
     **Security and quality > Code scanning** shows no open critical or high alerts.
   - Check that `github/codeql-action` in `.github/workflows/codeql.yml` is on the
     latest v4 release. If it is not, bump `init` and `analyze` to the same commit
     SHA in one change, and update the `# vX.Y.Z` comments.
4. **Run a release dry run**
   - `gh workflow run release.yml --ref main` (or **Actions > release > Run workflow** on `main`).
     Manual runs never publish.
   - Every job must pass: the provider and SDK builds, `verify` (lint, e2e compilation, script
     tests, and the pipeline suite), `publish (dry run)` (GoReleaser snapshot and archive names),
     `publish_sdks (dry run)` (`npm publish --dry-run`, `twine check`, the `.nupkg`, the Go SDK
     tag), and `publish_java_sdk (dry run)` (`publishToMavenLocal`).
5. **Validate documentation**
   - README quickstart instructions must reflect the published install paths.
   - Repo-local examples must clearly document any required local SDK build steps.
   - Cookie Consent docs must agree that preview/refresh/import do not publish,
     rules delete upstream, and configuration/publication deletes are state-only.
   - The Pulumi Registry pages `docs/_index.md` and `docs/installation-configuration.md` match
     the provider's resources, functions, and configuration, start with YAML front matter, and
     use only absolute links.
   - `docs/PUBLISHING.md` and the README badges and install commands name the published
     packages, and the version-pinned commands name the new version: the README's Java and plugin
     commands, the plugin command in each `docs/package-readmes/` file (and its `sdk/` copy), `docs/PUBLISHING.md`, `docs/UPGRADE.md`, `docs/troubleshooting.md`,
     `docs/installation-configuration.md`, `docs/logging.md` (the user agent example), the
     released-package sections of the example READMEs, the version placeholder in
     `.github/ISSUE_TEMPLATE/bug.yaml`, and, for a new minor version, the supported-versions
     table in `SECURITY.md`.
6. **Update the changelog**
   - In the release PR, move the `## [Unreleased]` entries of `CHANGELOG.md` into
     `## [X.Y.Z] - YYYY-MM-DD` and add its compare link. Date it with the day you tag (UTC),
     never a guessed date.
   - Summarize user-facing changes, new resources, and breaking updates.
   - Highlight Osano API version changes and any new required scopes.
   - For Cookie Consent changes, call out `scriptSrc`, `scriptTag`, the
     caller-managed `changeToken`, default preservation of unclassified
     discoveries, composite rule imports, and configurations/publications
     retained upstream after destroy.
7. **Tag and publish**
   - After the release PR merges: `git checkout main && git pull && git tag vX.Y.Z && git push origin vX.Y.Z`.
   - Verify the GitHub Actions `release` workflow completes successfully for provider binaries and every SDK.
   - If a job fails, fix the cause and use **Re-run failed jobs**; publishing steps skip versions
     that are already published. Right after an npm or Maven Central publish the lookup can miss
     the new version, so wait for it to appear first (see "Re-run a partially failed release" in
     the release guide). If the `publish` (GoReleaser) job failed after creating the GitHub
     release, delete the release (keep the tag) first. Never move a published tag.
   - Confirm npm, PyPI, NuGet, Maven Central, and Go publication all completed before announcing the release.
8. **Post-release follow-up**
   - Check that the plugin installs:
     `pulumi plugin install resource osano X.Y.Z --server github://api.github.com/jflavan/pulumi-osano`.
   - Verify provenance and signatures as described in `docs/PUBLISHING.md` (for example
     `gh attestation verify pulumi-resource-osano-vX.Y.Z-linux-amd64.tar.gz --owner jflavan`, and
     the same for the `Community.Pulumi.Osano.X.Y.Z.nupkg` asset on the GitHub release; the copy
     nuget.org serves is re-signed and verifies with `dotnet nuget verify` instead).
     Allow 10 to 30 minutes for Maven Central to reach `repo1.maven.org`, and use
     `npm view --prefer-online` if npm shows a stale 404.
   - The npm bootstrap was done for `v0.1.0`. It is needed again only for a brand-new npm
     package; see "npm: bootstrap, then trusted publishing" in the release guide.
   - Not done yet: the provider is not in the Pulumi Registry. The registry reads
     `docs/_index.md` and `docs/installation-configuration.md` from the latest release tag, and
     nothing blocks the listing since `v0.2.1`. List it once: open a PR to
     [pulumi/registry](https://github.com/pulumi/registry) that adds
     `{"repoSlug": "jflavan/pulumi-osano", "schemaFile": "provider/cmd/pulumi-resource-osano/schema.json"}`
     to `community-packages/package-list.json` and `"John Flavan": "john_flavan"` to
     `tools/resourcedocsgen/pkg/publishers/publisher-names.json`, then fix anything the PR's
     automated fact sheet flags and comment `/check`. Later releases are picked up by the
     registry automatically within a day; confirm the new version at
     `https://www.pulumi.com/registry/packages/osano/`.
   - Monitor issues for regressions.

Keep the checklist updated as automation improves.
