# Upgrade Guide

This guide captures breaking changes and migration tips between provider versions.

## Upgrading from 0.2.x to 0.3.0

`0.3.0` keeps every resource and function token and never replaces an Osano resource by itself.
Upgrade the SDK, run `pulumi preview`, and check the items below that apply to your program. The
first preview shows the provider change described in
[What the first preview shows for the provider](#what-the-first-preview-shows-for-the-provider)
(`default_0_3_0` replaces `default_0_2_1`) and nothing else.

### Provider configuration

- **Stack configuration now wins over environment variables.** If a pipeline exports
  `OSANO_API_KEY`, `OSANO_UC_API_KEY`, `OSANO_API_BASE_URL`, or `OSANO_API_TIMEOUT_SECONDS` while
  the stack also sets the matching `osano:` key, 0.2.x used the environment variable and 0.3.0 uses
  the stack configuration, with a warning that names both. To keep using the environment variable,
  remove the stack key (`pulumi config rm osano:osanoApiKey`).
- **`http` base URLs are rejected** unless the host is a loopback address. A stack that pointed
  `apiBaseUrl` or `customerBaseUrl` at a plain-`http` proxy fails to configure with
  `API keys are only sent over https`; use an `https` endpoint or a loopback tunnel.
- **`requestTimeoutSeconds` above 3600 fails**, and an `OSANO_API_TIMEOUT_SECONDS` value outside
  1 to 3600 is ignored with a warning. The timeout applies to each attempt, so a retried request
  can take longer in total than before.
- `OSANO_CUSTOMER_BASE_URL` is new; nothing changes while it is unset.

### Retries

Unified Consent lookups and `Consent` refreshes are now retried after transient failures, and
`CookieConsentConfig`, `CookieConsentRule`, and `Consent` creates are no longer retried after
`503`, only after `429`. A failed `CookieConsentConfig` create looks for the configuration Osano may
have created anyway and adopts it when exactly one matches; otherwise see
[troubleshooting](troubleshooting.md#cookie-consent-create-failed-with-a-server-error).

### Secrets

- `Consent.subject` and the subject identifiers and personal data returned by the Unified Consent
  functions are secrets; the [changelog](../CHANGELOG.md#unreleased) lists them. Stack outputs
  built from them show as `[secret]`; use `pulumi stack output --show-secrets` to read them.
- Existing `Consent` resources show no diff. The resource compares inputs by value, so the SDK now
  sending `subject` as a secret is not a change, and nothing is submitted again. The value is
  stored encrypted the next time the resource is written to state, for example by `pulumi refresh`.
- A value from one of these outputs makes any resource input it flows into secret as well. When the
  target is not sensitive, unwrap it with your SDK's `unsecret` helper.

### `Consent` refresh and import

- `pulumi refresh` no longer removes a `Consent` whose subject Osano reports without consent; it
  warns and keeps the resource. If you relied on refresh to resubmit such consents, submit them
  deliberately: change an input, which replaces the resource, or run `pulumi up --replace <urn>`.
- `pulumi import osano:index:Consent` now fails with an explicit error. It never produced a usable
  resource.
- `lastSynced` no longer changes on refresh; it is the submission time.

### Cookie Consent

- `CookieConsentRule`: an update no longer clears an optional field the program has never set
  (`title`, `vendorName`, `ruleType`, and for cookies `description` and `expiry`). To clear a value
  set in the Osano dashboard, set the field in the program, apply, then remove it and apply again.
- `CookieConsentConfig`: a `configuration` key removed from the program is cleared in Osano on the
  next update, where 0.2.x left it set. `pulumi preview` shows the removal before `pulumi up` sends
  it.
- A publication that failed with `Osano did not start a new publication` while Osano still showed
  the previous publication's error now waits for its `customTimeouts` (20 minutes when none is
  set). Set `customTimeouts` explicitly, as the examples do.
- Previews of new resources show their inputs; a `CookieConsentPublication` preview shows the
  configuration ID as its ID, and a `Consent` preview no longer shows a placeholder `consentId`.

### Runtime and toolchain requirements

| Language | Requires |
| --- | --- |
| Node.js | unchanged: Node.js 22 or later |
| Python | unchanged: Python 3.10 or later; `pulumi>=3.231.0` |
| Go | unchanged: Go 1.26.6 or later. The SDK module no longer pins a `toolchain`, so `go get` no longer downloads Go 1.27.1 |
| .NET | .NET 8 or later, as documented before. The package now targets `net8.0` (was `net6.0`), so a project that still targets `net6.0` or `net7.0` must move to `net8.0` or later |
| Java | unchanged: Java 11 or later |

## Upgrading from 0.2.0 to 0.2.1

`0.2.1` changes no resource, function, input, output, or provider setting, and the provider
plugin behaves as in `0.2.0`. It republishes the SDKs so that npm, PyPI, NuGet, and pkg.go.dev
show a README with examples in their own language. Update the pinned version; nothing else is
needed.

## Upgrading from 0.1.0 to 0.2.0

`0.2.0` keeps every resource and function token and never replaces a Cookie
Consent config, rule, or publication. Upgrade the SDK, run `pulumi preview`,
and check the items below that apply to your program.

### Runtime and toolchain requirements

| Language | Requires |
| --- | --- |
| Node.js | Node.js 22 or later (the current `@pulumi/pulumi` releases dropped Node.js 20) |
| Python | Python 3.10 or later; `pulumi>=3.231.0` |
| Go | Go 1.26.6 or later; `github.com/pulumi/pulumi/sdk/v3` v3.264.0 or later (`go get` raises it) |
| .NET | unchanged: `net6.0` package, tested on .NET 8 and .NET 10 |
| Java | unchanged: Java 11 or later |

A Go program that uses the SDK through a `replace` directive now requests the
SDK's own plugin version, so a plugin installed as `0.0.0` for that workaround is
no longer used.

### What the first preview shows for the provider

- Default providers are named after their version, so the first `pulumi
  preview` shows a new `default_0_2_0` provider created and `default_0_1_0`
  deleted. Your Osano resources move to the new provider unchanged.
- An explicit provider resource (`new osano.Provider(...)`) shows an update of
  `version`. 0.1.0 reported no change there and kept the old version in state;
  the update records the new one. It never replaces resources.

### Cookie Consent

- `CookieConsentConfig` now validates `configuration` during preview. A value
  Osano would have rejected with `400` at `pulumi up` (for example
  `tattleSampling: 2` or a `variantMapping` without `behavior`) now fails
  preview with the property path. A configuration that is unchanged since your
  last `pulumi up` only produces warnings, so the upgrade itself never blocks a
  stack; the check applies once you change the configuration. Keys the spec
  does not list, deprecated palette keys, and `ccpaRelaxed` next to a
  `variantMapping` also produce warnings; fix them or keep them deliberately.
- If your program declares part of a nested object such as `palette`, a
  `pulumi up --refresh` with 0.1.0 PATCHed the configuration on every run and
  left it `outdated`. After upgrading, one refresh brings state back in line and
  later runs are no-ops. The configuration may still show `outdated` in Osano
  until the next deliberate publication (change the publication's
  `changeToken`).
- `CookieConsentPublication.webhookUrl` is a secret. Existing state shows no
  diff and nothing republishes; the value is stored encrypted from the next
  update of that publication.

### Unified Consent

- `referenceType: anonymous` is deprecated and now sends `ref=subject`, which is
  what Osano expects for anonymous IDs. With 0.1.0, those lookups returned
  `exists: false`, and a refresh removed `Consent` resources with an
  `anonymousId` from state (the next `pulumi up` submitted them again). Replace
  `anonymous` with `subject` or remove it. Any value other than `subject`,
  `session`, or `anonymous` now fails the call.
- A new or changed `Consent` must use an action of `ACCEPT`, `REJECT`, or
  `UNSELECTED` (case-sensitive) and an `origin` of `api` or `gpc`, the values
  Osano documents. Existing consents whose values have not changed keep
  previewing. Subject IDs containing `#`, `%`, or spaces, a `compliance.gpc`
  other than 0 or 1, and malformed `countryCodeOverride`/`regionCodeOverride`
  codes fail at preview; Osano rejects them too.
- `verifySubjectCode` with `phone` requires `session`, the challenge session
  from the SMS code request. `hashedSubjectId` is optional on both subject
  verification functions.
- `Consent.actions` is optional, so the `actions` output is typed as optional
  in the SDKs (for example `Output<ConsentAction[] | undefined>` in
  TypeScript). Code that reads `consent.actions` may need a null check.
- New `Consent` inputs (`sessionToken`, `countryCodeOverride`,
  `regionCodeOverride`) replace the consent when changed, like every other
  `Consent` input, because consents are immutable in Osano. Adding them to an
  existing resource therefore submits a new consent record.

## Before 0.1.0

`v0.1.0` (2026-09-25) is the first published release, so a stack that starts
from the published packages has nothing to migrate. The next two sections
record behavior changes made during development before that release. They
matter only for a stack created with a provider built from source before
`v0.1.0`; moving such a stack to the published `0.1.0` packages picks up all of
them.

### Review hardening release

These fixes change behavior without changing resource tokens:

- `Consent` refresh keeps the submitted inputs and only updates `lastSynced`.
  Earlier builds copied the subject's merged Unified Consent view into the
  inputs, so the next `pulumi up` replaced the resource and submitted a
  duplicate consent. Run `pulumi preview` after upgrading. If a stack refreshed
  by an older build shows a `Consent` replacement caused only by `actions` or
  `attributes`, applying it would submit a new consent record; to keep the
  existing record instead, add `ignoreChanges: ["actions", "attributes"]` to
  that resource.
- Unified Consent requests keep any path prefix on `osano:apiBaseUrl` or
  `OSANO_API_BASE_URL` (for example a proxy mounted at `/osano`). Earlier builds
  sent `/v2/...` to the host root.
- Cookie Consent config and rule creates are no longer retried after ambiguous
  `500`/`502`/`504` responses. `429` and `503` are still retried, and
  publication requests still retry every retryable status.
- `verifySubjectCode.code` is now a secret input.
- `CookieConsentConfig` preview accepts inputs that are unknown until apply, and
  rule limits count characters rather than bytes.
- Update previews keep `configId`, `customerId`, `ruleId`, `scriptSrc`, and
  `scriptTag` known. Earlier builds marked them unknown whenever any input
  changed, so editing a config previewed every dependent rule and publication as
  a replacement (the real update did not replace them).
- `Consent` validation runs in `Check` and skips inputs that are unknown until
  apply, so a `vendor` or `target` wired from another resource's output no longer
  fails preview.
- Cookie Consent GET requests retry transport failures (for example a dropped
  connection during a long publication wait); writes still never replay after a
  transport failure.
- Provider configuration changes (rotating `osano:osanoApiKey`, adding
  `osano:requestTimeoutSeconds`, or changing a base URL) now update the provider
  in place. Earlier builds reported every provider config change as a
  replacement, so Pulumi replaced every Cookie Consent config, rule, and
  publication and created duplicate Osano configurations. The same defect made
  the first `pulumi up` after `pulumi import` replace the imported resources.
- `CookieConsentConfig` and `CookieConsentRule` state now stores inputs as
  applied. Keys Osano adds to `configuration` and server defaults for optional
  rule fields the program leaves unset no longer produce a diff on every
  `pulumi up`; refresh still surfaces drift in declared values.
- Config create and update omit `orgIds` unless it is set and send an empty
  list only to clear a previously managed value, instead of sending `null`.
- A `CookieConsentPublication.webhookUrl` wired from another resource's output
  no longer fails preview while it is unknown.

### Cookie Consent publication release

This release adds `osano:index:CookieConsentPublication` without changing the
tokens of existing resources. The resource queues asynchronous Cookie Consent
publication after configuration/rule dependencies settle, waits for completion,
and returns public `scriptSrc` and `scriptTag` installation outputs. It requires
a deterministic caller-managed `changeToken`; unchanged inputs do not publish.
Preview, refresh, import, and delete never publish.

`CookieConsentRule` adds optional `ruleType`, `description`, and `expiry` fields.
`description` and `expiry` apply only to cookie rules. Removed nullable fields
are now cleared upstream, list reads paginate, and missing rules/configurations
are treated as deleted state.

#### Customer REST credentials and timeouts

Cookie Consent resources now share the Customer REST configuration path:

- `OSANO_API_KEY` takes precedence over secret `osano:osanoApiKey`.
- `osano:customerBaseUrl` defaults to `https://api.osano.com`.
- A valid positive `OSANO_API_TIMEOUT_SECONDS` takes precedence over
  `osano:requestTimeoutSeconds`; otherwise provider config/default 60 seconds is
  used for individual Customer REST and Unified Consent HTTP calls.
- Publication waits for the resource's Pulumi create/update `customTimeouts`
  and stops after twenty minutes when none is set; the examples set twenty
  minutes explicitly.

If credentials were previously supplied only for administrative routes, verify
the same key is authorized for Customer REST CMP operations before applying.

#### Rule identities and adoption

New and imported rule state uses composite `<configId>/<ruleId>` identities:

```bash
pulumi import osano:index:CookieConsentRule analyticsRule <configId>/<ruleId>
```

Existing tracked numeric rule IDs remain readable with their state-held
`configId` and normalize on refresh without replacing the upstream rule. Review
the preview after upgrading, especially if state was manually edited.

Publication import uses the config ID and adopts
`import:<lastPublished>:<publishedRevision>` without publishing. Replacing that
adoption token with the program's intended token may cause one controlled
republish.

#### Delete behavior

`CookieConsentRule` deletion is a real upstream delete. Osano exposes no
delete/unpublish endpoint for Cookie Consent configurations or publications, so
deleting either of those resources removes only Pulumi state. A destroyed stack
therefore retains the upstream configuration and published script; review and
disable retained customer resources in Osano separately when required.

## Pinning pre-1.0 releases

The provider is pre-1.0 (`0.x`, starting with `v0.1.0`). Under semantic
versioning a `0.x` minor release may contain breaking changes, so pin the SDK
package to an exact version in your program's dependency manifest and review
the [CHANGELOG](../CHANGELOG.md) and
[release notes](https://github.com/jflavan/pulumi-osano/releases) before
upgrading. The Pulumi engine installs the matching provider plugin for the
pinned SDK version.

| Language | Exact pin |
| --- | --- |
| Node.js | `npm install --save-exact @jflavan/pulumi-osano@0.2.1` (`package.json`) |
| Python | `pulumi-osano==0.2.1` in `requirements.txt` |
| Go | `go get github.com/jflavan/pulumi-osano/sdk/go/osano@v0.2.1` (`go.mod`) |
| .NET | `<PackageReference Include="Community.Pulumi.Osano" Version="0.2.1" />` in the `.csproj` |
| Java | `io.github.jflavan.pulumi:pulumi-osano:0.2.1` in `pom.xml` or `build.gradle` |

- Fields may be renamed as Osano expands the API. Review the release notes for
  each version and update your Pulumi code accordingly.
- When new required fields are added, run `pulumi preview` to spot the diff
  before applying.

### SDK package names

| Language | Package | Import |
| --- | --- | --- |
| Node.js | `@jflavan/pulumi-osano` (npm) | `import * as osano from "@jflavan/pulumi-osano";` |
| Python | `pulumi-osano` (PyPI) | `import pulumi_osano as osano` |
| Go | `github.com/jflavan/pulumi-osano/sdk/go/osano` | `import "github.com/jflavan/pulumi-osano/sdk/go/osano"` |
| .NET | `Community.Pulumi.Osano` (NuGet) | `using Community.Pulumi.Osano;` |
| Java | `io.github.jflavan.pulumi:pulumi-osano` (Maven Central) | `import io.github.jflavan.pulumi.osano.*;` |

[PUBLISHING.md](PUBLISHING.md) links each registry page. After upgrading, check
your lockfile to confirm the new version is installed.

### Rolling out upgrades safely

1. Read the [CHANGELOG](../CHANGELOG.md) entries between your current and
   target versions.
2. Change the pinned SDK version in your dependency manifest and reinstall
   dependencies (`npm install`, `pip install -r requirements.txt`,
   `go get github.com/jflavan/pulumi-osano/sdk/go/osano@vX.Y.Z`,
   `dotnet restore`, or your Maven or Gradle build). Pulumi downloads the
   matching provider plugin on the next run.
3. Run `pulumi preview` on a staging stack and review every diff, especially
   replacements.
4. Deploy to the staging stack before touching production.

If you build the SDKs from a clone instead, regenerate and compile them first
with `make codegen && make build_sdks` and `make build_cookie_consent_examples`.

Report regressions as GitHub issues with stack traces and the Osano API response if available.
