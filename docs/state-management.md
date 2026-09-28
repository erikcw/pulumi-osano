# State Management

Pulumi stores each resource's inputs and outputs in stack state. Keep production
and development stacks separate so credentials, test subjects, and Cookie
Consent configurations are not mixed.

## Secrets and public installation outputs

Store API keys in encrypted Pulumi configuration:

```bash
pulumi config set osano:osanoApiKey --secret
pulumi config set osano:unifiedConsentApiKey --secret
```

CI can provide them through `OSANO_API_KEY` and `OSANO_UC_API_KEY` instead; an
environment variable is used only when the stack does not configure the key.
The provider's key properties are secrets in provider state. Never place
credentials in ordinary resource inputs, outputs, `changeToken`, or
diagnostics.

The schema marks these values secret, so Pulumi encrypts them in state and
masks them in output (`pulumi stack output --show-secrets` reveals them):

- `Consent.subject` and `Consent.sessionToken`.
- The subject references, subject IDs, verified and anonymous IDs, session IDs,
  consent records, conflicts, and profiles that `getUnifiedConsent`,
  `getSubject`, `getSubjectProfile`, `getSession`, `checkConsent`, and
  `getConsentProfile` take and return.
- The `email`, `phone`, `code`, `session`, `destination`, `profile`, and
  `verifiedId` values of `sendSubjectCode` and `verifySubjectCode`.
- `CookieConsentPublication.webhookUrl`: Osano calls it without authentication,
  so the URL itself is the only protection.

`Consent.attributes` and `Consent.tags` are not secret; mark them secret in the
program if they carry personal data. The schema
(`provider/cmd/pulumi-resource-osano/schema.json`) is the complete list.

`CookieConsentPublication.scriptSrc` and `scriptTag`, and the same outputs of
`getCookieConsentConfig` and `getCookieConsentConfigs`, contain only the public
customer/config installation path. They are deliberately non-secret so they can
feed site deployment resources and other stacks.

## Cookie Consent lifecycle

- `CookieConsentConfig` state stores the inputs as applied plus Osano's
  publication metadata. Refresh surfaces drift in declared values, but the
  `configuration` object tracks only the keys the program declares, recursively
  into nested objects such as `palette` and `translations`, so keys Osano adds
  on its side never produce a diff and `pulumi up --refresh` with no edits is a
  no-op. A declared key that Osano omits keeps its declared value.
  `variantMapping` is compared as a whole, so jurisdictions added in Osano show
  as drift. A key the program stops declaring is sent as `null` on the next
  update, so Osano clears it. An import, which has no declared keys, adopts the
  whole configuration Osano reports. Its delete is state-only because Osano
  exposes no configuration delete endpoint.
- A `CookieConsentConfig` create whose request fails with a `5xx` response or
  no response looks for the configuration Osano may have created anyway. If
  exactly one configuration with the requested name and domains was created at
  or after the request, it is adopted into state with a warning, so a transient
  server error does not leave an unmanaged duplicate that Osano cannot delete.
- `CookieConsentRule` uses `<configId>/<ruleId>` identity. An optional field the
  program has never set (`title`, `vendorName`, `ruleType`, and for cookies
  `description` and `expiry`) is neither read into state nor sent on update, so
  Osano's default or a value set in the dashboard stays unmanaged; removing a
  field the program did set clears it in Osano. Deleting the Pulumi resource
  deletes the managed rule from Osano; a missing upstream rule is treated as
  already deleted.
- `CookieConsentPublication` stores the desired publication options and
  caller-managed `changeToken` along with observed status, revision, and script
  outputs. Changing the token or an option causes an update and republish;
  unchanged inputs are a no-op.
- Publication delete is state-only because Osano exposes no unpublish endpoint.
  The upstream configuration and previously published script remain active.
- Provider configuration changes (API key rotation, `requestTimeoutSeconds`,
  base URLs) update the provider in place and never replace the resources it
  manages. An SDK upgrade changes only the provider as well: default providers
  are named after their version, so the new one is created and the old one
  deleted, and an explicit provider resource records the new `version` with an
  in-place update.

Build `changeToken` deterministically from every publish-relevant desired
configuration and rule value. Do not hash API keys or depend on unstable map
ordering. Explicit dependencies ensure the publication waits for configuration
and rule updates; the token ensures those desired-state changes trigger an
update.

## Refresh, preview, import, and destroy

- `pulumi preview` does not publish or otherwise mutate the Customer REST API,
  and resources make no request during it: a preview of a new resource shows
  its inputs, and server-assigned values stay unknown.
- `pulumi refresh` only reads. It can expose an externally changed publication
  as `outdated` but never republishes it.
- Import only reads. Publication import adopts
  `import:<lastPublished>:<publishedRevision>`; applying a program with a
  different intended token may cause one controlled republish.
- `pulumi destroy` deletes managed rules, but configuration/publication removal
  is state-only. Immutable Unified Consent submissions also remain upstream.

For a `Consent` resource, refresh looks the subject up by its `verifiedId` or
`anonymousId` (both are subject references to Osano) and checks that it still
has consent. It keeps the submitted inputs and `lastSynced` (the submission
time) rather than the subject's merged consent view, so a refresh never
triggers a replacement. If Osano reports no consent for the subject, refresh
warns and keeps the resource: a refresh can never cause a consent to be
submitted again. Remove the resource from the program to stop tracking it, or
change an input to submit a new consent deliberately. A `Consent` cannot be
imported, because Osano exposes only the merged consent of a subject. A GPC
consent also stores the actions Osano derived in `gpcActions`. Consent
submissions are append-only events, so removing a logical Pulumi resource never
erases the historical Osano record.

Use Pulumi state diffs and audit logs to understand who applied each desired
state, and `getCookieConsentAuditLog` to see Osano's side: publications and
configuration or rule changes, including edits made in the dashboard. Review
retained Osano configurations separately after stack teardown.
