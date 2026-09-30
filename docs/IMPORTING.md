# Importing Existing Osano Resources

## Cookie Consent resources

Use Pulumi CLI 3.252 or later for `pulumi import`. Earlier versions can delete
an imported resource when the import ID differs from the canonical ID the
provider reports, which can happen with rule IDs.

### Find the IDs

The configuration ID is in the Osano dashboard, or list configurations with
`getCookieConsentConfigs`, filtered by name, domains, organization, mode, or
publish status. List a configuration's rules, with each `ruleId`, with
`getCookieConsentRules`, optionally filtered by `storeType` and
`classification`. For example, in a scratch TypeScript program that has the
Customer REST API key configured:

```ts
import * as osano from "@jflavan/pulumi-osano";

export const configs = osano.getCookieConsentConfigsOutput({ domains: ["example.com"] })
    .configs.apply((items) => items.map((c) => ({ configId: c.configId, name: c.name, mode: c.mode })));

export const ruleImportIds = osano.getCookieConsentRulesOutput({ configId: "<configId>" })
    .rules.apply((rules) => rules.map((r) => `${r.configId}/${r.ruleId}`));
```

`pulumi preview` prints the outputs without creating anything. Both functions
follow Osano's pagination and return every match.

### Import

Existing Cookie Consent configurations, rules, and publications can be adopted
with these exact ID formats:

```bash
pulumi import osano:index:CookieConsentConfig consentConfig <configId>
pulumi import osano:index:CookieConsentRule analyticsRule <configId>/<ruleId>
pulumi import osano:index:CookieConsentPublication publication <configId>
```

A configuration import reads the remote configuration into state. It does not
publish. An empty `additionalLinks` list is left out: Osano reports `[]` for a
configuration without additional links, but its API spec requires at least one
link on write. A rule import requires the composite `<configId>/<ruleId>`
identity and reconstructs `storeType` from Osano's rule type (`cookie` becomes `cookies`,
`script` becomes `scripts`, `iframe` becomes `iframes`). Optional rule fields
that the program does not declare stay unmanaged: they are not read into state,
and later updates leave them as they are in Osano. `description` and `expiry`
apply only to cookie rules, so they are never read into state for other rules,
even though Osano reports `description: ""` on some script rules.

A publication import also performs only a read and never queues publication.
Because Osano cannot reconstruct the caller's prior desired-state token, the
provider adopts the observed publication with:

```text
import:<lastPublished>:<publishedRevision>
```

After adding the imported resource to a program, supply the program's intended
deterministic `changeToken`. If it differs from the adoption token, the first
`pulumi up` may perform one controlled republish. Include every
publish-relevant configuration and rule value in the token, but do not include
API keys or other secrets.

Before importing, ensure the program models the remote values and run
`pulumi preview`. Import and preview do not mutate Osano.

The first `pulumi up` after an import updates in place: it never replaces the
imported configuration or rules, and it republishes at most once, when the
program's `changeToken` differs from the adoption token.

## Unified Consent records

The Osano Unified Consent API records immutable consent events rather than
mutable resources. There is no canonical consent resource that can be imported
after it has been recorded inside Osano.

- `osano:index:Consent` represents a single API submission. Osano does not
  expose identifiers for individual consent actions that map back to Pulumi,
  so these immutable records are not importable: `pulumi import
  osano:index:Consent ...` fails with `osano:index:Consent cannot be imported:
  Osano exposes only the merged consent of a subject, not individual
  submissions`.
- Adopt Pulumi by submitting new desired consent events. Historical records
  remain untouched and queryable in Osano.
- Use `osano:index:getUnifiedConsent` to read the latest view for a subject and
  seed automations without importing a record.

For an importing scenario not covered here, open a
[GitHub issue](https://github.com/jflavan/pulumi-osano/issues) without including
real subject identifiers or API keys.
