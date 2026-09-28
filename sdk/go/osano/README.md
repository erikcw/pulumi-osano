# Osano Pulumi Provider for Go

[![Build Status](https://img.shields.io/github/actions/workflow/status/jflavan/pulumi-osano/build.yml?branch=main)](https://github.com/jflavan/pulumi-osano/actions/workflows/build.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/jflavan/pulumi-osano/blob/main/LICENSE)
[![Go module version](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fproxy.golang.org%2Fgithub.com%2Fjflavan%2Fpulumi-osano%2Fsdk%2Fgo%2Fosano%2F%40latest&query=%24.Version&label=Go&logo=go&logoColor=white)](https://pkg.go.dev/github.com/jflavan/pulumi-osano/sdk/go/osano)

> **⚠️ Unofficial community provider**
>
> This module is not affiliated with Osano or Pulumi. It is maintained by the community and provided "as is" under the MIT license. Use [GitHub Issues](https://github.com/jflavan/pulumi-osano/issues) for support.

`github.com/jflavan/pulumi-osano/sdk/go/osano` is the Go SDK for the Osano Pulumi provider. It lets you manage Osano Cookie Consent and Unified Consent workflows alongside the rest of your infrastructure-as-code. You can:

- Create Cookie Consent configurations and rules, publish them after all dependencies settle, and export the hosted CMP script URL and exact HTML tag, so the same pipeline that provisions a website can put the consent script first in its `<head>`.
- Look up the script, publish status, rules, discoveries, and audit log of any Cookie Consent configuration with `LookupCookieConsentConfig`, `GetCookieConsentConfigs`, `GetCookieConsentRules`, `GetCookieConsentDiscoveries`, and `GetCookieConsentAuditLog`, for example to consume a centrally managed configuration from a website stack or to gate a switch to production mode.
- Submit consent decisions programmatically from Pulumi deployments, including Global Privacy Control consents.
- Query unified consent state for a subject with `GetUnifiedConsent`, and resolve verified, anonymous, and session references with `GetSubject`, `GetSubjectProfile`, and `GetSession`.
- Inspect UC configuration and privacy protocol collections with `GetConfig`, `GetCollections`, and `GetCollection`, and check for existing consent state and hashed consent profiles with `CheckConsent` and `GetConsentProfile`.
- Start and verify subject-profile challenges with `SendSubjectCode` and `VerifySubjectCode`. Pulumi runs functions on every preview, update, and refresh, so call these two from automation rather than declaring them in a long-lived stack; otherwise each run sends a new code.

Every function also has an `Output` form, such as `LookupCookieConsentConfigOutput`, that accepts and returns Pulumi outputs. SDKs for Node.js, Python, .NET, and Java are also available; see the [project README](https://github.com/jflavan/pulumi-osano#readme).

## Prerequisites

- Pulumi CLI v3+
- Go 1.26.6+
- API access to an Osano tenant: a Customer REST API key for Cookie Consent, a Unified Consent API key for Unified Consent, or both for mixed workloads

## Installation

```bash
go get github.com/jflavan/pulumi-osano/sdk/go/osano
```

The module requires `github.com/pulumi/pulumi/sdk/v3` v3.264.0 or later, so `go get` may raise your requirement to it.

The module declares its provider plugin, and Pulumi downloads the matching `pulumi-resource-osano` release from GitHub the first time you run `pulumi preview` or `pulumi up`. To install it manually, pin the version and point Pulumi at the GitHub releases:

```bash
pulumi plugin install resource osano 0.2.1 --server github://api.github.com/jflavan/pulumi-osano
```

[Package publishing](https://github.com/jflavan/pulumi-osano/blob/main/docs/PUBLISHING.md) describes how each release is built and how to verify its provenance.

## Quick start: publish a consent script

This program creates a Cookie Consent configuration with one rule, publishes it, and hands the script tag to the rest of the program. It assumes a project created with `pulumi new go`:

```bash
go get github.com/jflavan/pulumi-osano/sdk/go/osano
pulumi config set osano:osanoApiKey --secret   # Customer REST API key
pulumi up
```

`main.go`:

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/jflavan/pulumi-osano/sdk/go/osano"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type rule struct {
	StoreType      string `json:"storeType"`
	Classification string `json:"classification"`
	Rule           string `json:"rule"`
	RuleType       string `json:"ruleType"`
}

type desiredConfig struct {
	Name          string                 `json:"name"`
	Domains       []string               `json:"domains"`
	Mode          string                 `json:"mode"`
	Configuration map[string]interface{} `json:"configuration"`
	Rules         []rule                 `json:"rules"`
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		desired := desiredConfig{
			Name:          "www-example-com",
			Domains:       []string{"www.example.com"},
			Mode:          "permissive",
			Configuration: map[string]interface{}{"storagePolicyHref": "https://www.example.com/privacy"},
			Rules:         []rule{{StoreType: "cookies", Classification: "ANALYTICS", Rule: "_ga", RuleType: "EXACT_MATCH"}},
		}

		config, err := osano.NewCookieConsentConfig(ctx, "consent", &osano.CookieConsentConfigArgs{
			Name:          pulumi.String(desired.Name),
			Domains:       pulumi.ToStringArray(desired.Domains),
			Mode:          pulumi.String(desired.Mode),
			Configuration: pulumi.ToMap(desired.Configuration),
		})
		if err != nil {
			return err
		}
		dependsOn := []pulumi.Resource{config}
		for i, r := range desired.Rules {
			created, err := osano.NewCookieConsentRule(ctx, fmt.Sprintf("rule-%d", i), &osano.CookieConsentRuleArgs{
				ConfigId:       config.ConfigId,
				StoreType:      pulumi.String(r.StoreType),
				Classification: pulumi.String(r.Classification),
				Rule:           pulumi.String(r.Rule),
				RuleType:       pulumi.String(r.RuleType),
			})
			if err != nil {
				return err
			}
			dependsOn = append(dependsOn, created)
		}

		// Publish exactly once per change: derive the token from everything that is published.
		descriptor, err := json.Marshal(desired)
		if err != nil {
			return err
		}
		token := sha256.Sum256(descriptor)
		publication, err := osano.NewCookieConsentPublication(ctx, "publication", &osano.CookieConsentPublicationArgs{
			ConfigId:    config.ConfigId,
			ChangeToken: pulumi.String(hex.EncodeToString(token[:])),
		}, pulumi.DependsOn(dependsOn), pulumi.Timeouts(&pulumi.CustomTimeouts{Create: "20m", Update: "20m"}))
		if err != nil {
			return err
		}

		// Hand the tag to whatever renders or configures the site's <head>; it must come first.
		ctx.Export("scriptTag", publication.ScriptTag)
		ctx.Export("headHtml", pulumi.Sprintf("<head>\n  %s\n</head>", publication.ScriptTag))
		return nil
	})
}
```

`pulumi preview` never publishes. `pulumi up` creates the configuration and rule, publishes, waits for Osano to finish, and returns `<script src="https://cmp.osano.com/{customerId}/{configId}/osano.js"></script>`. Running it again without changes publishes nothing.

The publication exports `ScriptSrc` (`https://cmp.osano.com/{customerId}/{configId}/osano.js`) and `ScriptTag` (exactly `<script src="{scriptSrc}"></script>`). These installation values are deliberately non-secret. Put the tag first in the site `<head>` without `async` or `defer`, so the CMP loads before scripts it may control. The URL never changes between revisions, so a website only needs it once. Publication completion and CDN propagation are separate: Osano's CDN can take up to 15 minutes to serve a new revision, and browsers cache `osano.js` for up to 24 hours.

## Read a configuration from another stack

To use the script in another stack, such as one per website, read it with `LookupCookieConsentConfigOutput` instead of managing the configuration there:

```go
consent := osano.LookupCookieConsentConfigOutput(ctx, osano.LookupCookieConsentConfigOutputArgs{
	ConfigId: pulumi.String("<config-id>"),
})
ctx.Export("headScript", consent.ScriptTag())    // the same value the publication exports
ctx.Export("published", consent.PublishStatus()) // the URL returns 403 until the first publish
```

Before switching a configuration to `production` mode, which blocks everything unclassified, `GetCookieConsentDiscoveries` lists what osano.js has discovered that no rule covers yet. The [end-to-end workflow guide](https://github.com/jflavan/pulumi-osano/blob/main/docs/end-to-end-workflow.md) covers the whole pipeline, including Content Security Policy settings and per-environment configurations.

## Unified Consent

The `Consent` resource submits a consent decision, and the functions read consent state back. This program assumes a project created with `pulumi new go`:

```bash
go get github.com/jflavan/pulumi-osano/sdk/go/osano
pulumi config set osano:unifiedConsentApiKey --secret
pulumi config set subjectRef <subject-id> --secret
pulumi config set configId <config-id>
pulumi config set privacyProtocolId <protocol-id>
pulumi up
```

`main.go`:

```go
package main

import (
	"github.com/jflavan/pulumi-osano/sdk/go/osano"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		subjectRef := cfg.RequireSecret("subjectRef")
		configID := cfg.Require("configId")
		privacyProtocolID := cfg.Require("privacyProtocolId")

		subject := &osano.ConsentSubjectArgs{VerifiedId: subjectRef}
		if cfg.Get("subjectType") == "anonymous" {
			subject = &osano.ConsentSubjectArgs{AnonymousId: subjectRef}
		}

		consent, err := osano.NewConsent(ctx, "example", &osano.ConsentArgs{
			Subject: subject,
			Actions: osano.ConsentActionArray{
				&osano.ConsentActionArgs{
					Target: pulumi.String(privacyProtocolID),
					Vendor: pulumi.String(configID),
					Action: pulumi.String("ACCEPT"),
				},
			},
			Attributes: pulumi.StringMap{"pulumiStack": pulumi.String(ctx.Stack())},
			Origin:     pulumi.String("api"),
			Tags:       pulumi.StringArray{pulumi.String("demo")},
		})
		if err != nil {
			return err
		}

		ctx.Export("consentId", consent.ConsentId)
		return nil
	})
}
```

Destroying the stack removes the logical Pulumi resource but does **not** delete historical events from Osano (they are immutable).

`GetSubject` and `GetUnifiedConsent` look anonymous and verified IDs up with the default `ReferenceType` (`subject`); `session` resolves a session ID. To submit a Global Privacy Control consent, set `Origin: pulumi.String("gpc")` and omit `Actions`: Osano derives the actions and the resource exports them as `GpcActions`. When a pipeline submits consents on a subject's behalf, set `CountryCodeOverride` (and `RegionCodeOverride`) so Osano does not geolocate the CI runner.

## Authentication

Two API keys exist:

| Key | Header | Usage |
| --- | --- | --- |
| Unified Consent API key | `x-uc-api-key` | Required for consent submissions and read operations |
| Osano Customer REST API key | `x-osano-api-key` | Required for Cookie Consent resources and functions; `SendSubjectCode` and `VerifySubjectCode` send every configured key, so either this key or the Unified Consent API key is enough |

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

- [Examples](https://github.com/jflavan/pulumi-osano/tree/main/examples), including a [Go Unified Consent quickstart](https://github.com/jflavan/pulumi-osano/tree/main/examples/quickstart/go)
- [End-to-end workflow guide](https://github.com/jflavan/pulumi-osano/blob/main/docs/end-to-end-workflow.md): install, deploy, day-2 changes, import, and teardown
- [Troubleshooting](https://github.com/jflavan/pulumi-osano/blob/main/docs/troubleshooting.md) and [upgrade notes](https://github.com/jflavan/pulumi-osano/blob/main/docs/UPGRADE.md)
- [Changelog](https://github.com/jflavan/pulumi-osano/blob/main/CHANGELOG.md)
- Osano's [Consent JavaScript API](https://developers.osano.com/cmp/javascript-api/developer-documentation-consent-javascript-api) and Customer REST API [`publishConfig` operation](https://developers.osano.com/customer-rest-api#tag/cmp/operation/publishConfig)
