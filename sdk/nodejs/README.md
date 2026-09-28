# Osano Pulumi Provider for Node.js

[![Build Status](https://img.shields.io/github/actions/workflow/status/jflavan/pulumi-osano/build.yml?branch=main)](https://github.com/jflavan/pulumi-osano/actions/workflows/build.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/jflavan/pulumi-osano/blob/main/LICENSE)
[![npm version](https://img.shields.io/npm/v/@jflavan/pulumi-osano?label=npm&logo=npm)](https://www.npmjs.com/package/@jflavan/pulumi-osano)

> **⚠️ Unofficial community provider**
>
> This package is not affiliated with Osano or Pulumi. It is maintained by the community and provided "as is" under the MIT license. Use [GitHub Issues](https://github.com/jflavan/pulumi-osano/issues) for support.

`@jflavan/pulumi-osano` is the TypeScript and JavaScript SDK for the Osano Pulumi provider. It lets you manage Osano Cookie Consent and Unified Consent workflows alongside the rest of your infrastructure-as-code. You can:

- Create Cookie Consent configurations and rules, publish them after all dependencies settle, and export the hosted CMP script URL and exact HTML tag, so the same pipeline that provisions a website can put the consent script first in its `<head>`.
- Look up the script, publish status, rules, discoveries, and audit log of any Cookie Consent configuration with `getCookieConsentConfig`, `getCookieConsentConfigs`, `getCookieConsentRules`, `getCookieConsentDiscoveries`, and `getCookieConsentAuditLog`, for example to consume a centrally managed configuration from a website stack or to gate a switch to production mode.
- Submit consent decisions programmatically from Pulumi deployments, including Global Privacy Control consents.
- Query unified consent state for a subject with `getUnifiedConsent`, and resolve verified, anonymous, and session references with `getSubject`, `getSubjectProfile`, and `getSession`.
- Inspect UC configuration and privacy protocol collections with `getConfig`, `getCollections`, and `getCollection`, and check for existing consent state and hashed consent profiles with `checkConsent` and `getConsentProfile`.
- Start and verify subject-profile challenges with `sendSubjectCode` and `verifySubjectCode`. Pulumi runs functions on every preview, update, and refresh, so call these two from automation rather than declaring them in a long-lived stack; otherwise each run sends a new code.

SDKs for Python, Go, .NET, and Java are also available; see the [project README](https://github.com/jflavan/pulumi-osano#readme).

## Prerequisites

- Pulumi CLI v3+
- Node.js 22+ (required by current `@pulumi/pulumi` releases)
- API access to an Osano tenant: a Customer REST API key for Cookie Consent, a Unified Consent API key for Unified Consent, or both for mixed workloads

## Installation

```bash
npm install @jflavan/pulumi-osano
```

The package declares its provider plugin, and Pulumi downloads the matching `pulumi-resource-osano` release from GitHub the first time you run `pulumi preview` or `pulumi up`. To install it manually, pin the version and point Pulumi at the GitHub releases:

```bash
pulumi plugin install resource osano 0.2.1 --server github://api.github.com/jflavan/pulumi-osano
```

[Package publishing](https://github.com/jflavan/pulumi-osano/blob/main/docs/PUBLISHING.md) describes how each release is built and how to verify its provenance.

## Quick start: publish a consent script

This program creates a Cookie Consent configuration with one rule, publishes it, and hands the script tag to the rest of the program. It assumes a project created with `pulumi new typescript`:

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

The publication exports `scriptSrc` (`https://cmp.osano.com/{customerId}/{configId}/osano.js`) and `scriptTag` (exactly `<script src="{scriptSrc}"></script>`). These installation values are deliberately non-secret. Put the tag first in the site `<head>` without `async` or `defer`, so the CMP loads before scripts it may control. The URL never changes between revisions, so a website only needs it once. Publication completion and CDN propagation are separate: Osano's CDN can take up to 15 minutes to serve a new revision, and browsers cache `osano.js` for up to 24 hours.

## Read a configuration from another stack

To use the script in another stack, such as one per website, read it with `getCookieConsentConfigOutput` instead of managing the configuration there:

```ts
const consent = osano.getCookieConsentConfigOutput({ configId: "<config-id>" });
export const headScript = consent.scriptTag;     // the same value the publication exports
export const published = consent.publishStatus;  // the URL returns 403 until the first publish
```

Before switching a configuration to `production` mode, which blocks everything unclassified, `getCookieConsentDiscoveries` lists what osano.js has discovered that no rule covers yet. The [end-to-end workflow guide](https://github.com/jflavan/pulumi-osano/blob/main/docs/end-to-end-workflow.md) covers the whole pipeline, including Content Security Policy settings and per-environment configurations.

## Unified Consent

The `Consent` resource submits a consent decision, and the functions read consent state back. This program assumes a project created with `pulumi new typescript`:

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

Destroying the stack removes the logical Pulumi resource but does **not** delete historical events from Osano (they are immutable).

`getSubject` and `getUnifiedConsent` look anonymous and verified IDs up with the default `referenceType` (`subject`); `session` resolves a session ID. To submit a Global Privacy Control consent, set `origin: "gpc"` and omit `actions`: Osano derives the actions and the resource exports them as `gpcActions`. When a pipeline submits consents on a subject's behalf, set `countryCodeOverride` (and `regionCodeOverride`) so Osano does not geolocate the CI runner.

## Authentication

Two API keys exist:

| Key | Header | Usage |
| --- | --- | --- |
| Unified Consent API key | `x-uc-api-key` | Required for consent submissions and read operations |
| Osano Customer REST API key | `x-osano-api-key` | Required for Cookie Consent resources and functions; `sendSubjectCode` and `verifySubjectCode` send every configured key, so either this key or the Unified Consent API key is enough |

Configure them with Pulumi config:

```bash
pulumi config set osano:unifiedConsentApiKey --secret
pulumi config set osano:osanoApiKey --secret
```

Or set the `OSANO_UC_API_KEY` and `OSANO_API_KEY` environment variables, for example in CI; they are used only when the stack does not configure the key.

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

## Learn more

- [Examples](https://github.com/jflavan/pulumi-osano/tree/main/examples): a [Unified Consent quickstart](https://github.com/jflavan/pulumi-osano/tree/main/examples/quickstart/typescript) and a [Cookie Consent program](https://github.com/jflavan/pulumi-osano/tree/main/examples/cookie-consent/typescript)
- [End-to-end workflow guide](https://github.com/jflavan/pulumi-osano/blob/main/docs/end-to-end-workflow.md): install, deploy, day-2 changes, import, and teardown
- [Troubleshooting](https://github.com/jflavan/pulumi-osano/blob/main/docs/troubleshooting.md) and [upgrade notes](https://github.com/jflavan/pulumi-osano/blob/main/docs/UPGRADE.md)
- [Changelog](https://github.com/jflavan/pulumi-osano/blob/main/CHANGELOG.md)
- Osano's [Consent JavaScript API](https://developers.osano.com/cmp/javascript-api/developer-documentation-consent-javascript-api) and Customer REST API [`publishConfig` operation](https://developers.osano.com/customer-rest-api#tag/cmp/operation/publishConfig)
