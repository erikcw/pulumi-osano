---
title: Osano (Unofficial)
meta_desc: Use the unofficial Osano provider for Pulumi to manage Osano Cookie Consent configurations, rules, and publications and to submit and read Unified Consent records.
layout: package
---

The Osano (Unofficial) provider for Pulumi lets you manage [Osano](https://www.osano.com/) consent management as code. Create Cookie Consent (CMP) configurations and rules, publish them once they settle, and get back the exact `<script>` tag to install, so the pipeline that provisions a website can also put its consent script first in the page `<head>`. Functions read any configuration's script, publish status, rules, discoveries, and audit log. For Unified Consent, the provider submits consent decisions and reads consent, subject, configuration, and privacy protocol data.

This is a community-maintained provider. It is not affiliated with or endorsed by Osano, Inc. or Pulumi Corporation. Source code and issues are at [github.com/jflavan/pulumi-osano](https://github.com/jflavan/pulumi-osano).

## Installation

The provider is available as a package for TypeScript/JavaScript, Python, Go, .NET, and Java, and as a plugin for Pulumi YAML. Each SDK records where its provider plugin is published, so Pulumi downloads the matching plugin from the provider's GitHub releases the first time you run `pulumi preview` or `pulumi up`.

{{< chooser language "typescript,python,go,csharp,java,yaml,hcl" >}}
{{% choosable language typescript %}}

```bash
npm install @jflavan/pulumi-osano
```

{{% /choosable %}}
{{% choosable language python %}}

```bash
pip install pulumi-osano
```

{{% /choosable %}}
{{% choosable language go %}}

```bash
go get github.com/jflavan/pulumi-osano/sdk/go/osano
```

{{% /choosable %}}
{{% choosable language csharp %}}

```bash
dotnet add package Community.Pulumi.Osano
```

{{% /choosable %}}
{{% choosable language java %}}

Replace `VERSION` with the [latest release](https://github.com/jflavan/pulumi-osano/releases) (for example `0.2.1`).

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

{{% /choosable %}}
{{% choosable language yaml %}}

Pulumi YAML programs use the provider plugin directly. Install the release you want to use (for example `0.2.1`; see [releases](https://github.com/jflavan/pulumi-osano/releases)), then reference the resource types (for example `osano:index:CookieConsentConfig`) in your program:

```bash
pulumi plugin install resource osano VERSION --server github://api.github.com/jflavan/pulumi-osano
```

{{% /choosable %}}
{{% choosable language hcl %}}

This provider has not been tested with Pulumi HCL programs. Use one of the SDK languages or Pulumi YAML.

{{% /choosable %}}
{{< /chooser >}}

The [packages and publishing guide](https://github.com/jflavan/pulumi-osano/blob/main/docs/PUBLISHING.md) links each package's registry page and explains how to verify release checksums, signatures, and build provenance.

## Example Usage

The program below creates a Cookie Consent configuration, publishes it, and exports the script tag to install first in the site `<head>`. Cookie Consent resources authenticate with an Osano Customer REST API key:

```bash
pulumi config set --secret osano:osanoApiKey <customer-rest-api-key>
```

{{< chooser language "typescript,python,go,csharp,java" >}}
{{% choosable language typescript %}}

```typescript
import * as osano from "@jflavan/pulumi-osano";

const consentConfig = new osano.CookieConsentConfig("cookie-consent", {
    name: "example-cookie-consent",
    domains: ["example.com"],
    mode: "permissive",
    configuration: {
        storagePolicyHref: "https://example.com/privacy/cookies",
    },
});

const publication = new osano.CookieConsentPublication("publication", {
    configId: consentConfig.configId,
    // Change the token whenever the published configuration or rules change.
    changeToken: "v1",
}, { dependsOn: [consentConfig], customTimeouts: { create: "20m", update: "20m" } });

export const scriptTag = publication.scriptTag;
```

{{% /choosable %}}
{{% choosable language python %}}

```python
import pulumi
import pulumi_osano as osano

consent_config = osano.CookieConsentConfig(
    "cookie-consent",
    name="example-cookie-consent",
    domains=["example.com"],
    mode="permissive",
    configuration={
        "storagePolicyHref": "https://example.com/privacy/cookies",
    },
)

publication = osano.CookieConsentPublication(
    "publication",
    config_id=consent_config.config_id,
    # Change the token whenever the published configuration or rules change.
    change_token="v1",
    opts=pulumi.ResourceOptions(
        depends_on=[consent_config],
        custom_timeouts=pulumi.CustomTimeouts(create="20m", update="20m"),
    ),
)

pulumi.export("scriptTag", publication.script_tag)
```

{{% /choosable %}}
{{% choosable language go %}}

```go
package main

import (
	"github.com/jflavan/pulumi-osano/sdk/go/osano"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		consentConfig, err := osano.NewCookieConsentConfig(ctx, "cookie-consent", &osano.CookieConsentConfigArgs{
			Name:    pulumi.String("example-cookie-consent"),
			Domains: pulumi.StringArray{pulumi.String("example.com")},
			Mode:    pulumi.String("permissive"),
			Configuration: pulumi.Map{
				"storagePolicyHref": pulumi.String("https://example.com/privacy/cookies"),
			},
		})
		if err != nil {
			return err
		}
		publication, err := osano.NewCookieConsentPublication(ctx, "publication", &osano.CookieConsentPublicationArgs{
			ConfigId: consentConfig.ConfigId,
			// Change the token whenever the published configuration or rules change.
			ChangeToken: pulumi.String("v1"),
		}, pulumi.DependsOn([]pulumi.Resource{consentConfig}),
			pulumi.Timeouts(&pulumi.CustomTimeouts{Create: "20m", Update: "20m"}))
		if err != nil {
			return err
		}
		ctx.Export("scriptTag", publication.ScriptTag)
		return nil
	})
}
```

{{% /choosable %}}
{{% choosable language csharp %}}

```csharp
using System;
using System.Collections.Generic;
using Community.Pulumi.Osano;
using Pulumi;

return await Deployment.RunAsync(() =>
{
    var consentConfig = new CookieConsentConfig("cookie-consent", new()
    {
        Name = "example-cookie-consent",
        Domains = { "example.com" },
        Mode = "permissive",
        Configuration =
        {
            ["storagePolicyHref"] = "https://example.com/privacy/cookies",
        },
    });

    var publication = new CookieConsentPublication("publication", new()
    {
        ConfigId = consentConfig.ConfigId,
        // Change the token whenever the published configuration or rules change.
        ChangeToken = "v1",
    }, new CustomResourceOptions
    {
        DependsOn = { consentConfig },
        CustomTimeouts = new CustomTimeouts
        {
            Create = TimeSpan.FromMinutes(20),
            Update = TimeSpan.FromMinutes(20),
        },
    });

    return new Dictionary<string, object?>
    {
        ["scriptTag"] = publication.ScriptTag,
    };
});
```

{{% /choosable %}}
{{% choosable language java %}}

```java
package myproject;

import com.pulumi.Pulumi;
import com.pulumi.resources.CustomResourceOptions;
import com.pulumi.resources.CustomTimeouts;
import io.github.jflavan.pulumi.osano.CookieConsentConfig;
import io.github.jflavan.pulumi.osano.CookieConsentConfigArgs;
import io.github.jflavan.pulumi.osano.CookieConsentPublication;
import io.github.jflavan.pulumi.osano.CookieConsentPublicationArgs;
import java.time.Duration;
import java.util.Map;

public class App {
    public static void main(String[] args) {
        Pulumi.run(ctx -> {
            var consentConfig = new CookieConsentConfig("cookie-consent", CookieConsentConfigArgs.builder()
                .name("example-cookie-consent")
                .domains("example.com")
                .mode("permissive")
                .configuration(Map.of("storagePolicyHref", "https://example.com/privacy/cookies"))
                .build());

            var publication = new CookieConsentPublication("publication", CookieConsentPublicationArgs.builder()
                .configId(consentConfig.configId())
                // Change the token whenever the published configuration or rules change.
                .changeToken("v1")
                .build(),
                CustomResourceOptions.builder()
                    .dependsOn(consentConfig)
                    .customTimeouts(CustomTimeouts.builder()
                        .create(Duration.ofMinutes(20))
                        .update(Duration.ofMinutes(20))
                        .build())
                    .build());

            ctx.export("scriptTag", publication.scriptTag());
        });
    }
}
```

{{% /choosable %}}
{{< /chooser >}}

`pulumi up` creates the configuration, publishes it, waits for Osano to finish, and exports `<script src="https://cmp.osano.com/{customerId}/{configId}/osano.js"></script>`. Pass that output to the resource that renders or configures your site, and put it first in `<head>` without `async` or `defer`. In a real program, add `CookieConsentRule` resources to `dependsOn` and derive `changeToken` from every published value (for example a SHA-256 hash), so each change publishes exactly once and an unchanged program publishes nothing. Another stack can read the same tag with the `getCookieConsentConfig` function. The [Cookie Consent example](https://github.com/jflavan/pulumi-osano/tree/main/examples/cookie-consent) shows the complete program in C# and TypeScript, and the [end-to-end workflow guide](https://github.com/jflavan/pulumi-osano/blob/main/docs/end-to-end-workflow.md) covers the whole pipeline, day-2 changes, imports, and teardown.

## Configuration

Every setting is optional at the provider level. Each resource and function needs the API key for the Osano API it calls: Cookie Consent resources and functions need `osanoApiKey`; the `sendSubjectCode` and `verifySubjectCode` functions send every configured key and need either `osanoApiKey` or `unifiedConsentApiKey`; the `Consent` resource and the other Unified Consent functions need `unifiedConsentApiKey`. Where an environment variable is listed, it is used when the Pulumi configuration value is unset; the stack configuration takes precedence, and the provider warns when both are set and differ.

- `osanoApiKey` (Optional, Secret) — Osano Customer REST API key, sent as `x-osano-api-key`. Used by Cookie Consent resources and functions and by the `sendSubjectCode` and `verifySubjectCode` functions. May also be set with the `OSANO_API_KEY` environment variable.
- `unifiedConsentApiKey` (Optional, Secret) — Unified Consent API key, sent as `x-uc-api-key`. Used by the `Consent` resource and the Unified Consent read functions. May also be set with the `OSANO_UC_API_KEY` environment variable.
- `apiBaseUrl` (Optional) — Base URL of the Unified Consent API, including any path prefix. Defaults to `https://uc.api.osano.com`. Must use `https`, except for loopback hosts. May also be set with the `OSANO_API_BASE_URL` environment variable.
- `customerBaseUrl` (Optional) — Base URL of the Customer REST API. Defaults to `https://api.osano.com`. Must use `https`, except for loopback hosts. May also be set with the `OSANO_CUSTOMER_BASE_URL` environment variable.
- `requestTimeoutSeconds` (Optional) — Timeout, in seconds, for each HTTP request attempt to the Osano APIs, from 1 to 3600. Defaults to `60`. May also be set with the `OSANO_API_TIMEOUT_SECONDS` environment variable, which is ignored with a warning when it is not a whole number in that range.
- `ucApiKey` (Optional, Secret, Deprecated) — Former name of `unifiedConsentApiKey`, read only when `unifiedConsentApiKey` is not set. Use `unifiedConsentApiKey` instead.
- `ucBaseUrl` (Optional, Deprecated) — Former name of `apiBaseUrl`, read only when `apiBaseUrl` is not set. Use `apiBaseUrl` instead.

See [Installation & Configuration](https://www.pulumi.com/registry/packages/osano/installation-configuration/) for configuration examples.

## Resources and functions

| Resource | Purpose |
| --- | --- |
| `CookieConsentConfig` | A Cookie Consent (CMP) configuration: name, domains, mode, and configuration object. |
| `CookieConsentRule` | A cookie, script, iframe, or localStorage classification rule in a configuration. |
| `CookieConsentPublication` | Publishes a configuration when its `changeToken` changes and exports `scriptSrc` and `scriptTag`. |
| `Consent` | Submits a Unified Consent decision for a subject. The subject is a secret. |

| Function | Purpose |
| --- | --- |
| `getCookieConsentConfig` | Reads a configuration, its publish status, and its `scriptSrc` and `scriptTag`. |
| `getCookieConsentConfigs` | Lists configurations by name, domains, organization, mode, or publish status, each with its script. |
| `getCookieConsentRules` | Lists a configuration's rules, with the rule IDs used for import. |
| `getCookieConsentDiscoveries` | Lists the cookies, scripts, iframes, or localStorage keys Osano discovered for a configuration. |
| `getCookieConsentAuditLog` | Queries configuration, rule, and publication events. |
| `getUnifiedConsent`, `checkConsent`, `getConsentProfile` | Read a subject's consent state. The subject identifiers and consent records are secrets. |
| `getSubject`, `getSubjectProfile`, `getSession` | Resolve subject identifiers, profiles, and sessions. The identifiers and profile data are secrets. |
| `getConfig`, `getCollections`, `getCollection` | Read the Unified Consent configuration and privacy protocol collections. |
| `sendSubjectCode`, `verifySubjectCode` | Send and verify a subject-profile code by email or SMS. |

Pulumi runs functions on every preview, update, and refresh. That suits the read functions, but call `sendSubjectCode` and `verifySubjectCode` from automation rather than a long-lived stack; otherwise each run sends a new code.

## Lifecycle notes

- `pulumi preview`, `pulumi refresh`, and `pulumi import` never publish a Cookie Consent configuration. A `CookieConsentPublication` publishes when it is created and when an update changes its `changeToken`, `keepUnclassifiedTattles`, `description`, or `webhookUrl`. Derive `changeToken` from every publish-relevant value so that each real change publishes exactly once.
- `keepUnclassifiedTattles` defaults to `true`, so publishing does not delete unclassified discoveries. `webhookUrl` is stored as a secret, because Osano calls it without authentication.
- The script URL never changes between revisions. Osano's CDN can take up to 15 minutes to serve a new revision, browsers cache `osano.js` for up to 24 hours, and the URL returns `403` until the configuration is first published.
- `CookieConsentConfig` checks the `configuration` object against Osano's published schema during `pulumi preview`, and `pulumi refresh` compares only the configuration keys your program declares, including inside nested objects such as `palette`.
- A `CookieConsentRule` update changes only the fields the program sets or has set before, so a value set in the Osano dashboard for a field the program never set stays as it is. A `configuration` key removed from a `CookieConsentConfig` is cleared in Osano on the next update.
- Deleting a `CookieConsentRule` deletes the rule in Osano. Osano has no delete or unpublish endpoint for configurations, so deleting a `CookieConsentConfig` or `CookieConsentPublication` only removes it from Pulumi state, and the configuration and its published script stay live.
- Consent records are immutable in Osano. Destroying a `Consent` resource removes it from Pulumi state only, `pulumi refresh` never removes or resubmits one (it warns when Osano no longer reports consent for the subject), and a `Consent` cannot be imported.
- Import configurations and publications with the Osano config ID, and rules with the composite ID `<configId>/<ruleId>`. Imports only read; an imported publication republishes once when your program applies its own `changeToken`.
