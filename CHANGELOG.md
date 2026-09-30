# Changelog

All notable changes to the Osano (Unofficial) Pulumi provider are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until 1.0.0, a minor release
(0.x.0) may contain breaking changes; see [docs/UPGRADE.md](docs/UPGRADE.md).

## [Unreleased]

### Fixed

- `pulumi import` no longer adopts values that the next `pulumi preview` rejects. A
  `CookieConsentRule` import leaves `description` and `expiry` unset on rules other than cookies,
  where Osano reports `description: ""` on some script rules, and a `CookieConsentConfig` import
  leaves out the `additionalLinks: []` Osano reports for a configuration without additional
  links. Previously the preview after importing such a rule failed with `description is only
  supported for cookies`, and the first change to such a configuration failed with
  `configuration.additionalLinks must be a list of one or two [text, url] pairs`. For a stack
  imported with an earlier version, remove these values from the program; the next `pulumi up`
  updates each affected resource once and then shows no changes.
- The attested NuGet package is attached to each GitHub release as
  `Community.Pulumi.Osano.X.Y.Z.nupkg`, because nuget.org re-signs the package it serves and that
  copy no longer matches the attested digest. The `0.3.0` release has the file attached by hand.

## [0.3.0] - 2026-09-28

This release comes out of a production-readiness review of the provider, its CI, and its
documentation. It contains breaking changes; upgrade notes:
[docs/UPGRADE.md](docs/UPGRADE.md#upgrading-from-02x-to-030).

### Changed

- **Stack configuration takes precedence over environment variables.** `osano:osanoApiKey`,
  `osano:unifiedConsentApiKey`, `osano:apiBaseUrl`, `osano:customerBaseUrl`, and
  `osano:requestTimeoutSeconds` now win over `OSANO_API_KEY`, `OSANO_UC_API_KEY`,
  `OSANO_API_BASE_URL`, `OSANO_CUSTOMER_BASE_URL`, and `OSANO_API_TIMEOUT_SECONDS`; an environment
  variable is used only when its configuration key is unset. Previously a set environment variable
  silently overrode the stack, so an exported key could point a stack at another Osano account.
  When both are set and differ, the provider warns that the stack configuration is used.
- **Base URLs must use `https`.** `apiBaseUrl` and `customerBaseUrl`, and their environment
  variables, are validated when the provider is configured, before any request: they must be
  absolute URLs, and `http` is accepted only for loopback hosts (`localhost`, `127.0.0.1`, `::1`),
  as used by local mocks. API keys travel in request headers, so a plain-`http` URL to any other
  host would send them in clear text. The HTTP client also no longer follows redirects, so a key is
  never re-sent to a host the response chose.
- **`requestTimeoutSeconds` must be from 1 to 3600** and applies to each request attempt. A
  configuration value above 3600 fails; an `OSANO_API_TIMEOUT_SECONDS` value that is not a whole
  number in that range is ignored with a warning, and the default of 60 is used.
- **The same retry policy applies to both Osano APIs**, which now share one HTTP client. GET
  requests, including every Unified Consent lookup and the `Consent` refresh, are retried after a
  dropped connection and after `429` or `5xx` responses; PATCH and DELETE requests after `429` or
  `5xx`; POST requests only after `429`, because Osano documents no idempotency keys, so a `5xx`
  may already have been processed. The publish request is the exception and is retried after `5xx`
  too, because Osano answers a duplicate publish with `409`, which the provider joins. Up to three
  retries, honoring `Retry-After` (capped at one minute) and otherwise doubling from one second. In
  0.2.x, Unified Consent requests were never retried and creates were retried after `503`.
- **`Consent` refresh never removes the resource.** When Osano reports no consent for the subject,
  `pulumi refresh` keeps the resource and its `lastSynced` value and warns, so a refresh can never
  cause a consent to be submitted again. In 0.2.x the resource was dropped from state and the next
  `pulumi up` submitted it again. `lastSynced` is the submission timestamp. `pulumi import` of a
  `Consent` fails with an explicit error instead of importing an empty resource: Osano exposes only
  the merged consent of a subject, not individual submissions.
- **`Consent` compares inputs by value.** Every changed input still replaces the resource, which
  submits a new consent, but a change in secretness alone (see the next item) or an absent list or
  map against an empty one is not a change.
- **Subject identifiers and personal data are secrets.** Pulumi now encrypts these in state and
  masks them in output: `Consent.subject`; `subjectRef` (input and output), `unifiedConsent`, and
  `conflicts` of `getUnifiedConsent`; `subjectRef`, `subjectId`, `verifiedId`, and `anonymousId` of
  `getSubject`; `subjectId` of `checkConsent` and `getSubjectProfile`; `getConsentProfile.profile`;
  `getSession.verifiedId`; `verifySubjectCode.verifiedId`; and the `email` and `phone` inputs of
  `sendSubjectCode` and `verifySubjectCode`. Values that were already secret stay secret; the
  schema is the complete list. A stack output built from one of these values is now shown as
  `[secret]`; `pulumi stack output --show-secrets` reveals it.
- **Previews show inputs, not placeholders.** A `pulumi preview` that creates a `Consent` no longer
  shows a made-up `consentId`; the ID and `consentId` stay unknown until `pulumi up`. A preview that
  creates a `CookieConsentPublication` shows the configuration ID instead of `preview`. Previews of
  new Cookie Consent configs and rules show their inputs, and server-assigned values stay unknown.
- **Error messages.** API errors read `osano api error: status=<code> body=<body>` (was
  `status <code>`), with the body cut at 2 KiB. A request that gets no response reports the method
  and host only (`Osano API request failed: GET https://api.osano.com: ...`), never the path or
  query, which can hold a session ID. Error responses to `sendSubjectCode` and `verifySubjectCode`
  withhold the body, which can echo the subject's email address or phone number. A response body
  over 8 MiB is rejected, and a list that pages more than 1000 times fails instead of looping.
- `countryCodeOverride` and `regionCodeOverride` are sent upper-case (`us-ca` is sent as `US-CA`).
- `getSubject` trims `subjectRef`; `verifySubjectCode` fails when Osano answers `200` with
  `verified: false`; and `referenceType: anonymous` logs a deprecation warning (it is still sent as
  `subject`).
- The provider validates its configuration when Pulumi configures it, so an invalid base URL or
  timeout and the deprecated `ucApiKey` and `ucBaseUrl` keys are reported once per run, before any
  resource operation, instead of at the first request that needs them.
- `CookieConsentRule` resource IDs must be `<configId>/<ruleId>`; the numeric-only form of
  pre-release builds is no longer read.
- Schema: the nested types (`ConsentAction`, `ConsentSubject`, `ConsentCompliance`,
  `ConsentPrivacyPolicy`, and the Cookie Consent result types) have descriptions, and the provider
  configuration descriptions state the precedence and validation rules.
- .NET: `Community.Pulumi.Osano` targets `net8.0` (was `net6.0`, which is out of support), has
  package tags, and ships its XML documentation. It requires .NET 8 or later as before, and
  `tests/dotnet` still compiles the example on .NET 8 and .NET 10.
- Go: the SDK module no longer pins a Go `toolchain`, so `go get` does not download Go 1.27.1 into
  a program that builds with any supported Go version (1.26.6 or later).

### Added

- `OSANO_CUSTOMER_BASE_URL` sets `customerBaseUrl`, as `OSANO_API_BASE_URL` does for `apiBaseUrl`.
- `getCookieConsentRules.maxResults` stops after that many rules; unset or `0` returns every match.
- A lost `CookieConsentConfig` create is adopted instead of duplicated: when the create request
  fails with a `5xx` response or no response, the provider lists the configurations with the
  requested name, and if exactly one has that name and those domains and was created at or after
  the request, it becomes the resource, with a warning. Otherwise the create fails as before, and
  the troubleshooting guide explains how to import a duplicate, since Osano cannot delete
  configurations.
- Supply chain and CI: every third-party GitHub Action is pinned to a commit SHA, with Dependabot
  updates for Actions, Go modules, npm, NuGet, pip, and the devcontainer; the NuGet package of each
  release has a GitHub build provenance attestation (verify the copy attached to the GitHub release
  with `gh attestation verify <package>.nupkg --owner jflavan`; nuget.org re-signs the copy it
  serves); a release publishes only after a new `verify` job (lint, e2e
  compilation, script tests, and the engine-level pipeline suite) passes; CodeQL also analyzes the
  C# and Java SDKs and examples; workflows check out without persisted credentials, and test jobs
  run without a `GITHUB_TOKEN`; the pipeline suite also runs on every push to `main`; and the
  Claude workflows run only when the `CLAUDE_ENABLED` repository variable is `true`.
- Make targets: `make vulncheck` runs `govulncheck`, `make lint_fix` applies lint fixes (`make lint`
  no longer rewrites files), `RACE=` runs the tests on a toolchain without cgo, and `make test_all`
  runs everything including the pipeline suite.

### Fixed

- `CookieConsentRule` updates no longer clear fields the program never set. An update sends the
  fields the program sets now or set before; an optional field (`title`, `vendorName`, `ruleType`,
  and for cookies `description` and `expiry`) the program has never set is omitted, so Osano's
  default or a value set in the dashboard stays as it is, matching create and refresh. Removing a
  field the program did set still clears it. In 0.2.x every optional field the program did not set
  was sent as `null` on each update.
- A `CookieConsentConfig.configuration` key the program stops declaring is now cleared in Osano:
  the update sends `null` for it, at any depth except inside the atomic objects such as
  `variantMapping`. Previously the key kept its value in Osano, and refresh showed no drift because
  it compares only declared keys.
- `CookieConsentPublication` waits for the deadline behind a stale error. When a configuration
  keeps reporting the previous publication's `error` after a new publish request was accepted, the
  provider keeps polling until the resource's `customTimeouts` (or the 20-minute default) and warns
  once, after about 25 seconds, that it is waiting for Osano to start the new publication. 0.2.x
  failed at that point with `Osano did not start a new publication`. A fresh `error` still fails
  immediately.
- A `CookieConsentConfig` or `CookieConsentPublication` whose Osano response has no `configId` now
  fails the read instead of being reported as deleted, which would have made the next update create
  an undeletable duplicate.

### Removed

- The opt-in live e2e suites no longer call Osano through their own HTTP client
  (`tests/e2e/internal/api`); they run the provider's functions and the `Consent` resource, so they
  test what users run.
- The `comment-on-stale-issues` workflow and the `ci-mgmt` configuration; the workflows are
  maintained by hand.

## [0.2.1] - 2026-09-25

### Changed

- The npm, PyPI, NuGet, and pkg.go.dev pages now show a README written for their own language, with
  TypeScript, Python, C#, or Go examples respectively. Previously npm, PyPI, and NuGet all showed the
  repository README with its TypeScript examples, and pkg.go.dev showed no README.

## [0.2.0] - 2026-09-25

This release brings the provider up to date with Osano's current Customer REST API and Unified
Consent Core API specs (both published 2026-09-23) and with pulumi-go-provider v1.6.0 and Pulumi
v3.264.0. Upgrade notes: [docs/UPGRADE.md](docs/UPGRADE.md#upgrading-from-010-to-020).

### Added

- Cookie Consent functions, all read-only and authenticated with the Osano API key:
  - `getCookieConsentConfig` reads one configuration, including its publish status, `scriptSrc`, and
    `scriptTag`. A website stack can fetch the consent script for a configuration that another stack
    or the Osano dashboard manages.
  - `getCookieConsentConfigs` lists configurations filtered by name, domains, organization, mode,
    publish status, or discovery recording, following pagination; each result includes its script.
  - `getCookieConsentRules` lists a configuration's rules, optionally by storage type and
    classification, with the rule IDs that `pulumi import` needs.
  - `getCookieConsentDiscoveries` lists the cookies, scripts, iframes, or localStorage keys Osano
    discovered, with the scan origin and AI classification confidence. Use it to check what is still
    unclassified before switching a configuration to `production` mode.
  - `getCookieConsentAuditLog` queries configuration, rule, and publication events (for example
    `cmp.configPublished`) with the acting user and change details.
- `CookieConsentConfig` validates the `configuration` object against Osano's spec during
  `pulumi preview`: value types, `tattleSampling` and `timeoutSeconds` ranges, the blocking modes,
  Do Not Sell categories, `additionalLinks`, the US banner-format `variantMapping`, and palette
  options. It warns about keys the spec does not list, deprecated palette keys, `ccpaRelaxed`
  alongside a `variantMapping`, and Google Consent Mode left on in a new `debug` configuration. A
  configuration that is unchanged since the last `pulumi up` only warns, so existing stacks keep
  previewing.
- Unified Consent:
  - The `Consent` resource submits Global Privacy Control consents: set `origin` to `gpc` and omit
    `actions`, and Osano derives the actions, which the resource exports as `gpcActions`.
  - `Consent` accepts `sessionToken`, and `Consent`, `getUnifiedConsent`, `checkConsent`, and
    `getConsentProfile` accept `countryCodeOverride` and `regionCodeOverride`, so a pipeline can
    state the subject's location instead of Osano geolocating the CI runner.
  - New functions `getSubjectProfile` and `getSession`. Their personal-data outputs are secrets.
  - `getUnifiedConsent` also returns `brandId`, `channelIds`, and `lastConflictDate`.
  - `sendSubjectCode` returns the SMS challenge `session` when Osano provides one, and
    `verifySubjectCode` takes that `session` and returns the `verifiedId`.
- An engine-level end-to-end suite (`tests/e2e/pipeline`, build tags `e2e pipeline`) runs
  `pulumi up`, `refresh`, and `destroy` through the Pulumi engine against a mock of the Osano API,
  covering the publish-and-export-the-script pipeline without credentials.
- `docs/PUBLISHING.md` lists every published package (npm, PyPI, NuGet, Go, Maven Central) and the
  provider plugin, with install commands, how each one is published, and how to verify its
  provenance or signature.
- CI checks that the .NET SDK builds for .NET 8 and .NET 10: `tests/dotnet/SdkCompatibility.csproj`
  compiles the cookie-consent C# example for both. The package still targets `net6.0`, which NuGet
  resolves for either version.

### Changed

- `referenceType` on `getUnifiedConsent` and `getSubject` accepts `subject` (the default) and
  `session`, the only values Osano's API accepts. `anonymous` is deprecated and now looks the
  reference up as `subject`, because anonymous IDs are subject references; any other value fails.
- `Consent` checks that `compliance.gpc` is 0 or 1, that subject IDs contain no `#`, `%`, or
  spaces, and that `countryCodeOverride` and `regionCodeOverride` are ISO 3166 codes. For new or
  changed values it also checks that each action is `ACCEPT`, `REJECT`, or `UNSELECTED` and that
  `origin` is `api` or `gpc`, the values Osano documents. `actions` is optional when `origin` is
  `gpc`, and a GPC consent warns about inputs the GPC endpoint does not accept (`tags`,
  `sessionToken`, `compliance.privacyPolicy`).
- A Unified Consent request that gets no response reports the host but not the request path, which
  can hold a session ID.
- `hashedSubjectId` is optional on `sendSubjectCode` and `verifySubjectCode`; Osano identifies the
  subject by email or phone. `verifySubjectCode` requires `session` with `phone`.
- Subject-verification calls send every configured key, so the Unified Consent API key alone is
  enough; Osano's OpenAPI spec lists that key for these routes.
- `CookieConsentPublication.webhookUrl` is stored as a secret. Osano calls the webhook without
  authentication, so the URL is often the only secret.
- The `destination` outputs of `sendSubjectCode` and `verifySubjectCode` and the `profile` output of
  `verifySubjectCode` are secrets, because they hold personal data.
- Built with pulumi-go-provider v1.6.0 (was v1.1.2), Pulumi SDK v3.264.0 (was v3.212.0), and Go
  1.27.1 (was 1.24.10, which no longer receives security fixes). The SDKs are generated by Pulumi
  CLI v3.264.0:
  - Go: the SDK embeds its version, so Go programs request the matching plugin even through a
    `replace` directive, and it generates resource array and map types. It requires Go 1.26.6 or
    later and `github.com/pulumi/pulumi/sdk/v3` v3.264.0 or later.
  - Python: the package is built from `pyproject.toml`, requires Python 3.10 or later and
    `pulumi>=3.231.0`, and uses `packaging` instead of `parver`.
  - Node.js: the package targets ES2022; current `@pulumi/pulumi` releases require Node.js 22 or
    later.
- CI and local builds use the .NET 10 SDK (10.0.401) instead of 8.0.414. The .NET 10 SDK also builds
  the .NET 8 targets. Lint runs with golangci-lint 2.14.0.

### Fixed

- An explicit provider resource (`new osano.Provider(...)`) now shows a plugin version change as an
  in-place update, so the stack's state records the version in use. Provider configuration diffs
  ignored the engine-managed `version` (pulumi/pulumi-go-provider#592), so the provider showed as
  unchanged and state kept the old version, which operations that load providers from state, such
  as `pulumi destroy`, then requested. Default providers were not affected. Neither change replaces
  resources.
- `pulumi refresh` and `pulumi up --refresh` converge when a program declares only part of a nested
  configuration object such as `palette` or `translations`. Refresh adopted Osano's whole object,
  with its server defaults, so every refreshed run reported drift and PATCHed the configuration,
  which also left it `outdated` in Osano.
- Refreshing a `Consent` resource for an `anonymousId` no longer drops it from state. The provider
  sent `ref=anonymous`, which Osano rejects with `400`, and read that as "no consent", so the next
  `pulumi up` submitted the consent again. `getUnifiedConsent` and `getSubject` with
  `referenceType: anonymous` returned `exists: false` for the same reason.
- `Consent` sends `attributes` as an empty object when none are set; Osano requires the field.
- SMS subject verification sends the `session` field that Osano requires.
- Cookie Consent list filters encode spaces as `%20`, as Osano documents for the configuration
  name search.

### Deprecated

- `referenceType: anonymous` on `getUnifiedConsent` and `getSubject`. Use `subject` (or omit it).

## [0.1.0] - 2026-09-25

First public release. The provider is community maintained and is not affiliated with Osano, Inc.
or Pulumi Corporation.

### Added

- Cookie Consent (CMP) resources for the Osano Customer REST API:
  - `CookieConsentConfig` manages a CMP configuration: name, domains, compliance mode
    (`debug`, `permissive`, or `production`), the configuration object (which must include
    `storagePolicyHref`), and optional organization IDs. Import it with the Osano config ID.
  - `CookieConsentRule` manages a cookie, script, iframe, or localStorage classification rule with
    its rule type, description, expiry, disclosure, title, and vendor name. Import it with the
    composite ID `<configId>/<ruleId>`. Changing a rule's `configId` or `storeType` replaces it.
  - `CookieConsentPublication` publishes a configuration, waits for Osano to finish, and exports the
    public, non-secret `scriptSrc` (`https://cmp.osano.com/{customerId}/{configId}/osano.js`) and
    the exact `scriptTag` (`<script src="{scriptSrc}"></script>`) to place first in the site `<head>`.
- Unified Consent support for the Unified Consent API:
  - The `Consent` resource submits a consent decision for a verified or anonymous subject, with
    actions, attributes, compliance metadata, jurisdiction, origin, and tags.
  - Functions `getUnifiedConsent`, `getSubject`, `getConfig`, `getCollections`, `getCollection`,
    `checkConsent`, and `getConsentProfile` read consent, subject, configuration, and privacy
    protocol data, and `sendSubjectCode` and `verifySubjectCode` start and verify subject-profile
    challenges with the Customer REST API key.
- Provider configuration `osanoApiKey` (`OSANO_API_KEY`), `unifiedConsentApiKey`
  (`OSANO_UC_API_KEY`), `apiBaseUrl` (`OSANO_API_BASE_URL`), `customerBaseUrl`, and
  `requestTimeoutSeconds` (`OSANO_API_TIMEOUT_SECONDS`, default 60 seconds). A set environment
  variable takes precedence over stack configuration.
- SDKs for Node.js (`@jflavan/pulumi-osano`), Python (`pulumi-osano`), Go
  (`github.com/jflavan/pulumi-osano/sdk/go/osano`), .NET (`Community.Pulumi.Osano`), and Java
  (`io.github.jflavan.pulumi:pulumi-osano`). The SDKs download the matching provider plugin from
  GitHub releases (`github://api.github.com/jflavan/pulumi-osano`) on first use.
- Provider binaries for Linux, macOS, and Windows on amd64 and arm64, with SHA-256 checksums, an
  SBOM per archive, and a SLSA build provenance attestation for each archive and SBOM (verify one
  with `gh attestation verify <archive> --owner jflavan`).
- Cookie Consent (Customer REST API) calls retry `429` and `503` responses with exponential backoff,
  honoring `Retry-After` (capped at one minute). Reads, updates, deletes, and the publish request also
  retry other `5xx` responses, and reads retry dropped connections. Config and rule creates are not
  replayed after an ambiguous `5xx` response, so a server error never creates a duplicate. The
  `Consent` resource and every function, including `sendSubjectCode` and `verifySubjectCode`, send
  each request once, without retries.
- Every HTTP request identifies the provider with the `pulumi-osano/<version>` user agent.
- Documentation: quickstart examples (TypeScript, Python, Go), a canonical Cookie Consent example
  (C# with a TypeScript companion), an end-to-end workflow guide, importing, upgrade,
  troubleshooting, and state management guides, and Pulumi Registry pages (`docs/_index.md`,
  `docs/installation-configuration.md`).

### Cookie Consent behavior to know

- `pulumi preview`, `pulumi refresh`, and `pulumi import` never publish. A `CookieConsentPublication`
  publishes when it is created and when an update changes its `changeToken`,
  `keepUnclassifiedTattles`, `description`, or `webhookUrl`. The caller manages `changeToken`:
  derive it deterministically (for example a SHA-256 hash) from every publish-relevant value so each
  real change publishes exactly once and an unchanged program is a no-op.
- `keepUnclassifiedTattles` defaults to `true`, so publishing preserves unclassified discoveries
  instead of deleting them.
- Publication waits up to the resource's create/update `customTimeouts`, or 20 minutes when none is
  set. Osano's CDN can take up to 15 more minutes to serve a new revision everywhere.
- `pulumi refresh` reports `publishStatus` as `outdated` after an edit in the Osano dashboard but
  does not publish. An imported publication adopts the token
  `import:<lastPublished>:<publishedRevision>`, so applying the program's own token republishes once.
- Deleting a `CookieConsentRule` deletes it in Osano. Osano has no delete or unpublish endpoint for
  configurations, so destroying a `CookieConsentConfig` or `CookieConsentPublication` only removes it
  from Pulumi state; the configuration and its published script stay live upstream.
- Changing provider configuration, such as rotating an API key, updates the provider in place and
  never replaces the Cookie Consent resources it manages.
- Consent records are immutable in Osano; destroying a `Consent` resource only removes it from
  Pulumi state.

### Deprecated

- The `ucApiKey` and `ucBaseUrl` provider configuration keys. They are still read as fallbacks;
  use `unifiedConsentApiKey` and `apiBaseUrl` instead.

[Unreleased]: https://github.com/jflavan/pulumi-osano/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/jflavan/pulumi-osano/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/jflavan/pulumi-osano/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/jflavan/pulumi-osano/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/jflavan/pulumi-osano/releases/tag/v0.1.0
