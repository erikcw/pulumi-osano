# Cookie Consent publication and script delivery design

**Date:** 2026-08-13

**Status:** Approved for implementation

**Target:** Osano hosted Cookie Consent CMP

## Purpose

Complete the provider's Cookie Consent workflow so a Pulumi program can create a CMP configuration, create its classification rules, publish the completed configuration, wait for Osano to finish publishing, and export an immediately deployable CMP script URL and HTML tag.

The required script format is:

```html
<script src="https://cmp.osano.com/{customerId}/{configId}/osano.js"></script>
```

The output must not add `async` or `defer`, because Osano requires the CMP script to load before other scripts that it may control.

## Background

The provider already exposes `CookieConsentConfig` and `CookieConsentRule`, but it does not call Osano's publish endpoint and does not return installation code. Saving the configuration alone is insufficient: Osano requires a configuration to be published before its hosted script is usable.

Publishing cannot safely be folded into `CookieConsentConfig`. Rules are separate Pulumi resources and can still be updating after the configuration update finishes. A separate publication resource creates an explicit dependency boundary after the config and all rules.

Relevant upstream contracts:

- Osano Customer REST API: `POST /v1/cookie-consent/configs/{configId}/publish` returns `204` after queuing asynchronous publication.
- The publish request accepts `keepUnclassifiedTattles`, `description`, and `webhookUrl`.
- A config's `publishStatus` is one of `unpublished`, `in-progress`, `published`, `outdated`, or `error`.
- Osano can return `409` while a config is already being published and `429` when publish capacity or rate guidance is exceeded.
- Config responses expose `customerId`, `configId`, `lastPublished`, and `publishedRevision`, which are sufficient to form the hosted script URL and observe publication progress.

## Goals

- Add an explicit `CookieConsentPublication` custom resource.
- Republish only when relevant desired inputs change.
- Wait for asynchronous publication to succeed before resolving deployment outputs.
- Return both the script URL and exact HTML script tag.
- Make the complete workflow usable and documented in a Pulumi C# project.
- Correct adjacent Customer API lifecycle defects that would make the workflow unreliable.
- Preserve existing resource tokens and existing program compatibility.

## Non-goals

- Managing Osano Unified Consent JavaScript SDK or Consent Prompts.
- Injecting the script directly into every possible website or hosting provider. The provider returns portable outputs that a site resource or deployment system can consume.
- Deleting an Osano CMP configuration or undoing a publication; the Customer REST API exposes neither operation.
- Replacing the flexible CMP `configuration` object with a fully typed model in this change.
- Requiring live Osano credentials in ordinary CI.

## Public Pulumi contract

Register a new resource token:

```text
osano:index:CookieConsentPublication
```

### Inputs

| Input | Type | Required | Behavior |
| --- | --- | --- | --- |
| `configId` | string | yes | The Osano CMP configuration to publish. A change replaces the Pulumi resource. |
| `changeToken` | string | yes | An opaque deterministic token derived from relevant config and rule desired values. A change triggers an in-place republish. |
| `keepUnclassifiedTattles` | boolean | no | Sent to Osano during publication. Default `true` to avoid deleting unclassified discoveries unless the user explicitly opts in. |
| `description` | string | no | Version comment retained in the Osano Admin UI. A change triggers republishing. |
| `webhookUrl` | string | no | Optional callback Osano invokes after publication. A change triggers republishing; the provider still polls and does not depend on the callback. |

`changeToken` is deliberately opaque. The provider does not infer dependency changes from resource ordering because Pulumi's `dependsOn` controls order but does not itself cause a dependent resource update. Documentation will hash the desired config and rules and pass the hash as the token. This republishes on meaningful changes without publishing on every `pulumi up`.

### Outputs

| Output | Type | Description |
| --- | --- | --- |
| `configId` | string | Published Osano configuration ID. |
| `customerId` | string | Osano customer ID read from the configuration. |
| `publishStatus` | string | Last observed publication status; successful creates/updates resolve as `published`. |
| `lastPublished` | integer | Osano's Unix epoch publication timestamp. |
| `publishedRevision` | integer | Published config revision reported by Osano. |
| `scriptSrc` | string | `https://cmp.osano.com/{customerId}/{configId}/osano.js` |
| `scriptTag` | string | `<script src="{scriptSrc}"></script>` |

The provider must derive `scriptSrc` and `scriptTag` from server-returned IDs, not user-supplied display values. These outputs are public deployment data and are not secrets. The Osano API key remains a provider secret and must never appear in them or in diagnostics.

### Resource identity and diff

- The Pulumi resource ID is `configId`.
- `configId` changes use `UpdateReplace`.
- Changes to `changeToken` or any publish request option use an in-place update.
- Unchanged inputs produce no diff and no publish request.
- Only one publication resource should manage a given config in a Pulumi stack.

## Publication lifecycle

### Preview

Preview performs no HTTP requests. It preserves known inputs and leaves server-derived publication metadata and script outputs unknown.

### Create and update

Create and update use the same publication operation:

1. Read the config before publishing to validate it exists and capture the current status, `lastPublished`, and `publishedRevision` as a baseline.
2. Send `POST /v1/cookie-consent/configs/{configId}/publish` with the three publish options.
3. On `204`, poll `GET /v1/cookie-consent/configs/{configId}` until the accepted operation is observably complete.
4. On `409`, assume an operation for this config is already in progress and join the same polling path rather than sending another request.
5. On `429`, honor `Retry-After` when present; otherwise use bounded exponential backoff before retrying the publish request.
6. Retry transient `5xx` responses with bounded backoff. Fail immediately for non-retryable `4xx` responses other than `409` and `429`.
7. Finish successfully only when the config reports `published` and the operation is distinguishable from the pre-publish baseline by a status transition or advanced publication metadata.
8. Fail with an actionable diagnostic when the config reports `error`, disappears, the context is cancelled, or the operation exceeds its deadline.

Polling starts at a modest interval and backs off to avoid excessive API calls. All waits must select on `ctx.Done()` so Pulumi cancellation and custom timeouts stop promptly. A provider-side maximum protects against an unbounded wait when the engine supplies no earlier deadline; documentation will show a Pulumi custom timeout long enough for Osano publication.

The successful state is built from the final config response. It includes the exact script outputs and the desired publication inputs, including the token.

### Read and refresh

Read performs only `GET /v1/cookie-consent/configs/{configId}`. It never publishes.

- If the config exists, refresh status and script outputs while preserving the publication inputs already in Pulumi state.
- If the config returns `404`, return an empty ID so Pulumi treats the publication resource as deleted.
- A later external config change may make the status `outdated`; refresh exposes that drift but does not publish unless an input change causes Pulumi to call update.

### Import

Import uses the config ID:

```bash
pulumi import osano:index:CookieConsentPublication publication <configId>
```

Import reads the config and script metadata without publishing. Because the remote API cannot reconstruct the user's prior publish request or token, import seeds a deterministic adoption token from the observed publication metadata. When the program supplies its intended `changeToken`, a difference causes one controlled republish.

### Delete

Delete is a no-op against Osano and removes only Pulumi state. The documentation must state that the published script remains active and the config remains in Osano after `pulumi destroy`.

## Customer API client behavior

All CMP resources use a common Customer REST API client configuration path:

- Resolve the key from `OSANO_API_KEY` first, then `osano:osanoApiKey`.
- Resolve the base URL from `osano:customerBaseUrl`, defaulting to `https://api.osano.com`.
- Apply `requestTimeoutSeconds` (and the existing environment override where supported) to Customer REST API HTTP calls.
- Preserve `x-osano-api-key` authentication.
- Expose enough structured HTTP error information to distinguish `404`, `409`, `429`, and transient server errors without string matching.
- Parse `Retry-After` in both seconds and HTTP-date forms.

## Existing CMP resource corrections

These changes are included because they directly affect a repeatable config-rules-publication workflow.

### `CookieConsentConfig`

- Treat `404` during read as deleted state.
- Compare the complete `configuration` map during diff so removing a previously managed key is detected.
- Keep all current field names and resource identity behavior.
- Continue no-op deletion because Osano has no delete endpoint.

### `CookieConsentRule`

- Add optional `ruleType` for all store types with Osano's documented enum values.
- Add optional `description` and `expiry` for cookie rules.
- Validate documented length and enum limits and reject cookie-only fields on non-cookie store types.
- Send explicit JSON `null` when a previously set nullable field is removed, allowing `title`, `vendorName`, `ruleType`, `description`, and `expiry` to be cleared.
- Include the new fields in diff and refreshed state.
- Page through `GET /v1/cookie-consent/configs/{configId}/rules` using the returned `next` cursor until the rule is found or all pages are exhausted.
- Adopt composite import IDs in the form `<configId>/<ruleId>`. Existing tracked resources with numeric IDs remain readable using their state-held `configId`; refreshed/imported state normalizes identity without forcing upstream replacement.
- Treat missing rules and config/rule `404` responses as deleted state.

## C# end-to-end example

Add a runnable example under `examples/cookie-consent/csharp` with:

- `Pulumi.yaml`
- a .NET project referencing the generated `Community.Pulumi.Osano` SDK
- `Program.cs`
- a README containing setup, configuration, deploy, output consumption, and destroy caveats

The example will define the configuration and rules as ordinary C# desired-state values, serialize the publish-relevant values with stable ordering, and compute a SHA-256 hash. It will pass that hash as `ChangeToken` and add explicit `DependsOn` entries for the config and every rule.

Conceptual shape:

```csharp
var desiredState = new
{
    Config = configDefinition,
    Rules = ruleDefinitions,
};

var changeToken = Convert.ToHexString(
    SHA256.HashData(Encoding.UTF8.GetBytes(JsonSerializer.Serialize(desiredState)))
).ToLowerInvariant();

var publication = new CookieConsentPublication("publication", new()
{
    ConfigId = consentConfig.ConfigId,
    ChangeToken = changeToken,
    KeepUnclassifiedTattles = true,
}, new CustomResourceOptions
{
    DependsOn = rules.Cast<Resource>().Append(consentConfig).ToArray(),
    CustomTimeouts = new CustomTimeouts
    {
        Create = TimeSpan.FromMinutes(20),
        Update = TimeSpan.FromMinutes(20),
    },
});

return new Dictionary<string, object?>
{
    ["cookieConsentScriptSrc"] = publication.ScriptSrc,
    ["cookieConsentScriptTag"] = publication.ScriptTag,
};
```

The final source must use the actual generated C# types and compile against the generated SDK. The example explains that every publish-relevant desired value must be part of the hash. It must not hash secrets or depend on nondeterministic collection ordering.

## Documentation changes

Update all repository documentation that describes provider capabilities or CMP lifecycle:

- Root `README.md`: include Cookie Consent as a primary capability, correct authentication descriptions, add the C# end-to-end entry point, document the script outputs, and link official Osano guidance.
- `examples/README.md`: list the C# Cookie Consent example.
- New example README: full clone-based and published-package workflows.
- `docs/faq.md`: remove language describing managed Cookie Consent as future work and explain publication, republishing, script order, and no-op delete.
- `docs/IMPORTING.md`: document config, composite rule, and publication imports.
- `docs/state-management.md`: document upstream resources retained after destroy and the role of the publication token.
- `docs/troubleshooting.md`: cover API key selection, publication states, `409`, `429`, timeouts, and stale/outdated configs.
- `docs/UPGRADE.md`: describe the additive resource and rule fields plus the composite import format.
- `docs/RELEASE_CHECKLIST.md` and `docs/RELEASE_GUIDE.md`: require the C# example compile check and call out the new lifecycle in release notes.
- Provider annotations/schema descriptions: document every input, output, default, import ID, and no-op delete behavior so all generated SDKs carry accurate resource documentation.

Documentation will link to the official Osano Customer REST API and Cookie Consent installation/publish guidance, and to Pulumi's resource dependency/custom timeout documentation.

## Testing strategy

### Unit and lifecycle tests

Use `httptest.Server` and the real Customer API client to cover:

- Config create/read/update/no-op delete and `404` removal.
- Complete configuration-map diff, including removed keys.
- Rule create/read/update/delete, nullable field clearing, type validation, pagination, missing state, legacy numeric IDs, and composite imports.
- Publication check/diff/preview.
- Publish request body and authentication.
- Successful asynchronous transitions from unpublished or outdated through in-progress to published.
- Protection against accepting a stale pre-publish `published` response as completion.
- `409` join-and-wait behavior.
- `429` `Retry-After` handling and fallback backoff.
- Transient `5xx` retry and permanent `4xx` failure.
- Terminal `error`, `404`, cancellation, and timeout diagnostics.
- Read/import not publishing.
- Delete not calling Osano.
- Exact `scriptSrc` and `scriptTag` formatting.
- `OSANO_API_KEY`, customer base URL, and request timeout behavior.

Wait and clock behavior will be injectable in tests so retry/poll tests are fast and deterministic.

### Generated artifacts and examples

- Regenerate `provider/cmd/pulumi-resource-osano/schema.json` and every SDK.
- Build Go, Node.js, Python, .NET, and Java SDKs using existing repository targets.
- Compile the C# example against the locally generated .NET SDK.
- Verify generated SDKs expose the resource and outputs with their idiomatic names.

### Repository verification

- Run Go formatting and linting.
- Run `make test_provider` with race detection.
- Run schema/code generation and verify no unexpected generated drift.
- Run `make build_sdks` and the C# example build.
- Run GitNexus impact analysis before symbol edits and `detect_changes` before each commit.

### Optional live verification

Provide an opt-in live test or documented smoke-test path gated by Osano credentials and an explicitly supplied test domain. It must not run in default CI, publish an arbitrary customer config, or delete discoveries unless explicitly configured.

## Error and safety semantics

- Diagnostics identify the operation and config ID but never include API keys or full secret headers.
- The default `keepUnclassifiedTattles = true` avoids surprising upstream data deletion.
- The provider never publishes during preview, refresh, or import.
- A cancelled or failed publish leaves Pulumi reporting failure while preserving the last observed state; the next `pulumi up` can safely retry or join an in-progress operation.
- Script outputs are returned only from a real config response containing both IDs.
- Publication completion and CDN propagation are distinct. The provider waits for Osano's API publication state; documentation notes Osano's stated propagation delay before all edge locations necessarily serve the latest revision.

## Compatibility and rollout

The new resource and fields are additive. Existing config/rule programs continue to compile and behave as before, except for correctness fixes to authentication, diff, reads, pagination, and nullable clearing.

Generated SDKs and schema are committed together with provider changes. Release notes must identify:

- the new end-to-end Cookie Consent publication workflow;
- the `scriptSrc` and `scriptTag` outputs;
- the required `changeToken` pattern;
- the safer default for keeping unclassified discoveries;
- new rule fields and import format;
- retained upstream configuration/publication behavior on destroy.

## Acceptance criteria

The work is complete when:

1. A C# Pulumi program can create a config and rules, publish after those resources settle, and export the exact hosted Osano script tag.
2. Changing any value included in the example's deterministic token triggers exactly one new publication update; an unchanged `pulumi up` triggers none.
3. Pulumi does not report the publication resource created/updated until Osano reports a completed publication corresponding to the accepted operation.
4. Refresh, import, and destroy do not publish.
5. The documented config/rule lifecycle defects are fixed and covered by deterministic tests.
6. All supported SDKs generate and build, and the C# example compiles against the generated .NET SDK.
7. All repository capability, lifecycle, importing, state, troubleshooting, upgrade, and release documentation reflects the completed CMP workflow.
