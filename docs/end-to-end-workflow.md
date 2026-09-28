# End-to-End Workflow

This guide walks through the complete lifecycle for both provider areas, from
installation to teardown, and the contributor loop used to change and validate
the provider. Each step links to the detailed reference page.

The core Cookie Consent use case is an infrastructure-as-code pipeline that
creates and publishes a consent configuration and hands the consent script
(`scriptTag`) to the resources that build the website, which put it first in
the page `<head>`. Sections 2 to 8 follow that pipeline.

## 1. Choose keys and install

| Workload | Key | Config / environment |
| --- | --- | --- |
| Cookie Consent resources and the `getCookieConsent*` functions | Osano Customer REST API key (`x-osano-api-key`) | `osano:osanoApiKey` (secret) or `OSANO_API_KEY` |
| `sendSubjectCode`, `verifySubjectCode` | Every configured key; either one is enough | Either of the above |
| `Consent` resource and the other Unified Consent functions | Unified Consent API key (`x-uc-api-key`) | `osano:unifiedConsentApiKey` (secret) or `OSANO_UC_API_KEY` |

Stack configuration takes precedence over the environment variables, which are
used only when the stack does not configure the key. Store config values with
`--secret`:

```bash
pulumi config set osano:osanoApiKey --secret
pulumi config set osano:unifiedConsentApiKey --secret
```

**Published packages.** Add the SDK for your language; [PUBLISHING.md](PUBLISHING.md)
lists every package with its install command, language requirements, and how to
verify it (the [README](../README.md#installation) has the short version). The
SDK declares its provider plugin, and Pulumi downloads the matching release from
GitHub on the first `pulumi preview` or `pulumi up`; no manual plugin install is
needed. Pin the SDK to an exact version (see
[Pinning pre-1.0 releases](UPGRADE.md#pinning-pre-10-releases)).

**From a clone.** The generated SDKs request the development plugin version
`0.1.0-alpha.0+dev`, which is never published, so build and install the local
provider binary before running any repo-local example:

```bash
mise exec -- make provider
mise exec -- pulumi plugin install resource osano 0.1.0-alpha.0+dev \
  --file ./bin/pulumi-resource-osano --exact --reinstall
```

Every SDK in the clone requests that version, including the Go SDK when a
program uses it through a `replace` directive, as the Go quickstart does.

## 2. Cookie Consent: first deployment

The canonical program is [examples/cookie-consent](../examples/cookie-consent)
(C#, with a TypeScript companion). It declares three kinds of resources:

1. `CookieConsentConfig`: name, domains, mode, and the CMP `configuration`
   object (which must include `storagePolicyHref`).
2. One `CookieConsentRule` per cookie, script, iframe, or localStorage pattern,
   each referencing `consentConfig.configId`.
3. One `CookieConsentPublication` that publishes the config and returns the
   install script. Give it:
   - `dependsOn` every config and rule resource, so it publishes only after
     they settle;
   - a `changeToken` derived deterministically (for example a SHA-256 hash)
     from every publish-relevant value, built from the same data the config
     and rules use;
   - `customTimeouts` of 20 minutes for create and update; the provider waits
     for that window and stops after 20 minutes when none is set.

The example projects reference the SDKs in this repository, so run them from a
clone after the setup in section 1, or follow the example README's
[released NuGet package](../examples/cookie-consent/README.md#c-with-the-released-nuget-package)
or [released npm package](../examples/cookie-consent/README.md#typescript-with-the-released-npm-package)
section to build them against the published packages.

Deploy:

```bash
export OSANO_API_KEY="replace-with-a-customer-rest-api-key"
pulumi config set domain example.com
pulumi config set storagePolicyHref https://example.com/privacy/cookies
pulumi preview   # validates the configuration; makes no changes in Osano
pulumi up        # creates the config and rules, then publishes and waits
pulumi stack output cookieConsentScriptTag
pulumi stack output headHtml
```

`pulumi preview` checks the `configuration` object against Osano's published
Customer REST API spec. A value Osano would reject with `400`, such as a
non-boolean flag or a `tattleSampling` outside 0 to 1, fails preview with the
property path; unknown or deprecated keys produce warnings. See
[configuration check failures and warnings](troubleshooting.md#configuration-check-failures-and-warnings).

## 3. Hand the script to the website

`scriptTag` is exactly `<script src="{scriptSrc}"></script>`, where `scriptSrc`
is `https://cmp.osano.com/{customerId}/{configId}/osano.js`. Both are
deliberately non-secret outputs so they can feed other resources.

### In the same program

Pass `publication.scriptTag` as an input to the resource that renders or
configures the site: a template file, a CDN edge function, or a hosting
provider's head-script setting. Both examples also export `headHtml`, a `<head>`
fragment with the tag first:

```ts
export const headHtml = pulumi.interpolate`<head>
  ${publication.scriptTag}
  <meta charset="utf-8">
</head>`;
```

A resource that takes the tag as an input depends on the publication, so Pulumi
creates or updates it only after the publication completes. The URL is the same
for every revision, so a republish does not change the tag and does not update
the downstream resource.

### From another stack

When a central stack (or the Osano dashboard) owns the configuration and each
website has its own stack, the website stack can read the tag in two ways.

A [stack reference](https://www.pulumi.com/docs/iac/concepts/stacks/#stackreferences)
reads the consent stack's outputs. It needs read access to that stack but no
Osano API key:

```ts
const consentStack = new pulumi.StackReference("acme/cookie-consent/prod");
export const scriptTag = consentStack.requireOutput("cookieConsentScriptTag");
```

`getCookieConsentConfig` reads the configuration from Osano. It needs the
Customer REST API key, works for any configuration in the account, including
one managed in the dashboard, and also returns the publication state. It
returns `exists: false` when Osano answers `404`:

```ts
const settings = new pulumi.Config();
const cmp = osano.getCookieConsentConfigOutput({ configId: settings.require("osanoConfigId") });

export const scriptTag = cmp.apply((config) => {
    if (!config.exists) {
        throw new Error("the Osano configuration does not exist");
    }
    if (config.lastPublished === 0) {
        throw new Error("the Osano configuration has never been published; its script returns 403");
    }
    return config.scriptTag;
});
```

`getCookieConsentConfigs` lists configurations by name, domain, organization,
mode, or publish status, each with its `scriptTag`, when a stack needs to find
the configuration for a domain.

### Install rules

- Put the tag first in `<head>`, before Google Tag Manager and analytics tags,
  with no `async` or `defer` attribute, so it loads before the scripts it
  controls.
- If the site uses Google Consent Mode v2, its inline gtag `consent default`
  block must run before `osano.js`, so it is the one thing that goes before
  the tag.
- `scriptTag` has no other attributes. Osano documents two optional ones the
  site can add itself: `data-osano-ui-target="<css selector>"` and
  `data-osano-load-mode="event"`, which requires the page to call
  `Osano.cm.render()`.
- Do not add `?language=` or `?variant=` to the URL. These overrides apply to
  every visitor and void the Osano Guarantee.

## 4. Publish timing and limits

`CookieConsentPublication` completes when Osano's API reports the accepted
operation as `published`. What visitors see follows later:

- Osano's CDN can take up to 15 minutes to serve the new revision.
- Browsers cache `osano.js` for 24 hours, so a returning visitor can see the
  previous revision for up to a day.
- Before a configuration's first publication, its script URL returns `403`. In
  the same program, the downstream resource waits for the publication; from
  another stack, check `lastPublished` (`0` means never published) or
  `publishStatus`, as in the example above.

Osano runs one publication per configuration at a time (a second request gets
`409`, which the provider joins), queues at most 300 configurations per
account, and publishes in batches of up to 250 per 30 minutes. A pipeline that
publishes many configurations at once can wait behind these limits, so
stagger those updates or allow longer `customTimeouts`.

## 5. Gate the switch to production mode

Production mode blocks everything that no rule classifies. Before changing a
configuration's `mode` to `production`, review what osano.js and URL scans have
discovered with `getCookieConsentDiscoveries`, add rules for what the site
needs, and only then switch. A pipeline can enforce this by failing the update
while discoveries remain. In the TypeScript example, where `mode` comes from
stack config:

```ts
const storeTypes = ["cookies", "scripts", "iframes", "localStorage"];
const unclassified = pulumi.all(storeTypes.map((storeType) =>
    osano.getCookieConsentDiscoveriesOutput({ configId: consentConfig.configId, storeType })
        .discoveries.apply((items) => items.map((item) => `${storeType}: ${item.storeKey}`)),
)).apply((lists) => lists.flat());

export const unclassifiedItems = unclassified.apply((items) => {
    if (mode === "production" && items.length > 0) {
        throw new Error(`classify these before switching to production mode: ${items.join(", ")}`);
    }
    return items;
});
```

Pulumi runs the function on every preview and update once the configuration
exists, so the gate also stops `pulumi preview`. It keeps running after the
switch, so new discoveries then fail later updates until they are classified.
`storeType` defaults to `cookies`, and each discovery reports its `scanOrigin`
(`URL Scan` or `osano.js`), the page where it was first seen, and, for cookies
only, Osano's classification `confidence`.

## 6. Confirm the publication and detect dashboard edits

`getCookieConsentAuditLog` returns Osano's audit events, newest first, with the
event type, actor, and timestamp. Filter on the configuration and
`cmp.configPublished` to confirm what the pipeline published:

```ts
const lastPublish = osano.getCookieConsentAuditLogOutput({
    configIds: [publication.configId],
    eventTypes: ["cmp.configPublished"],
    maxResults: 1,
});
export const lastPublishedAt = lastPublish.events.apply((events) => events[0]?.timestamp);
```

`publication.configId` resolves only when the publication's create or update
finishes, so the query runs after the publication completes.

Query `cmp.configUpdated`, `cmp.ruleCreated`, or `cmp.ruleUpdated` (or
`changeType`, `actor`, and a date range) to find edits made in the Osano
dashboard. `maxResults` defaults to 200 events; `0` returns every match.

`cmp.configPublished` is an audit-log event type, not a webhook event. The
publication's `webhookUrl` is called without authentication, and Osano does not
document or sign its payload, so the provider stores the URL as a secret; use
an unguessable URL and do not rely on the payload.

## 7. One configuration per environment

Every environment where `osano.js` loads, including staging, QA, and automated
browser tests, counts toward Osano traffic and billing. Give each environment
its own configuration, a copy of production with that environment's domains:
with Pulumi, one stack per environment, each with its own
`CookieConsentConfig`. Run non-production configurations in `debug` or
`permissive` mode while testing.

In `debug` mode with `googleConsent` enabled (Osano's default), Osano signals
denied Google Consent Mode consent for every visitor; the provider warns about
this combination. Set `googleConsent` to `false` in the configuration until it
moves to `permissive` or `production` mode.

## 8. Content Security Policy

If the site sends a `Content-Security-Policy`, add these sources to it:

| Directive | Add |
| --- | --- |
| `script-src` | `https://*.osano.com` |
| `style-src` | `https://*.osano.com` and `'unsafe-inline'` (as in Osano's example) |
| `connect-src` | `https://*.osano.com` |
| `frame-src` | `https://*.osano.com` |
| `worker-src` | `https://*.osano.com` and `blob:` |

To list hosts instead of the wildcard, allow at least `cmp.osano.com`,
`consent.api.osano.com`, `tattle.api.osano.com`, and
`disclosure.api.osano.com`. Nonces work: the site adds its nonce attribute to
the tag. Subresource Integrity and hash sources do not, because `osano.js`
changes with every publication, every Osano CMP release, and by visitor
location.

## 9. Cookie Consent: day-2 changes

1. Edit a rule or configuration value in the program. Because the token is
   derived from the same values, it changes too.
2. `pulumi preview` shows in-place updates for the edited config/rule and for
   the publication's `changeToken`.
3. `pulumi up` applies the edits, then republishes exactly once.
4. Running `pulumi up` again with no edits is a no-op and does not publish.

Changing `configId` or a rule's `storeType` replaces that resource. A rule
update sends only the fields the program sets or has set before, so a value set
in the Osano dashboard for a field the program never set stays as it is;
removing a field the program did set clears it. Removing a `configuration` key
from the config clears it in Osano too. Changing the publication's
`keepUnclassifiedTattles`, `description`, or `webhookUrl` also republishes. `keepUnclassifiedTattles` defaults to `true` so publication does not
delete unclassified discoveries. Changing provider configuration, such as
rotating the API key or adding `osano:requestTimeoutSeconds`, updates the
provider in place and never replaces the Cookie Consent resources. Upgrading
the SDK changes only the provider: Pulumi creates a default provider for the
new version and deletes the old one (for example `default_0_2_0` and
`default_0_1_0`), or updates an explicit provider resource's `version` in
place.

`pulumi refresh` and `pulumi up --refresh` compare only the configuration keys
the program declares, recursively into nested objects such as `palette` and
`translations`. Declaring one palette color compares that color alone, and
defaults Osano adds never produce a diff, so a refreshed run with no edits is a
no-op. `variantMapping` is compared as a whole: jurisdictions added in the
Osano dashboard show as drift.

If someone edits the configuration in the Osano dashboard, `pulumi refresh`
reports the publication's `publishStatus` as `outdated` but never publishes.
Find the edit with `getCookieConsentAuditLog` (section 6), reconcile the
program, change the token deliberately, and run `pulumi up` once. See
[state management](state-management.md) and
[troubleshooting](troubleshooting.md#persistent-outdated-status).

## 10. Adopt existing Cookie Consent resources

Find the IDs with `getCookieConsentConfigs` (configurations) and
`getCookieConsentRules` (each rule's `ruleId`), write the program to match the
remote values, then import:

```bash
pulumi import osano:index:CookieConsentConfig consentConfig <configId>
pulumi import osano:index:CookieConsentRule analyticsRule <configId>/<ruleId>
pulumi import osano:index:CookieConsentPublication publication <configId>
```

Use Pulumi CLI 3.252 or later for imports. Imports only read. An imported
publication adopts the token `import:<lastPublished>:<publishedRevision>`, so
applying your program's own token causes one controlled republish. Details:
[IMPORTING.md](IMPORTING.md).

## 11. Tear down

`pulumi destroy` deletes managed rules in Osano. Osano has no delete or
unpublish endpoint for configurations, so destroying the config and publication
only removes them from Pulumi state: the configuration and its published script
stay live. Remove the script tag from your site and disable the configuration in
Osano if it should stop serving.

## 12. Unified Consent

Submit a consent decision with the `Consent` resource (see
[examples/quickstart](../examples/quickstart)):

```ts
const consent = new osano.Consent("example", {
    subject: { verifiedId: subjectRef },
    actions: [{ target: privacyProtocolId, vendor: configId, action: "ACCEPT" }],
    origin: "api",
    countryCodeOverride: "US",
    regionCodeOverride: "US-CA",
});
```

- `action` must be `ACCEPT`, `REJECT`, or `UNSELECTED`; `origin` must be `api`
  (the default) or `gpc`; `compliance.gpc` must be `0` or `1`; subject IDs must
  not contain `#`, `%`, or spaces; and the override codes must be ISO 3166
  codes. Invalid values fail `pulumi preview`. The `action` and `origin` rules
  apply to new or changed values, so a consent submitted by an earlier provider
  version keeps previewing.
- Without `countryCodeOverride` (ISO 3166-1) and `regionCodeOverride`
  (ISO 3166-2), Osano geolocates the caller's IP address, which in a pipeline
  is the CI runner's. The same inputs exist on `getUnifiedConsent`,
  `checkConsent`, and `getConsentProfile`.
- For a Global Privacy Control consent, set `origin: "gpc"` and omit
  `actions`. Osano derives the actions and the resource exports them as
  `gpcActions`.
- `subject` is a secret in state and outputs, as are the subject identifiers
  and profiles the Unified Consent functions return. `sessionToken` (a secret)
  carries the token returned when the subject's profile was created.
- Every `Consent` input replaces the resource when it changes, which submits a
  new consent record.

Read the subject's merged state back with a function:

```ts
const unified = osano.getUnifiedConsentOutput({ subjectRef });
export const hasConsent = unified.exists;
```

`referenceType` is `subject` (the default) for a verified or anonymous ID, or
`session` for a session ID. `anonymous` is a deprecated alias of `subject`, and
any other value fails the call. `getSubjectProfile` and `getSession` resolve a
subject ID or session ID to profile data; their personal-data outputs are
secrets. Osano documents the Unified Consent API at
`https://uc.api.osano.com` only.

Behavior to plan for:

- Consent records are immutable. Destroy only forgets the Pulumi resource, and
  records cannot be imported.
- `pulumi refresh` looks the subject up by its `verifiedId` or `anonymousId`
  and confirms it still has consent; it keeps your inputs and `lastSynced` (the
  submission time), so a refresh never forces a new submission. If Osano
  reports no consent for the subject, refresh warns and keeps the resource;
  change an input to submit a new consent deliberately.
- Pulumi runs functions on every preview, update, and refresh. That is fine for
  the `get*` and `checkConsent` lookups, but `sendSubjectCode` would send a new
  code each run and `verifySubjectCode` would reuse a one-time code. Run
  subject verification from a dedicated short-lived program or directly from
  your application, not from a long-lived stack. For SMS, pass the `session`
  that `sendSubjectCode` returns (when Osano includes one) to
  `verifySubjectCode`, which requires it with `phone`. `hashedSubjectId` is
  optional on both.

## 13. Contributor loop

```bash
eval "$(mise activate zsh)" && mise install   # pinned toolchain (.config/mise.toml)
make codegen                # after any provider/ change: schema + all SDKs
make lint                   # golangci-lint; make lint_fix applies the fixable findings
make test_provider          # mocked HTTP, no credentials (RACE= without cgo)
make build_examples         # C#/TypeScript Cookie Consent + Go quickstart
make test_e2e_compile       # vets every e2e suite without credentials
make test_pipeline_e2e      # real pulumi up/refresh/destroy against a mock Osano API
make test_all               # tests, e2e compilation, script tests, and the pipeline suite
```

To exercise a change against Osano, install the local plugin (section 1), then
run an example with `pulumi preview` and, only when you intend to create real
resources, `pulumi up`.

The opt-in live suites under [tests/e2e](../tests/e2e) run the provider's
Unified Consent functions and the `Consent` resource against Osano, so they
check what users run. The engine-level pipeline
suite (`make test_pipeline_e2e`) builds the provider and runs a Pulumi YAML
program with real `pulumi up`, `refresh`, and `destroy` against a mock Osano
API; it needs the Pulumi CLI but no credentials. See
[tests/README.md](../tests/README.md). The Cookie Consent lifecycle is covered
by the provider's mocked-HTTP tests, the pipeline suite, and the opt-in example
deployment above.

## Troubleshooting index

| Symptom | Where to look |
| --- | --- |
| `Osano API key not configured` / `Unified Consent API key not configured` / `no Osano API key configured` | [troubleshooting](troubleshooting.md#missing-customer-rest-api-key) |
| `invalid apiBaseUrl`, `API keys are only sent over https`, `requestTimeoutSeconds must be at most 3600`, or a warning that an `OSANO_*` variable is ignored | [troubleshooting](troubleshooting.md#provider-configuration-rejected) |
| Preview fails with `configuration.<key> ...`, or warns about configuration keys | [troubleshooting](troubleshooting.md#configuration-check-failures-and-warnings) |
| `pulumi up --refresh` reports drift or updates the configuration on every run | [troubleshooting](troubleshooting.md#drift-or-an-update-on-every-refresh) |
| Preview shows provider changes after an SDK upgrade | [troubleshooting](troubleshooting.md#provider-changes-after-upgrading-the-sdk) |
| Publication `409`, `429`, `error`, or timeout | [troubleshooting](troubleshooting.md#publication-status-and-http-responses) |
| Create failed with a `5xx` response or no response | [troubleshooting](troubleshooting.md#cookie-consent-create-failed-with-a-server-error) |
| Script URL returns `403` | [troubleshooting](troubleshooting.md#script-url-returns-403) |
| Visitors still see the old revision | [troubleshooting](troubleshooting.md#delayed-cdn-propagation-after-published) |
| Browser blocks `osano.js` or its requests | [troubleshooting](troubleshooting.md#content-security-policy-blocks-osano) |
| `referenceType must be subject or session` | [troubleshooting](troubleshooting.md#referencetype-must-be-subject-or-session) |
| `getUnifiedConsent` returns `exists: false` for a subject with consent | [troubleshooting](troubleshooting.md#lookups-return-exists-false) |
| `osano:index:Consent cannot be imported` | [troubleshooting](troubleshooting.md#osanoindexconsent-cannot-be-imported) |
| `Osano reports no consent for the subject` warning on refresh | [troubleshooting](troubleshooting.md#osano-reports-no-consent-for-the-subject) |
| `session is required to verify an SMS code` | [troubleshooting](troubleshooting.md#session-is-required-to-verify-an-sms-code) |
| Plugin `osano` not found, `404 HTTP error fetching plugin`, or GitHub rate limit | [troubleshooting](troubleshooting.md#provider-plugin-not-found-or-not-downloaded) (from a clone: section 1 of this guide) |
| Verbose request logging | [logging](logging.md) |
