# Quickstart

This example deploys the `osano:index:Consent` resource to submit a consent decision for a subject. Three language variants are provided:

- [TypeScript](./typescript)
- [Python](./python)
- [Go](./go)

## Prerequisites

1. Create or locate a Unified Consent API key in the Osano dashboard.
2. Identify the subject reference: a verified subject ID, or an anonymous ID when `subjectType` is `anonymous`. It must not contain `#`, `%`, or spaces.
3. Identify the privacy protocol target ID and configuration (vendor) ID that should receive the consent.
4. If you are running these examples from a repository clone, prepare the local SDK for your language and install the locally built provider plugin. The SDKs in the clone request the development plugin version `0.1.0-alpha.0+dev`, which is never published; that includes the Go SDK, which the Go example resolves through a `replace` directive:

   ```bash
   mise exec -- make nodejs_sdk   # TypeScript only; Python and Go use the checked-in SDK sources
   mise exec -- make provider
   mise exec -- pulumi plugin install resource osano 0.1.0-alpha.0+dev \
     --file ./bin/pulumi-resource-osano --exact --reinstall
   ```

## Running

From the language directory (`typescript`, `python`, or `go`), install the dependencies and create a stack:

```bash
npm install             # TypeScript only
pulumi install          # Python only: creates the venv and installs requirements.txt
pulumi stack init dev
```

Configure the stack before running any example:

```bash
pulumi config set osano:unifiedConsentApiKey --secret
pulumi config set subjectRef <subject-id> --secret
pulumi config set configId <config-id>
pulumi config set privacyProtocolId <protocol-id>
# Optional:
pulumi config set subjectType anonymous      # default: verified
pulumi config set jurisdiction <jurisdiction> # applied to the consent action
```

Each language example loads the same config keys. Then run `pulumi up` to submit the consent.

`subjectType` only chooses the subject field: `verified` submits `subjectRef` as the subject's `verifiedId`, and `anonymous` submits it as `anonymousId`. Osano treats both as subject references, so when you read the consent back with `getUnifiedConsent` or `getSubject`, leave `referenceType` at its default, `subject`, for either kind; use `session` only for a session ID. `pulumi refresh` finds the consent the same way for both kinds of ID.

The examples do not set `countryCodeOverride` or `regionCodeOverride`, so Osano resolves the location from the caller's IP address. When you submit from CI, where that is the runner's address, set the `Consent` inputs `countryCodeOverride` (ISO 3166-1) and `regionCodeOverride` (ISO 3166-2) to the subject's location. Changing any `Consent` input replaces the resource, which submits a new consent record.

In a clone, run these `pulumi` commands through `mise exec --` as well, or in a shell with mise activated. The repository's mise configuration sets `PULUMI_HOME` to `.pulumi` inside the clone, so a plain `pulumi` command uses `~/.pulumi` instead and does not find the plugin installed in step 4.

The examples in this folder are repo-local development examples. The Go example builds against the local SDK through a `replace` directive; `make build_quickstart_examples` compiles it.

## Run against the published packages

To run an example with the published packages instead of the clone's SDKs, copy its language directory out of the repository and switch the SDK reference:

- TypeScript: `npm install @jflavan/pulumi-osano@0.3.0` (replaces the `file:` dependency in `package.json`). Current `@pulumi/pulumi` releases require Node.js 22 or later.
- Python: in `requirements.txt`, replace `-e ../../../sdk/python` with `pulumi-osano==0.3.0`. It requires Python 3.10 or later.
- Go: `go mod edit -dropreplace=github.com/jflavan/pulumi-osano/sdk/go/osano -require=github.com/jflavan/pulumi-osano/sdk/go/osano@v0.3.0 && go mod tidy`. It requires Go 1.26.6 or later.

Skip the local plugin install in step 4 and the mise note above: the published SDKs request provider plugin 0.3.0, and Pulumi downloads it from GitHub on the first `pulumi preview` or `pulumi up`. [docs/PUBLISHING.md](../../docs/PUBLISHING.md) lists every published package and its requirements.

Do not commit these changes to the repository: CI builds the examples against the local SDKs.
