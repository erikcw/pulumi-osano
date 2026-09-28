# Cookie Consent publication

These examples create an Osano Cookie Consent configuration and its managed
rules, publish that desired state, wait for Osano to report completion, and
export the hosted CMP script URL, the complete script tag, and a `<head>`
fragment for the website. C# is the canonical example; TypeScript is a
companion for this repository's baseline language policy.

Compilation, local provider installation, and `pulumi preview` do not publish
to Osano. Only an opted-in `pulumi up` creates the configuration and rules and
then queues publication. The publication depends on the configuration and
every rule. Its deterministic `changeToken` is the SHA-256 hash of the complete
desired configuration/rule descriptor, and the config and rules are built from
that same descriptor, so any publish-relevant edit republishes exactly once and
an unchanged `pulumi up` does not publish again. The C# and TypeScript programs
serialize the descriptor differently, so their tokens differ for the same
values; never copy a token between them.

For day-2 changes, imports, and teardown in context, see the
[end-to-end workflow guide](../../docs/end-to-end-workflow.md).

## Outputs

| Output | Value |
| --- | --- |
| `cookieConsentScriptSrc` | The hosted script URL, `https://cmp.osano.com/CUSTOMER_ID/CONFIG_ID/osano.js` |
| `cookieConsentScriptTag` | The complete tag, `<script src="..."></script>`, with no `async` or `defer` |
| `headHtml` | A `<head>` fragment with the tag first, followed by `<meta charset="utf-8">` |

`headHtml` shows how a website resource consumes the tag in the same program:
pass `publication.scriptTag` (or a fragment built from it) as an input to the
resource that renders or configures the site, such as a template file, a CDN
edge function, or a hosting provider's head-script setting. That resource then
depends on the publication, so it is created or updated only after Osano
reports the publication complete. The URL is the same for every revision, so a
republish does not change it.

A website stack that does not own the configuration can read the tag from this
stack with a stack reference (`cookieConsentScriptTag`), or from Osano with the
`getCookieConsentConfig` function; see
[Hand the script to the website](../../docs/end-to-end-workflow.md#3-hand-the-script-to-the-website).

## C# from a clone

The committed C# project references the generated SDK at
`sdk/dotnet/Community.Pulumi.Osano.csproj`. From the repository root:

```bash
mise exec -- make build_cookie_consent_examples PROVIDER_VERSION=0.1.0-alpha.0+dev
cd examples/cookie-consent/csharp
pulumi stack init dev
```

The Make target builds the generated .NET and Node.js SDKs, creates ignored
local artifacts including `sdk/dotnet/version.txt` and `sdk/nodejs/bin`, and
compiles both examples. It does not install a provider plugin, run a Pulumi
deployment, or contact Osano.

## C# with the released NuGet package

To use the example outside this clone, copy the `csharp` directory and replace
its local `<ProjectReference>` with the released package reference:

```xml
<PackageReference Include="Community.Pulumi.Osano" Version="0.3.0" />
```

Or, in the copied directory, run
`dotnet remove reference ../../../sdk/dotnet/Community.Pulumi.Osano.csproj`
and then `dotnet add package Community.Pulumi.Osano --version 0.3.0`.

Keep the existing Pulumi package reference, then run `dotnet restore` and
`dotnet build`, followed by `pulumi stack init dev`. In this released-package
workflow, the SDK requests its matching released provider plugin and Pulumi
downloads that plugin automatically when the program runs; do not install the
checkout's dev binary for it.

## TypeScript companion

The TypeScript project references the generated Node.js SDK at
`sdk/nodejs/bin` and uses the same desired values, deterministic SHA-256 token,
dependencies, and 20-minute create/update timeouts:

```bash
mise exec -- make build_cookie_consent_examples PROVIDER_VERSION=0.1.0-alpha.0+dev
cd examples/cookie-consent/typescript
pulumi stack init dev
```

Run the Make command from the repository root. It materializes the ignored
`sdk/nodejs/bin` package before the example's frozen install and TypeScript
compile, as described in the C# section, without publishing to Osano.

## TypeScript with the released npm package

To use the TypeScript companion outside this clone, copy the `typescript`
directory, delete its `yarn.lock` (it pins the local SDK), and install the
released package, which replaces the `file:` dependency in `package.json`:

```bash
npm install @jflavan/pulumi-osano@0.3.0
pulumi stack init dev
```

Current `@pulumi/pulumi` releases require Node.js 22 or later.

As with the NuGet package, the SDK requests its matching released provider
plugin and Pulumi downloads it when the program runs; skip the checkout-only
plugin install below.

## Opt-in live smoke test

The following commands create and publish real resources in the Osano customer
account associated with the API key. Do not run them against an account where
that is not intended.

For either checkout/dev example, return to the repository root after the clone
setup above and build and install the provider at the same exact version as the
local SDK:

```bash
mise exec -- make provider PROVIDER_VERSION=0.1.0-alpha.0+dev
mise exec -- pulumi plugin install resource osano 0.1.0-alpha.0+dev --file ./bin/pulumi-resource-osano --exact --reinstall
cd examples/cookie-consent/csharp # or examples/cookie-consent/typescript
```

The explicit `--file` installs the just-built executable rather than
downloading a provider. These commands do not contact Osano. They deliberately
avoid `make install`, whose SDK linking/package-copy steps are unrelated to
this example. If you are using a released package (NuGet or npm) instead, skip
these checkout-only commands; Pulumi downloads the matching released plugin
declared by that SDK.

In a checkout, run every `pulumi` command in this README, including
`pulumi stack init`, through `mise exec --` or in a shell with mise activated.
The repository's mise configuration sets `PULUMI_HOME` to `.pulumi` inside the
clone, so a plain `pulumi` command uses `~/.pulumi` instead and does not find
the plugin installed above.

From the selected example directory, set the inputs and explicitly deploy:

```bash
export OSANO_API_KEY="replace-with-a-customer-rest-api-key"
pulumi config set domain example.com
pulumi config set storagePolicyHref https://example.com/privacy/cookies
pulumi config set mode permissive
pulumi preview
pulumi up
pulumi stack output cookieConsentScriptTag
pulumi stack output headHtml
```

`pulumi preview` checks the `configuration` object against Osano's published
spec and reports invalid values before anything is sent. `pulumi up` first
creates or updates the configuration and managed rules, then queues
publication and waits for the accepted operation. Osano's CDN can take up to 15
minutes after the API reports the configuration as published to serve the new
revision, and browsers cache `osano.js` for 24 hours. Before the first
publication completes, the script URL returns `403`.

The tag has this form, with no `async` or `defer` attribute:

```html
<script src="https://cmp.osano.com/CUSTOMER_ID/CONFIG_ID/osano.js"></script>
```

Place that tag first in the site's `<head>`, before Google Tag Manager and
analytics tags, so consent controls load before other scripts. Every site that
loads it, including staging and test environments, counts toward Osano traffic,
so use a separate stack, and therefore a separate configuration, per
environment.

## Destroy behavior

`pulumi destroy` deletes the managed rules from Osano. Osano exposes no
delete/unpublish endpoint for the configuration or publication, so removing
those Pulumi resources is state-only: the upstream configuration and its
publication remain active. Remove or disable retained customer resources in
Osano separately when required.
