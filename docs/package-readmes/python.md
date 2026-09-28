# Osano Pulumi Provider for Python

[![Build Status](https://img.shields.io/github/actions/workflow/status/jflavan/pulumi-osano/build.yml?branch=main)](https://github.com/jflavan/pulumi-osano/actions/workflows/build.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/jflavan/pulumi-osano/blob/main/LICENSE)
[![PyPI version](https://img.shields.io/pypi/v/pulumi-osano?label=PyPI&logo=pypi&logoColor=white)](https://pypi.org/project/pulumi-osano/)

> **⚠️ Unofficial community provider**
>
> This package is not affiliated with Osano or Pulumi. It is maintained by the community and provided "as is" under the MIT license. Use [GitHub Issues](https://github.com/jflavan/pulumi-osano/issues) for support.

`pulumi-osano` is the Python SDK for the Osano Pulumi provider, imported as `pulumi_osano`. It lets you manage Osano Cookie Consent and Unified Consent workflows alongside the rest of your infrastructure-as-code. You can:

- Create Cookie Consent configurations and rules, publish them after all dependencies settle, and export the hosted CMP script URL and exact HTML tag, so the same pipeline that provisions a website can put the consent script first in its `<head>`.
- Look up the script, publish status, rules, discoveries, and audit log of any Cookie Consent configuration with `get_cookie_consent_config`, `get_cookie_consent_configs`, `get_cookie_consent_rules`, `get_cookie_consent_discoveries`, and `get_cookie_consent_audit_log`, for example to consume a centrally managed configuration from a website stack or to gate a switch to production mode.
- Submit consent decisions programmatically from Pulumi deployments, including Global Privacy Control consents.
- Query unified consent state for a subject with `get_unified_consent`, and resolve verified, anonymous, and session references with `get_subject`, `get_subject_profile`, and `get_session`.
- Inspect UC configuration and privacy protocol collections with `get_config`, `get_collections`, and `get_collection`, and check for existing consent state and hashed consent profiles with `check_consent` and `get_consent_profile`.
- Start and verify subject-profile challenges with `send_subject_code` and `verify_subject_code`. Pulumi runs functions on every preview, update, and refresh, so call these two from automation rather than declaring them in a long-lived stack; otherwise each run sends a new code.

Every function also has an `_output` form, such as `get_cookie_consent_config_output`, that accepts and returns Pulumi outputs. SDKs for Node.js, Go, .NET, and Java are also available; see the [project README](https://github.com/jflavan/pulumi-osano#readme).

## Prerequisites

- Pulumi CLI v3+
- Python 3.10+
- API access to an Osano tenant: a Customer REST API key for Cookie Consent, a Unified Consent API key for Unified Consent, or both for mixed workloads

## Installation

```bash
pip install pulumi-osano
```

In a project created with `pulumi new python`, add `pulumi-osano` to `requirements.txt` and run `pulumi install` instead.

The package declares its provider plugin, and Pulumi downloads the matching `pulumi-resource-osano` release from GitHub the first time you run `pulumi preview` or `pulumi up`. To install it manually, pin the version and point Pulumi at the GitHub releases:

```bash
pulumi plugin install resource osano 0.3.0 --server github://api.github.com/jflavan/pulumi-osano
```

[Package publishing](https://github.com/jflavan/pulumi-osano/blob/main/docs/PUBLISHING.md) describes how each release is built and how to verify its provenance.

## Quick start: publish a consent script

This program creates a Cookie Consent configuration with one rule, publishes it, and hands the script tag to the rest of the program. It assumes a project created with `pulumi new python`:

```bash
echo "pulumi-osano" >> requirements.txt
pulumi install
pulumi config set osano:osanoApiKey --secret   # Customer REST API key
pulumi up
```

`__main__.py`:

```python
import hashlib
import json

import pulumi
import pulumi_osano as osano

name = "www-example-com"
domains = ["www.example.com"]
mode = "permissive"
configuration = {"storagePolicyHref": "https://www.example.com/privacy"}
rule_definitions = [
    {"store_type": "cookies", "classification": "ANALYTICS", "rule": "_ga", "rule_type": "EXACT_MATCH"},
]

config = osano.CookieConsentConfig(
    "consent", name=name, domains=domains, mode=mode, configuration=configuration)
rules = [
    osano.CookieConsentRule(
        f"rule-{i}",
        config_id=config.config_id,
        store_type=rule["store_type"],
        classification=rule["classification"],
        rule=rule["rule"],
        rule_type=rule["rule_type"],
    )
    for i, rule in enumerate(rule_definitions)
]

# Publish exactly once per change: derive the token from everything that is published.
desired = {"name": name, "domains": domains, "mode": mode,
           "configuration": configuration, "rules": rule_definitions}
publication = osano.CookieConsentPublication(
    "publication",
    config_id=config.config_id,
    change_token=hashlib.sha256(json.dumps(desired, sort_keys=True).encode()).hexdigest(),
    opts=pulumi.ResourceOptions(
        depends_on=[config, *rules],
        custom_timeouts=pulumi.CustomTimeouts(create="20m", update="20m"),
    ),
)

# Hand the tag to whatever renders or configures the site's <head>; it must come first.
pulumi.export("scriptTag", publication.script_tag)
pulumi.export("headHtml", pulumi.Output.format("<head>\n  {0}\n</head>", publication.script_tag))
```

`pulumi preview` never publishes. `pulumi up` creates the configuration and rule, publishes, waits for Osano to finish, and returns `<script src="https://cmp.osano.com/{customerId}/{configId}/osano.js"></script>`. Running it again without changes publishes nothing.

The publication exports `script_src` (`https://cmp.osano.com/{customerId}/{configId}/osano.js`) and `script_tag` (exactly `<script src="{scriptSrc}"></script>`). These installation values are deliberately non-secret. Put the tag first in the site `<head>` without `async` or `defer`, so the CMP loads before scripts it may control. The URL never changes between revisions, so a website only needs it once. Publication completion and CDN propagation are separate: Osano's CDN can take up to 15 minutes to serve a new revision, and browsers cache `osano.js` for up to 24 hours.

## Read a configuration from another stack

To use the script in another stack, such as one per website, read it with `get_cookie_consent_config_output` instead of managing the configuration there:

```python
consent = osano.get_cookie_consent_config_output(config_id="<config-id>")
pulumi.export("headScript", consent.script_tag)     # the same value the publication exports
pulumi.export("published", consent.publish_status)  # the URL returns 403 until the first publish
```

Before switching a configuration to `production` mode, which blocks everything unclassified, `get_cookie_consent_discoveries` lists what osano.js has discovered that no rule covers yet. The [end-to-end workflow guide](https://github.com/jflavan/pulumi-osano/blob/main/docs/end-to-end-workflow.md) covers the whole pipeline, including Content Security Policy settings and per-environment configurations.

## Unified Consent

The `Consent` resource submits a consent decision, and the functions read consent state back. This program assumes a project created with `pulumi new python` with `pulumi-osano` in `requirements.txt`:

```bash
pulumi config set osano:unifiedConsentApiKey --secret
pulumi config set subjectRef <subject-id> --secret
pulumi config set configId <config-id>
pulumi config set privacyProtocolId <protocol-id>
pulumi up
```

`__main__.py`:

```python
import pulumi
import pulumi_osano as osano

cfg = pulumi.Config()
subject_ref = cfg.require_secret("subjectRef")
config_id = cfg.require("configId")
privacy_protocol_id = cfg.require("privacyProtocolId")
subject_type = cfg.get("subjectType") or "verified"

subject = (
    osano.ConsentSubjectArgs(anonymous_id=subject_ref)
    if subject_type == "anonymous"
    else osano.ConsentSubjectArgs(verified_id=subject_ref)
)

consent = osano.Consent(
    "example",
    subject=subject,
    actions=[
        osano.ConsentActionArgs(target=privacy_protocol_id, vendor=config_id, action="ACCEPT"),
    ],
    attributes={"pulumiStack": pulumi.get_stack()},
    origin="api",
    tags=["demo"],
)

pulumi.export("consentId", consent.consent_id)
```

Destroying the stack removes the logical Pulumi resource but does **not** delete historical events from Osano (they are immutable).

`get_subject` and `get_unified_consent` look anonymous and verified IDs up with the default `reference_type` (`subject`); `session` resolves a session ID. To submit a Global Privacy Control consent, set `origin="gpc"` and omit `actions`: Osano derives the actions and the resource exports them as `gpc_actions`. When a pipeline submits consents on a subject's behalf, set `country_code_override` (and `region_code_override`) so Osano does not geolocate the CI runner.

## Authentication

Two API keys exist:

| Key | Header | Usage |
| --- | --- | --- |
| Unified Consent API key | `x-uc-api-key` | Required for consent submissions and read operations |
| Osano Customer REST API key | `x-osano-api-key` | Required for Cookie Consent resources and functions; `send_subject_code` and `verify_subject_code` send every configured key, so either this key or the Unified Consent API key is enough |

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

- [Examples](https://github.com/jflavan/pulumi-osano/tree/main/examples), including a [Python Unified Consent quickstart](https://github.com/jflavan/pulumi-osano/tree/main/examples/quickstart/python)
- [End-to-end workflow guide](https://github.com/jflavan/pulumi-osano/blob/main/docs/end-to-end-workflow.md): install, deploy, day-2 changes, import, and teardown
- [Troubleshooting](https://github.com/jflavan/pulumi-osano/blob/main/docs/troubleshooting.md) and [upgrade notes](https://github.com/jflavan/pulumi-osano/blob/main/docs/UPGRADE.md)
- [Changelog](https://github.com/jflavan/pulumi-osano/blob/main/CHANGELOG.md)
- Osano's [Consent JavaScript API](https://developers.osano.com/cmp/javascript-api/developer-documentation-consent-javascript-api) and Customer REST API [`publishConfig` operation](https://developers.osano.com/customer-rest-api#tag/cmp/operation/publishConfig)
