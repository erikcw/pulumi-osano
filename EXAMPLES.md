# Example Guidelines for pulumi-osano

This document explains how examples in the Pulumi Osano provider should be organized, documented, and maintained.

## Why Examples Matter

- They double as documentation for users learning the provider.
- They validate APIs by exercising real Pulumi programs.
- They provide regression coverage for future changes (manual or automated).

## Baseline Requirements

Every resource exported by the provider MUST have, and every invoke SHOULD have:

- ✅ A runnable **TypeScript** example.
- ✅ An accompanying `README.md` describing prerequisites, config, and outputs.
- ✅ Clearly named placeholder values (e.g., `your-privacy-protocol-id`).

When adding new features, update or create examples in the same PR.

TypeScript remains the repository-wide baseline. The multi-resource Cookie
Consent publication workflow additionally uses C# as its canonical example so
the complete configuration, rule, dependency, timeout, publication, and output
contract is validated against the generated .NET SDK.

## Recommended Coverage Levels

| Tier | Applies To | Requirement |
| --- | --- | --- |
| Tier 1 (Required) | All resources (invokes as coverage grows) | TypeScript example + README |
| Tier 2 (Preferred) | Frequently used APIs | Add Python and Go variants |
| Tier 3 (Nice to have) | Mission-critical / complex workflows | Include .NET and Java samples or integration-style demos |

## Repository Layout

```
examples/
  quickstart/
    README.md
    typescript/
    python/
    go/
  cookie-consent/
    README.md
    csharp/
    typescript/
  <resource>/
    README.md
    typescript/
    ...
```

Each language folder includes its own `Pulumi.yaml` plus ecosystem-specific metadata (`package.json`, `requirements.txt`, `go.mod`, etc.).

Those dependency files reference the SDKs generated in this repository (`sdk/`), not the published packages, so `make build_examples` compiles the Cookie Consent examples and the Go quickstart against the current code generation. To run an example against the published packages, follow [Run against the published packages](examples/quickstart/README.md#run-against-the-published-packages); [docs/PUBLISHING.md](docs/PUBLISHING.md) lists every package.

## README Template

```markdown
# <Name> Example

This example demonstrates how to use the `osano:<package>:<resource>` resource (or invoke).

## What It Does

<Short description>

## Prerequisites

- Pulumi CLI + language runtime
- `OSANO_UC_API_KEY` (and other config as needed)
- Pulumi stack config commands to set secrets

## Running

```bash
cd typescript
npm install
pulumi up
```

Repeat for each language offered.

## Key Properties

- `privacyProtocolId` – …
- `subjectAnonymousId` – …
```

## Code Quality Checklist

- [ ] Minimal, declarative Pulumi programs (no unnecessary helpers).
- [ ] Helpful inline comments where behavior might surprise newcomers.
- [ ] Outputs that demonstrate meaningful data returned by the provider.
- [ ] Configurable inputs retrieved via `pulumi.Config` (avoid hardcoding secrets).
- [ ] `README.md` validated against actual steps required to run the program.

## Current Coverage Snapshot

| Area | Languages | Notes |
| --- | --- | --- |
| `examples/quickstart` | TypeScript, Python, Go | Demonstrates the `osano:index:Consent` resource |
| `examples/cookie-consent` | C# (canonical), TypeScript | Creates configuration and rules, publishes with `CookieConsentPublication`, and exports `scriptSrc`/`scriptTag` using an explicit `changeToken` |

Functions (invokes) do not have dedicated examples yet. The [end-to-end workflow guide](docs/end-to-end-workflow.md) shows `getCookieConsentConfig`, `getCookieConsentDiscoveries`, `getCookieConsentAuditLog`, and `getUnifiedConsent` in context, and every function's inputs and outputs are documented in the generated SDKs.

Add new rows as additional resources or workflows are introduced.

## Contribution Workflow

1. Build / update provider code.
2. Create or update the example directory.
3. Compile the example locally (`make build_examples`) and run `pulumi preview`
   when credentials and a matching provider plugin are available. Compilation
   and preview must not mutate Osano.
4. Document any new config keys in the example README.
5. Commit the example code alongside provider changes and regenerated SDKs.

## Troubleshooting

- Use `pulumi config` for stack-scoped data (API keys, subject IDs, etc.).
- When referencing local SDK builds during development, point the example at the local SDK (`file:../../../sdk/nodejs/bin` for Node.js, a `replace` directive for Go, a `ProjectReference` for .NET, or an editable `-e ../../../sdk/python` requirement for Python) and install the locally built provider plugin with `pulumi plugin install resource osano 0.1.0-alpha.0+dev --file ./bin/pulumi-resource-osano --exact --reinstall`. The Go SDK embeds its version, so the Go quickstart requests that same plugin version through its `replace` directive. Commands run through `make` export `PULUMI_IGNORE_AMBIENT_PLUGINS`, so they ignore a `pulumi-resource-osano` binary on `PATH`; install the plugin rather than relying on `PATH`.
- If an example requires multiple resources, prefer separate files over large monoliths so users can quickly see the relevant snippet.

## Questions?

Open an issue at https://github.com/jflavan/pulumi-osano/issues if you need help designing or validating an example.
