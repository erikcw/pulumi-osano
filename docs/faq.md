# Frequently Asked Questions

**Is this an official Osano integration?**
> No. This is a community-maintained Pulumi provider built against Osano's public APIs. It is not affiliated with Osano.

**Where is the provider published? Is it in the Pulumi Registry?**
> The SDKs are published to npm (`@jflavan/pulumi-osano`), PyPI (`pulumi-osano`), NuGet (`Community.Pulumi.Osano`), Maven Central (`io.github.jflavan.pulumi:pulumi-osano`), and the Go module proxy (`github.com/jflavan/pulumi-osano/sdk/go/osano`). The provider plugin is attached to each [GitHub release](https://github.com/jflavan/pulumi-osano/releases) and downloads automatically. The provider is not listed in the Pulumi Registry yet. See [PUBLISHING.md](./PUBLISHING.md) for install commands and for verifying checksums, signatures, and provenance.

**Which APIs are supported?**
> The provider manages Cookie Consent configurations, rules, and explicit publications through the [Customer REST API](https://developers.osano.com/customer-rest-api), and reads configurations, rules, discoveries, and the audit log with the `getCookieConsent*` functions. It also supports Unified Consent submissions (including Global Privacy Control consents) and functions for consent lookups, subject, profile, and session resolution, configuration and collection reads, consent profiles, and subject verification. Osano operations that do not fit declarative infrastructure as code, such as merging subjects, creating subject profiles, and creating tokens, are not exposed.

**Do I need both API keys?**
> Only for mixed workloads. Cookie Consent resources and functions require the Customer REST API key (secret provider config `osano:osanoApiKey`, or `OSANO_API_KEY`). The `Consent` resource and the other Unified Consent functions require `osano:unifiedConsentApiKey` or `OSANO_UC_API_KEY`. `sendSubjectCode` and `verifySubjectCode` send every configured key, so either one is enough.

**Which wins, the stack configuration or an environment variable?**
> The stack configuration. An environment variable such as `OSANO_API_KEY` is used only when the matching `osano:` key is not configured. When both are set and differ, the provider warns that the stack configuration is used, so a key exported in a pipeline cannot silently point a stack at another Osano account. The same rule applies to the base URLs and to `requestTimeoutSeconds`.

**What causes Cookie Consent to publish?**
> Creating `CookieConsentPublication` queues publication after its declared dependencies. Changing its `changeToken` or publication options queues one in-place republish; an unchanged update does not publish. Include every publish-relevant desired configuration and rule value in a stable token. [`dependsOn`](https://www.pulumi.com/docs/iac/concepts/resources/options/dependson/) controls ordering but does not itself trigger an update, so both the dependencies and token are required.

**Do preview, refresh, or import publish?**
> No. Preview performs no Customer REST API request at all for resources, and refresh/import only read. If external changes make the configuration `outdated`, `pulumi refresh` exposes that status without publishing. A later input change, such as the program's intended `changeToken` replacing an import adoption token, may cause one publication during `pulumi up`.

**How do I install the returned CMP script?**
> `scriptSrc` and `scriptTag` are deliberately public deployment outputs. Pass `scriptTag` to the resource that renders or configures the site and place it first in the site `<head>`, with no `async` or `defer`, so Osano loads before scripts it may control. The examples export a `headHtml` fragment that does this. See [End-to-End Workflow, section 3](./end-to-end-workflow.md#3-hand-the-script-to-the-website) and the [Consent JavaScript API](https://developers.osano.com/cmp/javascript-api/developer-documentation-consent-javascript-api).

**How does a stack that does not manage the configuration get the script?**
> Read the consent stack's `cookieConsentScriptTag` output with a stack reference, or call `getCookieConsentConfig` with the config ID, which also works for configurations managed in the Osano dashboard and returns the publish status. The script URL returns `403` until the configuration's first publication; `lastPublished` is `0` until then.

**How long until a publication reaches visitors?**
> `CookieConsentPublication` completes when Osano reports the configuration as published. Osano's CDN can then take up to 15 minutes to serve the new revision, and browsers cache `osano.js` for 24 hours, so a returning visitor can see the previous revision for up to a day. The script URL does not change between revisions.

**What happens to configuration keys and rule fields I remove from the program?**
> A `CookieConsentConfig.configuration` key the program stops declaring is sent as `null` on the next update, so Osano clears it; keys the program never declared are never touched. A `CookieConsentRule` field the program set and then removed is cleared in Osano too, but a field the program has never set (`title`, `vendorName`, `ruleType`, and for cookies `description` and `expiry`) stays unmanaged: Osano's default or a value set in the dashboard is neither read into state nor changed by an update.

**What does destroy remove from Osano?**
> Managed Cookie Consent rules are deleted upstream. Osano exposes no delete/unpublish endpoint for configurations or publications, so those deletes remove only Pulumi state; the upstream configuration and published script remain active. Immutable Unified Consent events also remain in Osano.

**What if a create request fails with a server error?**
> Osano may have created the configuration or rule before answering, so the provider does not retry creates after a `5xx` (only after `429`). For a `CookieConsentConfig`, it lists the configurations with the requested name and adopts the one Osano created when exactly one matches the name and domains and is at least as new as the request. Otherwise the create fails; check Osano before running `pulumi up` again and import a duplicate instead of creating another, as [troubleshooting](./troubleshooting.md#cookie-consent-create-failed-with-a-server-error) describes.

**Can I import existing consent records?**
> No. `pulumi import osano:index:Consent` fails with an explicit error: Osano exposes only the merged consent of a subject, not individual submissions. Cookie Consent configurations, composite-ID rules, and publications are importable; see [IMPORTING.md](./IMPORTING.md) for exact commands and adoption-token behavior.

**Does this provider store personal data or credentials in state?**
> Pulumi stores resource inputs and outputs. Provider API keys are secrets in provider state; keep source config encrypted with `pulumi config set --secret`. The schema also marks the personal data the provider handles as secret: `Consent.subject` and `Consent.sessionToken`; the subject references, IDs, session IDs, consent records, and profiles the Unified Consent functions take and return; the contact details, code, session, and profile of the subject verification functions; and `CookieConsentPublication.webhookUrl`. Mark `Consent.attributes` and `Consent.tags` secret yourself if they carry personal data. Public CMP `scriptSrc` and `scriptTag` outputs intentionally are not secrets. See [state management](./state-management.md#secrets-and-public-installation-outputs).

**How are API errors surfaced?**
> An error response is reported as `osano api error: status=<code> body=<body>`, with the body cut at 2 KiB; a request that gets no response reports the method and host only, never the path, which can hold a session ID. Errors from `sendSubjectCode` and `verifySubjectCode` withhold the body, which can echo the contact details. Publication terminal-status diagnostics contain `status`, `lastPublished`, and `publishedRevision`; they contain neither the config ID nor the API key. Use the status-specific guidance in [troubleshooting.md](./troubleshooting.md).

**Why did `sendSubjectCode` send a code on `pulumi preview`?**
> Pulumi runs functions (invokes) during every preview, update, and refresh. Declaring `sendSubjectCode` in a stack therefore sends a new code each run, and `verifySubjectCode` re-submits a one-time code that is already used. Call these functions from automation code rather than from a long-lived stack. `verifySubjectCode.code` is marked secret. To verify an SMS code, pass the `session` that `sendSubjectCode` returns to `verifySubjectCode`, which requires it with `phone`.

**Does refresh change my `Consent` resource?**
> Refresh confirms that the subject, identified by its `verifiedId` or `anonymousId`, still has Unified Consent. It keeps the submitted inputs and `lastSynced`, because the unified view merges every consent for the subject and cannot be mapped back to one submission. If Osano reports no consent for the subject, refresh warns and keeps the resource, so a refresh never causes a consent to be submitted again; remove the resource from the program to stop tracking it, or change an input to submit a new consent.

**Why does `pulumi up --refresh` update my Cookie Consent configuration?**
> Refresh compares only the configuration keys your program declares, recursively into nested objects such as `palette`, so with no edits in the program or in Osano a refreshed run is a no-op. `variantMapping` is compared as a whole, and `ccpaRelaxed` next to a non-empty `variantMapping` drifts because Osano rewrites it. See [troubleshooting](./troubleshooting.md#drift-or-an-update-on-every-refresh).

**Where can I ask more questions?**
> Open a [GitHub issue](https://github.com/jflavan/pulumi-osano/issues). Never share real subject identifiers or API keys. Report vulnerabilities as described in [SECURITY.md](../SECURITY.md).
