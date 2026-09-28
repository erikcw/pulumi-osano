# Osano Pulumi Provider

[![Build Status](https://img.shields.io/github/actions/workflow/status/jflavan/pulumi-osano/build.yml?branch=main)](https://github.com/jflavan/pulumi-osano/actions/workflows/build.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/jflavan/pulumi-osano/blob/main/LICENSE)
[![npm version](https://img.shields.io/npm/v/@jflavan/pulumi-osano?label=npm&logo=npm)](https://www.npmjs.com/package/@jflavan/pulumi-osano)
[![PyPI version](https://img.shields.io/pypi/v/pulumi-osano?label=PyPI&logo=pypi&logoColor=white)](https://pypi.org/project/pulumi-osano/)
[![NuGet version](https://img.shields.io/nuget/v/Community.Pulumi.Osano?label=NuGet&logo=nuget)](https://www.nuget.org/packages/Community.Pulumi.Osano)
[![Go module version](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fproxy.golang.org%2Fgithub.com%2Fjflavan%2Fpulumi-osano%2Fsdk%2Fgo%2Fosano%2F%40latest&query=%24.Version&label=Go&logo=go&logoColor=white)](https://pkg.go.dev/github.com/jflavan/pulumi-osano/sdk/go/osano)
[![Maven Central version](https://img.shields.io/maven-central/v/io.github.jflavan.pulumi/pulumi-osano?label=Maven%20Central&logo=apachemaven)](https://central.sonatype.com/artifact/io.github.jflavan.pulumi/pulumi-osano)

> **⚠️ Unofficial community provider**
>
> This repo is not affiliated with Osano or Pulumi. It is maintained by the community and provided "as is" under the MIT license. Use [GitHub Issues](https://github.com/jflavan/pulumi-osano/issues) for support.

The provider lets you manage Osano Cookie Consent and Unified Consent workflows alongside the rest of your infrastructure-as-code. You can:

- Create Cookie Consent configurations and rules, publish them after all dependencies settle, and export the hosted CMP script URL and exact HTML tag, so the same pipeline that provisions a website can put the consent script first in its `<head>`.
- Look up the script, publish status, rules, discoveries, and audit log of any Cookie Consent configuration with `getCookieConsentConfig`, `getCookieConsentConfigs`, `getCookieConsentRules`, `getCookieConsentDiscoveries`, and `getCookieConsentAuditLog`, for example to consume a centrally managed configuration from a website stack or to gate a switch to production mode.
- Submit consent decisions programmatically from Pulumi deployments, including Global Privacy Control consents.
- Query unified consent state for a subject using Pulumi invokes.
- Resolve verified, anonymous, and session references via `osano.getSubject`, `osano.getSubjectProfile`, and `osano.getSession` when stitching identity flows.
- Inspect UC configuration and privacy protocol collections with `osano.getConfig`, `osano.getCollections`, and `osano.getCollection` invokes.
- Check for existing consent state and hashed consent profiles with `osano.checkConsent` and `osano.getConsentProfile`.
- Start and verify subject-profile challenges via `osano.sendSubjectCode` and `osano.verifySubjectCode`. Pulumi runs invokes on every preview, update, and refresh, so call these two from automation rather than declaring them in a long-lived stack; otherwise each run sends a new code.
- Wire Osano calls into your CI/CD pipelines with first-class Node.js, Python, Go, .NET, and Java SDKs.

## Table of contents

1. [Prerequisites](#prerequisites)
2. [Installation](#installation)
3. [Quick start: publish a consent script](#quick-start-publish-a-consent-script)
4. [Cookie Consent end to end](#cookie-consent-end-to-end)
5. [Unified Consent](#unified-consent)
6. [Authentication](#authentication)
7. [Configuration](#configuration)
8. [Examples](#examples)
9. [Development](#development)

---

## Prerequisites

- Pulumi CLI v3+
- API access to an Osano tenant (a Customer REST API key for Cookie Consent, a Unified Consent API key for Unified Consent, or both for mixed workloads)
- Runtime for your preferred language: Node.js 22+ (required by current `@pulumi/pulumi` releases), Python 3.10+, Go 1.26.6+, .NET 8+, or Java 11+

## Installation

Add the SDK for your language to a Pulumi program. Each package is published to its language's registry; the badges above show the latest version:

- **Node.js**: [`@jflavan/pulumi-osano`](https://www.npmjs.com/package/@jflavan/pulumi-osano) on npm: `npm install @jflavan/pulumi-osano`
- **Python**: [`pulumi-osano`](https://pypi.org/project/pulumi-osano/) on PyPI, imported as `pulumi_osano`: `pip install pulumi-osano`
- **Go**: [`github.com/jflavan/pulumi-osano/sdk/go/osano`](https://pkg.go.dev/github.com/jflavan/pulumi-osano/sdk/go/osano): `go get github.com/jflavan/pulumi-osano/sdk/go/osano`
- **.NET**: [`Community.Pulumi.Osano`](https://www.nuget.org/packages/Community.Pulumi.Osano) on NuGet: `dotnet add package Community.Pulumi.Osano`
- **Java**: [`io.github.jflavan.pulumi:pulumi-osano`](https://central.sonatype.com/artifact/io.github.jflavan.pulumi/pulumi-osano) on Maven Central. With Gradle, `implementation("io.github.jflavan.pulumi:pulumi-osano:0.3.0")`; with Maven:

  ```xml
  <dependency>
    <groupId>io.github.jflavan.pulumi</groupId>
    <artifactId>pulumi-osano</artifactId>
    <version>0.3.0</version>
  </dependency>
  ```

The SDK declares its provider plugin, and Pulumi downloads the matching `pulumi-resource-osano` release from GitHub the first time you run `pulumi preview` or `pulumi up`. To install it manually, pin the version and point Pulumi at the GitHub releases:

```bash
pulumi plugin install resource osano 0.3.0 --server github://api.github.com/jflavan/pulumi-osano
```

The Java and plugin commands pin 0.3.0, the current release. [Package publishing](https://github.com/jflavan/pulumi-osano/blob/main/docs/PUBLISHING.md) lists every published artifact, how each one is released, and how to verify its provenance or signature.

## Quick start: publish a consent script

This TypeScript program creates a Cookie Consent configuration with one rule, publishes it, and hands the script tag to the rest of the program. It assumes a project created with `pulumi new typescript`:

```bash
npm install @jflavan/pulumi-osano
pulumi config set osano:osanoApiKey --secret   # Customer REST API key
pulumi up
```

`index.ts`:

```ts
import { createHash } from "crypto";
import * as pulumi from "@pulumi/pulumi";
import * as osano from "@jflavan/pulumi-osano";

const desired = {
  name: "www-example-com",
  domains: ["www.example.com"],
  mode: "permissive",
  configuration: { storagePolicyHref: "https://www.example.com/privacy" },
  rules: [{ storeType: "cookies", classification: "ANALYTICS", rule: "_ga", ruleType: "EXACT_MATCH" }],
};

const config = new osano.CookieConsentConfig("consent", {
  name: desired.name,
  domains: desired.domains,
  mode: desired.mode,
  configuration: desired.configuration,
});
const rules = desired.rules.map((rule, i) =>
  new osano.CookieConsentRule(`rule-${i}`, { configId: config.configId, ...rule }));

// Publish exactly once per change: derive the token from everything that is published.
const publication = new osano.CookieConsentPublication("publication", {
  configId: config.configId,
  changeToken: createHash("sha256").update(JSON.stringify(desired)).digest("hex"),
}, { dependsOn: [config, ...rules], customTimeouts: { create: "20m", update: "20m" } });

// Hand the tag to whatever renders or configures the site's <head>; it must come first.
export const scriptTag = publication.scriptTag;
export const headHtml = pulumi.interpolate`<head>\n  ${publication.scriptTag}\n</head>`;
```

`pulumi preview` never publishes. `pulumi up` creates the configuration and rule, publishes, waits for Osano to finish, and returns `<script src="https://cmp.osano.com/{customerId}/{configId}/osano.js"></script>`. Running it again without changes publishes nothing.

If you're working from a repository clone instead of published packages, the repo-local examples under [examples](https://github.com/jflavan/pulumi-osano/tree/main/examples) are aimed at contributors: they build against the SDKs generated in the clone and need a locally built provider plugin. Follow the setup in the [quickstart README](https://github.com/jflavan/pulumi-osano/blob/main/examples/quickstart/README.md), which also shows how to switch an example to the published packages.

## Cookie Consent end to end

The canonical [C# Cookie Consent example](https://github.com/jflavan/pulumi-osano/tree/main/examples/cookie-consent) creates a CMP configuration and its rules, then uses `CookieConsentPublication` to publish only after those resources settle. The companion TypeScript example implements the same lifecycle. Both compute a deterministic `changeToken`, declare explicit [`dependsOn`](https://www.pulumi.com/docs/iac/concepts/resources/options/dependson/) relationships, and allow a twenty-minute [`customTimeouts`](https://www.pulumi.com/docs/iac/concepts/resources/options/customtimeouts/) window.

Cookie Consent resources require a Customer REST API key:

```bash
export OSANO_API_KEY="replace-with-a-customer-rest-api-key"
```

After publication succeeds, the resource exposes these exact public outputs:

```csharp
var publication = new CookieConsentPublication(/* ... */);

return new Dictionary<string, object?>
{
    ["cookieConsentScriptSrc"] = publication.ScriptSrc, // scriptSrc
    ["cookieConsentScriptTag"] = publication.ScriptTag, // scriptTag
};
```

`scriptSrc` has the form `https://cmp.osano.com/{customerId}/{configId}/osano.js`; `scriptTag` is exactly `<script src="{scriptSrc}"></script>`. These installation values are deliberately non-secret. Put the returned tag first in the site `<head>` without `async` or `defer`, so the CMP loads before scripts it may control. The URL never changes between revisions, so a website only needs it once. Publication completion and CDN propagation are separate: Osano's CDN can take up to 15 minutes to serve a new revision, and browsers cache `osano.js` for up to 24 hours.

To use the script in another stack, such as one per website, read it with `getCookieConsentConfig` instead of managing the configuration there:

```ts
const consent = osano.getCookieConsentConfigOutput({ configId: "<config-id>" });
export const headScript = consent.scriptTag;     // the same value the publication exports
export const published = consent.publishStatus;  // the URL returns 403 until the first publish
```

Before switching a configuration to `production` mode, which blocks everything unclassified, `getCookieConsentDiscoveries` lists what osano.js has discovered that no rule covers yet. The [end-to-end workflow guide](https://github.com/jflavan/pulumi-osano/blob/main/docs/end-to-end-workflow.md) covers the whole pipeline, including Content Security Policy settings and per-environment configurations.

See Osano's [Consent JavaScript API](https://developers.osano.com/cmp/javascript-api/developer-documentation-consent-javascript-api) and direct Customer REST API [`publishConfig` operation](https://developers.osano.com/customer-rest-api#tag/cmp/operation/publishConfig) for the upstream contracts.

## Unified Consent

The `Consent` resource submits a consent decision, and the functions read consent state back. This TypeScript snippet assumes a project created with `pulumi new typescript`:

```bash
npm install @jflavan/pulumi-osano
pulumi config set osano:unifiedConsentApiKey --secret
pulumi config set subjectRef <subject-id> --secret
pulumi config set configId <config-id>
pulumi config set privacyProtocolId <protocol-id>
pulumi up
```

`index.ts`:

```ts
import * as pulumi from "@pulumi/pulumi";
import * as osano from "@jflavan/pulumi-osano";

const cfg = new pulumi.Config();
const subjectRef = cfg.requireSecret("subjectRef");
const configId = cfg.require("configId");
const privacyProtocolId = cfg.require("privacyProtocolId");
const subjectType = cfg.get("subjectType") ?? "verified";

const subject = subjectRef.apply((value) =>
  subjectType === "anonymous" ? { anonymousId: value } : { verifiedId: value }
);

const consent = new osano.Consent("example", {
  subject,
  actions: [
    { target: privacyProtocolId, vendor: configId, action: "ACCEPT" },
  ],
  attributes: { pulumiStack: pulumi.getStack() },
  origin: "api",
  tags: ["demo"],
});

export const consentId = consent.consentId;
```

Run `pulumi up` to submit the consent. The subject is stored as a secret. Every `Consent` input replaces the resource when it changes, which submits a new consent; `pulumi refresh` never resubmits one. Destroying the stack removes the logical Pulumi resource but does **not** delete historical events from Osano (they are immutable).

Look anonymous and verified IDs up with the default `referenceType` (`subject`); `session` resolves a session ID. To submit a Global Privacy Control consent, set `origin: "gpc"` and omit `actions`: Osano derives the actions and the resource exports them as `gpcActions`. When a pipeline submits consents on a subject's behalf, set `countryCodeOverride` (and `regionCodeOverride`) so Osano does not geolocate the CI runner. The runnable version of this program is in [examples/quickstart](https://github.com/jflavan/pulumi-osano/tree/main/examples/quickstart).

## Authentication

Two API keys exist:

| Key | Header | Usage |
| --- | --- | --- |
| Unified Consent API key | `x-uc-api-key` | Required for consent submissions and read operations |
| Osano Customer REST API key | `x-osano-api-key` | Required for Cookie Consent resources and functions; `sendSubjectCode` and `verifySubjectCode` send every configured key, so either this key or the Unified Consent API key is enough |

Configure them with Pulumi config:

```bash
pulumi config set osano:unifiedConsentApiKey --secret
pulumi config set osano:osanoApiKey --secret   # Cookie Consent; also accepted for subject verification
```

Or set environment variables for CI. An environment variable is used only when the stack does not configure the key:

```bash
export OSANO_UC_API_KEY="..."
export OSANO_API_KEY="..."
```

## Configuration

Provider-level settings (all optional). Stack configuration takes precedence over the environment variables, which are used only when the key is unset; the provider warns when both are set and differ:

| Key | Description |
| --- | --- |
| `osano:unifiedConsentApiKey` | Unified Consent API key (secret); `OSANO_UC_API_KEY` is used when it is unset |
| `osano:osanoApiKey` | Customer REST API key (secret) for Cookie Consent, also accepted by subject verification; `OSANO_API_KEY` is used when it is unset |
| `osano:apiBaseUrl` | Base URL of the Unified Consent API, including any path prefix; defaults to `https://uc.api.osano.com`; `OSANO_API_BASE_URL` is used when it is unset. Must use `https` (`http` only for loopback hosts) |
| `osano:customerBaseUrl` | Base URL of the Customer REST API; defaults to `https://api.osano.com`; `OSANO_CUSTOMER_BASE_URL` is used when it is unset. Must use `https` (`http` only for loopback hosts) |
| `osano:requestTimeoutSeconds` | Timeout for each request attempt, 1 to 3600 seconds, default 60; `OSANO_API_TIMEOUT_SECONDS` is used when it is unset |

The deprecated `osano:ucApiKey` and `osano:ucBaseUrl` keys are still read when `unifiedConsentApiKey` and `apiBaseUrl` are unset.

Resource-level inputs are documented in the generated SDKs, for example the [Go package reference](https://pkg.go.dev/github.com/jflavan/pulumi-osano/sdk/go/osano).

## Examples

- [examples/quickstart/typescript](https://github.com/jflavan/pulumi-osano/tree/main/examples/quickstart/typescript)
- [examples/quickstart/python](https://github.com/jflavan/pulumi-osano/tree/main/examples/quickstart/python)
- [examples/quickstart/go](https://github.com/jflavan/pulumi-osano/tree/main/examples/quickstart/go)
- [examples/cookie-consent](https://github.com/jflavan/pulumi-osano/tree/main/examples/cookie-consent) (canonical C# and companion TypeScript)

These repo-local examples contain `Pulumi.yaml` plus language-specific dependency files that reference the SDKs generated in this repository. The shared [quickstart README](https://github.com/jflavan/pulumi-osano/blob/main/examples/quickstart/README.md) documents the local SDK and plugin setup required when running them from a clone, and how to switch an example to the published packages.

## Development

1. Install toolchain dependencies: `eval "$(mise activate zsh)" && mise install`
2. Build the provider: `make provider`
3. Run tests and lint: `make test_provider` and `make lint` (`make test_all` runs every suite)
4. Regenerate schema + SDKs after editing Go code: `make codegen`

For the full lifecycle (install, deploy, day-2 changes, import, teardown, and the contributor loop) see the [end-to-end workflow guide](https://github.com/jflavan/pulumi-osano/blob/main/docs/end-to-end-workflow.md). For releases, see the [release guide](https://github.com/jflavan/pulumi-osano/blob/main/docs/RELEASE_GUIDE.md) and [package publishing](https://github.com/jflavan/pulumi-osano/blob/main/docs/PUBLISHING.md). See [CONTRIBUTING.md](https://github.com/jflavan/pulumi-osano/blob/main/CONTRIBUTING.md) and the [docs](https://github.com/jflavan/pulumi-osano/tree/main/docs) for troubleshooting tips and workflows.
