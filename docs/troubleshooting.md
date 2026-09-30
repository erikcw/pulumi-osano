# Troubleshooting

## Missing Unified Consent API key

`Unified Consent API key not configured` means a Unified Consent route needs
`osano:unifiedConsentApiKey` or `OSANO_UC_API_KEY`:

```bash
pulumi config set osano:unifiedConsentApiKey --secret
```

## Missing Customer REST API key

`Osano API key not configured` means a Cookie Consent configuration, rule, or
publication, or one of the `getCookieConsent*` functions, needs the Osano API
key. Set encrypted config or the environment variable:

```bash
pulumi config set osano:osanoApiKey --secret
# or
export OSANO_API_KEY="replace-with-a-customer-rest-api-key"
```

When both are present, the stack configuration is used, and the provider warns
that `OSANO_API_KEY` is set but ignored. Confirm the key belongs to the same
customer/environment as the target config. See the
[Customer REST API](https://developers.osano.com/customer-rest-api).

`sendSubjectCode` and `verifySubjectCode` send every configured key, because
Osano's guide and its OpenAPI spec name different keys for these routes; either
key is enough. With neither set they fail with `no Osano API key configured; set osano:osanoApiKey or
OSANO_API_KEY (or osano:unifiedConsentApiKey or OSANO_UC_API_KEY)`.

## Provider configuration rejected

The provider validates its configuration when Pulumi configures it, before any
request, so these errors appear at the start of `pulumi preview` or `pulumi up`:

- `invalid apiBaseUrl "...": must be an absolute http(s) URL` (or
  `customerBaseUrl`): the value, or its environment variable
  (`OSANO_API_BASE_URL`, `OSANO_CUSTOMER_BASE_URL`), is not a URL with a scheme
  and host.
- `API keys are only sent over https (http is accepted for loopback hosts)`:
  the base URL uses plain `http` for a host other than `localhost`,
  `127.0.0.1`, or `::1`. Use an `https` endpoint, or a loopback tunnel for a
  local proxy.
- `requestTimeoutSeconds must be at most 3600`: lower the value. It applies to
  each request attempt. An `OSANO_API_TIMEOUT_SECONDS` value that is not a
  whole number from 1 to 3600 does not fail; it is ignored with a warning and
  the default of 60 seconds is used.
- A warning such as `OSANO_API_KEY is set but osano:osanoApiKey is configured;
  the stack configuration is used` means the stack and the environment disagree.
  Remove one of them so the intent is clear.

## Provider plugin not found or not downloaded

Each published SDK names its provider plugin version and download location
(`github://api.github.com/jflavan/pulumi-osano`), and Pulumi downloads the
matching `pulumi-resource-osano` archive from the
[GitHub release](https://github.com/jflavan/pulumi-osano/releases) on the first
`pulumi preview` or `pulumi up`.

- **From a repository clone**: the committed SDKs, including the Go SDK used
  through a `replace` directive, request the development version
  `0.1.0-alpha.0+dev`, which has no GitHub release, so the download fails with
  `404 HTTP error fetching plugin`. Build and install the local plugin as
  described in
  [End-to-End Workflow, section 1](end-to-end-workflow.md#1-choose-keys-and-install).
- **GitHub rate limit**: plugin downloads use the GitHub API. If Pulumi reports
  `GitHub rate limit exceeded`, set `GITHUB_TOKEN` to a GitHub token and run the
  command again.
- **No network access at deploy time**: install the plugin in advance, at the
  same version as the SDK:

  ```bash
  pulumi plugin install resource osano 0.3.0 --server github://api.github.com/jflavan/pulumi-osano
  ```

To verify a downloaded archive's checksum and build provenance, see
[PUBLISHING.md](PUBLISHING.md).

## Provider changes after upgrading the SDK

After you upgrade the SDK, the first `pulumi preview` shows a provider change
and no change to the Osano resources:

- **Default providers** are named after their version, so the preview creates
  the new one (for example `default_0_2_0`) and deletes the old one
  (`default_0_1_0`).
- **An explicit provider resource** (`new osano.Provider(...)`) shows as an
  in-place update of `version`, so state records the plugin version in use.

Neither replaces a resource. Before `0.2.0`, an explicit provider showed as
unchanged after an upgrade and state kept the old version, which state-driven
operations such as `pulumi destroy` then request; the next `pulumi up` with
`0.2.0` or later records the new version.

## Configuration check failures and warnings

`CookieConsentConfig` checks its `configuration` object against Osano's
published Customer REST API spec during `pulumi preview` and `pulumi up`, so a
value Osano would reject with `400` fails before any request is sent. Each
failure names the property, for example
`configuration.tattleSampling must be a number from 0 to 1`.

| Key | Must be |
| --- | --- |
| `storagePolicyHref` | Present, a non-empty string (the privacy policy URL) |
| Boolean flags such as `googleConsent`, `crossDomain`, `showWidget` | `true` or `false`, not strings |
| `tattleSampling` | A number from 0 to 1 |
| `timeoutSeconds` | A whole number |
| `iframeBlocking`, `localStorageBlocking` | `""`, `debug`, `permissive`, or `production` |
| `doNotSellCategories` | A list of `MARKETING`, `ANALYTICS`, `PERSONALIZATION`; at least one when `enableDoNotSell` is `true` |
| `additionalLinks` | One or two `[text, url]` pairs; `text` is a documented link ID (for example `privacyPolicy` or `termsOfService`) and differs from `policyLinkText`. For no additional links, leave the key out rather than setting `[]` |
| `variantMapping` | `{}`, or `{ byJurisdiction: { "us" or "us-xx": "one" or "three" }, behavior: "fallbackToOsano" }` |
| `palette.dialogType` | `bar` or `box` (every palette value may also be `null`, which removes it) |
| `palette.widgetPosition`, `infoDialogPosition`, `optOutWidgetPosition` | `right` or `left` |
| `palette.theme` | `classic` or `modern` |
| `palette.displayPosition` | `top` or `bottom` when `dialogType` is `bar`; `top-left`, `top-right`, `bottom-left`, `bottom-right`, or `center` when it is `box`; any of these when the program does not set `dialogType` |
| `translations` | An object |

If the configuration is unchanged since the last `pulumi up`, these problems are
reported as warnings instead, because Osano accepted the configuration when it
was applied; they fail again once you change the configuration.

Warnings do not stop the deployment. They appear in the preview output for:

- a configuration or palette key that is not in Osano's published spec. Osano
  rejects configuration keys it does not know, so check the spelling unless
  Osano added the key recently;
- a deprecated palette key: use `toggleOffThumbColor` for
  `toggleButtonOffColor`, `toggleOnThumbColor` for `toggleButtonOnColor`,
  `toggleOffTrackColor` for `toggleOffBackgroundColor`, and
  `toggleOnTrackColor` for `toggleOnBackgroundColor`;
- a `policyLinkText` value Osano does not document;
- `ccpaRelaxed` declared next to a non-empty `variantMapping`. Osano overwrites
  `ccpaRelaxed` to mirror the mapping, so a value that disagrees shows as drift
  on refresh; remove it and express the US banner formats in `variantMapping`;
- `googleConsent` declared `true`, or left to Osano's default on a new
  configuration, while `mode` is `debug`. Osano then signals denied Google
  Consent Mode consent for every visitor; set `googleConsent` to `false` until
  the configuration moves to `permissive` or `production`.

## Drift or an update on every refresh

`pulumi refresh` and `pulumi up --refresh` compare only the configuration keys
the program declares, recursively into nested objects such as `palette` and
`translations`, so defaults Osano adds never produce a diff. If a refreshed run
still updates the configuration every time:

- **Provider `0.1.0`** compared nested objects whole, so declaring part of
  `palette` reported drift and PATCHed the configuration on every refreshed run,
  which also left it `outdated` in Osano. Upgrade to `0.2.0`; one refresh brings
  state in line. The configuration can stay `outdated` until the next deliberate
  publication (change the publication's `changeToken`).
- **`variantMapping`** is compared as a whole. Jurisdictions added in the Osano
  dashboard show as drift; add them to the program or remove them in Osano.
- **`ccpaRelaxed` with a `variantMapping`**: Osano rewrites `ccpaRelaxed`, as
  described in the warning above. Remove it from the program.
- **Dashboard edits**: find them with `getCookieConsentAuditLog`, filtered on
  the configuration and event types such as `cmp.configUpdated`, then
  reconcile the program.

## Publication status and HTTP responses

Osano reports `unpublished`, `in-progress`, `published`, `outdated`, or `error`.
`CookieConsentPublication` waits for a newly accepted operation to reach
`published`; it does not accept stale pre-publish metadata as completion.

### Publication `409 Conflict`

A publish POST can return `409` when the configuration already has a publication
in progress. The provider treats that operation as accepted elsewhere, does not
send another immediate POST, and joins the polling path. Let the current
`pulumi up` continue. If it later times out, check Osano before retrying so the
accepted operation has time to finish.

### Publication `429 Too Many Requests`

A publish POST can return `429` because of rate or publication-capacity limits.
Osano queues at most 300 configurations per account and publishes in batches of
up to 250 per 30 minutes. The provider honors `Retry-After` when supplied and
otherwise uses bounded backoff. If the bounded retries are exhausted, wait for
the indicated window or for capacity to recover before running a single new
`pulumi up`. Do not run repeated immediate publish attempts; they extend
throttling and can compete with an already accepted operation.

This publication-specific guidance differs from batching ordinary consent
submissions. Avoid concurrent publication resources for the same config.

### Terminal publication `error`

`error` is an Osano terminal status, so the provider stops instead of continuing
to poll. Open the configuration in Osano, inspect its publication/configuration
validation details, and correct the configuration or rules. Then change the
publish-relevant desired state (and therefore `changeToken`) and run one new
`pulumi up`. The terminal diagnostic contains `status`, `lastPublished`, and
`publishedRevision` from the current response (the metadata values are zero when
Osano does not provide them). It contains neither the config ID nor the API key.

If the configuration was already in `error` before the publish request, Osano
keeps reporting that same `error` until it starts the new operation, which can
wait behind its publication queue. The provider keeps polling until the
publication timeout and warns once, after about 25 seconds, that it is
`waiting for Osano to start the new publication`. If the timeout is reached,
check the configuration in Osano before running `pulumi up` again.

### Publication timeout or cancellation

The provider waits for the resource's Pulumi create/update `customTimeouts` and
stops after twenty minutes when none is set. Set twenty minutes explicitly so
the intent is visible in the program, as the canonical C# example does:

```csharp
var publicationOptions = new CustomResourceOptions
{
    CustomTimeouts = new CustomTimeouts
    {
        Create = TimeSpan.FromMinutes(20),
        Update = TimeSpan.FromMinutes(20),
    },
};
```

See Pulumi [`customTimeouts`](https://www.pulumi.com/docs/iac/concepts/resources/options/customtimeouts/).
`requestTimeoutSeconds` and `OSANO_API_TIMEOUT_SECONDS` govern individual HTTP
calls, not the whole asynchronous publication wait.

Cancellation and timeout stop provider polling promptly, but an operation
already accepted by Osano may continue. Check its status before retrying. A
later `pulumi up` can safely join an in-progress operation through `409`; do not
launch repeated immediate attempts.

### Persistent `outdated` status

Refresh is read-only: `pulumi refresh` can expose `outdated` but never publishes.
Confirm the program models all intended configuration and rule values, and that
its deterministic `changeToken` includes every publish-relevant desired value.
Because [`dependsOn`](https://www.pulumi.com/docs/iac/concepts/resources/options/dependson/)
controls ordering rather than update triggering, changing rules without changing
the token cannot republish. After resolving external drift, deliberately change
the token (for example through a stable desired publication revision) and apply
once. Do not continually vary the token to force immediate retries.

### Delayed CDN propagation after `published`

The provider completes when the Customer REST API proves the accepted operation
is `published`. What visitors load follows later:

- Osano's CDN can take up to 15 minutes after that point to serve the newest
  script revision at every edge.
- Browsers cache `osano.js` for 24 hours, so a returning visitor can see the
  previous revision for up to a day. Test a change in a private window or with
  the browser cache disabled.

The script URL is the same for every revision, so the site does not need a new
tag after a republish. Keep the exact returned tag first in the site `<head>`
with no `async` or `defer`, and allow the propagation window before diagnosing
the script as stale. To confirm that a publication happened, query
`getCookieConsentAuditLog` for `cmp.configPublished` events on the
configuration. See Osano's
[direct Customer REST API `publishConfig` operation](https://developers.osano.com/customer-rest-api#tag/cmp/operation/publishConfig)
and [Consent JavaScript API](https://developers.osano.com/cmp/javascript-api/developer-documentation-consent-javascript-api).

## Script URL returns `403`

A configuration's script URL returns `403` until its first publication. Add a
`CookieConsentPublication` for it, or publish it in Osano, and wait for the
publication to complete. When another stack consumes the tag through
`getCookieConsentConfig`, `lastPublished` is `0` for a configuration that has
never been published; check it before handing the tag to a site (see
[End-to-End Workflow, section 3](end-to-end-workflow.md#from-another-stack)).

## Content Security Policy blocks Osano

If the browser console reports Content Security Policy violations for
`osano.com` hosts, add `https://*.osano.com` to `script-src`, `style-src`,
`connect-src`, `frame-src`, and `worker-src`, plus `'unsafe-inline'` in
`style-src` and `blob:` in `worker-src`.
[End-to-End Workflow, section 8](end-to-end-workflow.md#8-content-security-policy)
lists the individual hosts. Nonces work; Subresource Integrity and hash sources
do not, because `osano.js` changes with every publication, every Osano CMP
release, and by visitor location.

## Cookie Consent create failed with a server error

Config and rule creates are not retried after a `5xx` response or a lost
response, because Osano may already have created the resource before the error
was returned. Osano has no delete endpoint for configurations, so a blind retry
could leave a permanent duplicate. `429` is still retried, because it means the
request was not processed.

For a `CookieConsentConfig`, the provider then lists the configurations with
the requested name. If exactly one has that name and those domains and was
created at or after the request, the provider adopts it into state, warns, and
`pulumi up` continues. When none or several match, the create fails with the
original error (`osano api error: status=503 ...`).

Before re-running `pulumi up`, check Osano for a configuration or rule matching
your inputs (`getCookieConsentConfigs` and `getCookieConsentRules` list them).
If one exists, import it instead of creating another:

```bash
pulumi import osano:index:CookieConsentConfig consentConfig <configId>
pulumi import osano:index:CookieConsentRule analyticsRule <configId>/<ruleId>
```

## Unified Consent errors

### `referenceType must be subject or session`

`getUnifiedConsent` and `getSubject` accept `referenceType` `subject` (the
default) for a verified or anonymous ID, and `session` for a session ID. These
are the only values Osano's `ref` parameter accepts. `anonymous` is still
accepted as a deprecated alias of `subject`; any other value fails with this
error. Remove `referenceType` or set it to `subject` or `session`.

### Lookups return `exists: false`

Osano answers a lookup for a subject with no consent with `400`, which
`getUnifiedConsent` reports as `exists: false`. Check that `subjectRef` is the
ID Osano knows and that `referenceType` matches it: `subject` for verified and
anonymous IDs, `session` for a session ID.

Provider `0.1.0` sent `referenceType: anonymous` to Osano unchanged, which Osano
rejected, so those lookups returned `exists: false` and a refresh removed
`Consent` resources with an `anonymousId` from state. Upgrade to `0.2.0`.

### `Consent` validation failures

`pulumi preview` fails a `Consent` resource whose `compliance.gpc` is not `0` or
`1`, whose subject ID contains `#`, `%`, or a space, or whose
`countryCodeOverride` or `regionCodeOverride` is not an ISO 3166 code such as
`US` or `US-CA`. For new or changed values it also fails an
`actions[].action` other than `ACCEPT`, `REJECT`, or `UNSELECTED`
(case-sensitive) and an `origin` other than `api` or `gpc`. `actions` is
required unless `origin` is `gpc`.

### `osano:index:Consent cannot be imported`

`pulumi import osano:index:Consent` fails with this error. Osano exposes only the
merged consent of a subject, not the individual submissions a `Consent`
resource represents, so there is nothing to import. Submit a new consent with
the resource instead; see [IMPORTING.md](IMPORTING.md#unified-consent-records).

### `Osano reports no consent for the subject`

This warning appears during `pulumi refresh` (or `pulumi up --refresh`) when
Osano no longer reports consent for the subject of a `Consent` resource. The
resource is kept, so nothing is submitted again. Check the subject with
`getUnifiedConsent`. To submit a new consent, change an input, which replaces
the resource; to stop tracking it, remove it from the program.

### `session is required to verify an SMS code`

Osano requires the SMS challenge session to verify a code sent by SMS. Pass the
`session` output of `sendSubjectCode` (returned when Osano's response includes
one) as the `session` input of `verifySubjectCode`. Email verification does not
use it.

## Other API errors

### `400 Bad Request`

Check the Pulumi diagnostic for the operation and Osano validation response.
Fix invalid IDs, configuration values, or missing fields, then rerun preview.
Many invalid `CookieConsentConfig` and `Consent` values fail at preview
instead; see
[configuration check failures and warnings](#configuration-check-failures-and-warnings)
and [`Consent` validation failures](#consent-validation-failures).

### `401 Unauthorized`

Verify the route uses the correct key type, the key has not expired, and it
matches the production/sandbox environment. Rotate it with encrypted Pulumi
config and apply once.

### `5xx` responses and dropped connections

An error is reported as `osano api error: status=<code> body=<body>` (the body
is cut at 2 KiB), or as `Osano API request failed: GET https://api.osano.com:
...` when no response arrived. Reads, including every `get*` function and
`pulumi refresh`, are retried up to three times after a dropped connection or
a `429` or `5xx` response; updates and deletes after `429` or `5xx`; creates
and the subject verification functions only after `429` (see
[create failed with a server error](#cookie-consent-create-failed-with-a-server-error)).
A failure that survives the retries usually means an Osano incident; wait and
run `pulumi up` again.

## Debug strategy

1. Re-run with `--logtostderr --logflow -v=9 2> pulumi-debug.log` and redact
   credentials and PII (see [logging](logging.md)).
2. Use `pulumi refresh` to inspect remote status without mutation.
3. For Cookie Consent, use `getCookieConsentConfig` for the current status and
   `getCookieConsentAuditLog` for recent changes and publications. For Unified
   Consent, use `getUnifiedConsent` to inspect the subject's latest state.
4. If the error persists, open an issue with sanitized logs, the status code,
   the relevant resource ID, and the provider version (`pulumi plugin ls` lists
   the installed `osano` plugin versions).

Never share API keys or real subject identifiers in public threads.
