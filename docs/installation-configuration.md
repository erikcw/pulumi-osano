---
title: Osano (Unofficial) Installation & Configuration
meta_desc: How to install the unofficial Osano provider for Pulumi and configure its Osano API keys, API endpoints, and request timeout.
layout: package
---

## Installation

The Osano (Unofficial) provider is available as a package in these Pulumi languages:

- JavaScript/TypeScript: [`@jflavan/pulumi-osano`](https://www.npmjs.com/package/@jflavan/pulumi-osano)

  ```bash
  npm install @jflavan/pulumi-osano
  ```

- Python: [`pulumi-osano`](https://pypi.org/project/pulumi-osano/)

  ```bash
  pip install pulumi-osano
  ```

- Go: [`github.com/jflavan/pulumi-osano/sdk/go/osano`](https://pkg.go.dev/github.com/jflavan/pulumi-osano/sdk/go/osano)

  ```bash
  go get github.com/jflavan/pulumi-osano/sdk/go/osano
  ```

- .NET: [`Community.Pulumi.Osano`](https://www.nuget.org/packages/Community.Pulumi.Osano)

  ```bash
  dotnet add package Community.Pulumi.Osano
  ```

- Java: [`io.github.jflavan.pulumi:pulumi-osano`](https://central.sonatype.com/artifact/io.github.jflavan.pulumi/pulumi-osano) on Maven Central. Replace `VERSION` with the release you want (for example `0.3.0`).

  Maven:

  ```xml
  <dependency>
      <groupId>io.github.jflavan.pulumi</groupId>
      <artifactId>pulumi-osano</artifactId>
      <version>VERSION</version>
  </dependency>
  ```

  Gradle:

  ```groovy
  implementation("io.github.jflavan.pulumi:pulumi-osano:VERSION")
  ```

Every package's registry page, how each is published, and how to verify release checksums, signatures, and build provenance are listed in [Packages and publishing](https://github.com/jflavan/pulumi-osano/blob/main/docs/PUBLISHING.md).

### Requirements

| Language | Requires |
| --- | --- |
| Node.js | Node.js 22 or later, which current `@pulumi/pulumi` releases require |
| Python | Python 3.10 or later and `pulumi>=3.231.0,<4.0.0` |
| Go | Go 1.26.6 or later and `github.com/pulumi/pulumi/sdk/v3` v3.264.0 or later |
| .NET | .NET 8 or later, tested with .NET 8 and .NET 10 (the package targets `net8.0`) |
| Java | Java 11 or later, with `com.pulumi:pulumi` as a dependency of your program |

Use Pulumi CLI 3.252 or later to import existing resources with `pulumi import`.

### Provider plugin

The SDK packages record the plugin download location (`github://api.github.com/jflavan/pulumi-osano`), so Pulumi downloads the matching `pulumi-resource-osano` plugin from the provider's [GitHub releases](https://github.com/jflavan/pulumi-osano/releases) the first time you run `pulumi preview` or `pulumi up`. Pin the SDK package to an exact version and the engine installs the plugin of the same version.

To install the plugin by hand, for example on a machine that runs Pulumi YAML programs or before going offline, run the following, replacing `VERSION` with your SDK's version (for example `0.3.0`):

```bash
pulumi plugin install resource osano VERSION --server github://api.github.com/jflavan/pulumi-osano
```

## Authentication

Osano exposes two APIs, and each uses its own key. Configure the key for the API you use, or both for programs that mix Cookie Consent and Unified Consent.

| Key | Header | Used by |
| --- | --- | --- |
| Customer REST API key (`osano:osanoApiKey`) | `x-osano-api-key` | `CookieConsentConfig`, `CookieConsentRule`, and `CookieConsentPublication`; the `getCookieConsentConfig`, `getCookieConsentConfigs`, `getCookieConsentRules`, `getCookieConsentDiscoveries`, and `getCookieConsentAuditLog` functions; and the `sendSubjectCode` and `verifySubjectCode` functions |
| Unified Consent API key (`osano:unifiedConsentApiKey`) | `x-uc-api-key` | The `Consent` resource and the `getUnifiedConsent`, `getSubject`, `getSubjectProfile`, `getSession`, `getConfig`, `getCollections`, `getCollection`, `checkConsent`, and `getConsentProfile` functions |

`sendSubjectCode` and `verifySubjectCode` send every configured key, so either key is enough.

Store keys as Pulumi secrets:

```bash
pulumi config set --secret osano:osanoApiKey <customer-rest-api-key>
pulumi config set --secret osano:unifiedConsentApiKey <unified-consent-api-key>
```

Or provide them through environment variables, for example in CI. An environment variable is used only when the stack does not configure the key; when both are set and differ, the provider uses the stack configuration and warns:

```bash
export OSANO_API_KEY="<customer-rest-api-key>"
export OSANO_UC_API_KEY="<unified-consent-api-key>"
```

A resource or function whose key is missing fails with an error naming the configuration key and environment variable to set.

## Configuration reference

All settings are optional at the provider level. A stack configuration value takes precedence over its environment variable, which is used only when the value is unset; the provider warns when both are set and differ. The provider validates its configuration when Pulumi configures it, so an invalid base URL or timeout fails before any request.

| Name | Environment variable | Secret | Default | Description |
| --- | --- | --- | --- | --- |
| `osanoApiKey` | `OSANO_API_KEY` | Yes | | Osano Customer REST API key for Cookie Consent resources and functions and the `sendSubjectCode` and `verifySubjectCode` functions. |
| `unifiedConsentApiKey` | `OSANO_UC_API_KEY` | Yes | | Unified Consent API key for the `Consent` resource and the Unified Consent functions. `sendSubjectCode` and `verifySubjectCode` also send it, so it is enough for them without `osanoApiKey`. |
| `apiBaseUrl` | `OSANO_API_BASE_URL` | No | `https://uc.api.osano.com` | Base URL of the Unified Consent API, including any path prefix. Must use `https`; `http` is accepted only for loopback hosts, as used by local mocks. |
| `customerBaseUrl` | `OSANO_CUSTOMER_BASE_URL` | No | `https://api.osano.com` | Base URL of the Customer REST API. Must use `https`; `http` is accepted only for loopback hosts. |
| `requestTimeoutSeconds` | `OSANO_API_TIMEOUT_SECONDS` | No | `60` | Timeout, in seconds, for each HTTP request attempt to the Osano APIs, from 1 to 3600. A retried request waits for each attempt separately. An environment variable value outside that range is ignored with a warning. |
| `ucApiKey` | | Yes | | Deprecated: use `unifiedConsentApiKey`. Read only when `unifiedConsentApiKey` is unset; like it, it takes precedence over `OSANO_UC_API_KEY`. |
| `ucBaseUrl` | | No | | Deprecated: use `apiBaseUrl`. Read only when `apiBaseUrl` is unset; like it, it takes precedence over `OSANO_API_BASE_URL`. |

Changing provider configuration, such as rotating an API key or adding `requestTimeoutSeconds`, updates the provider in place. It never replaces the Cookie Consent resources the provider manages.

## Configuration examples

### Cookie Consent only

```bash
pulumi config set --secret osano:osanoApiKey <customer-rest-api-key>
```

### Unified Consent only

```bash
pulumi config set --secret osano:unifiedConsentApiKey <unified-consent-api-key>
```

### Custom endpoints and a longer timeout

```bash
pulumi config set osano:apiBaseUrl https://consent.example.com/uc
pulumi config set osano:customerBaseUrl https://api.example.com
pulumi config set osano:requestTimeoutSeconds 120
```

### Environment variables only

```bash
export OSANO_API_KEY="<customer-rest-api-key>"
export OSANO_UC_API_KEY="<unified-consent-api-key>"
export OSANO_API_BASE_URL="https://uc.api.osano.com"
export OSANO_CUSTOMER_BASE_URL="https://api.osano.com"
export OSANO_API_TIMEOUT_SECONDS=120
pulumi up
```
