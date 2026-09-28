# Tests

## Provider tests

Go provider tests live under `provider/`. Run them with:

```bash
make test_provider
```

They use mocked HTTP and need no credentials. Besides each resource's lifecycle, they cover every
function, the `CookieConsentConfig` configuration checks and refresh convergence
(`cookie_consent_configuration_test.go`, `cookie_consent_convergence_test.go`), API error handling,
pagination caps, and lost-create adoption (`cookie_consent_api_errors_test.go`), the shared HTTP
client's retry, redirect, size, and redaction rules (`internal/osano/client_test.go`),
configuration precedence and validation (`config_resolve_test.go`), the provider version diff, the
rule that a newly secret input never diffs against plain state (`secret_inputs_diff_test.go`), and
contract tests against Osano's Unified Consent OpenAPI spec (`unified_consent_contract_test.go`).
`schema_test.go` fails when a resource, function, input, or output has no description or when a
personal-data property is not secret. `main_test.go` shortens the retry backoff for the whole
package.

Whenever you modify code inside `provider/`, remember to run `make codegen` afterwards so the schema
and SDKs stay in sync.

## .NET version compatibility

The .NET SDK package targets `net8.0`.
[tests/dotnet/SdkCompatibility.csproj](dotnet/SdkCompatibility.csproj) compiles the cookie-consent C#
example against the SDK once per supported .NET version (currently `net8.0` and `net10.0`), so a change
that drops support for one of them fails CI. `make build_examples` builds it; to run it alone:

```bash
make dotnet_sdk
dotnet build tests/dotnet/SdkCompatibility.csproj
```

To support another .NET version, add it to `TargetFrameworks` in that project and make sure the SDK
pinned in `.config/mise.toml` can build it.

## End-to-end tests

Opt-in integration tests live under [tests/e2e](e2e). Shared helpers in
[tests/e2e/internal/testenv](e2e/internal/testenv/env.go) centralize the environment variables
each suite needs. Every test file is guarded by a build tag so you only run the flows you have
credentials for:

- Subject verification: `-tags "e2e subjectverification"`
- Unified consent reads: `-tags "e2e consentread"`
- Consent writes: `-tags "e2e consentwrite"`
- Engine-level pipeline: `-tags "e2e pipeline"` (no credentials; see
  [Engine-level pipeline suite](#engine-level-pipeline-suite))

The first three suites run the provider in-process: `provider_test.go` starts it with the
framework's integration server, configures it from the `OSANO_*` variables, and calls its functions
and the `Consent` resource against Osano, so they test what users run. The full Pulumi workflow
(configuration, rules, publication, script outputs) is covered by mocked provider tests, by the
engine-level pipeline suite, and by the opt-in example deployment described in the
[end-to-end workflow guide](../docs/end-to-end-workflow.md).

In addition to the build tag, each suite checks for an opt-in flag before it runs:

| Suite | Opt-in env | Description |
| --- | --- | --- |
| Subject verification | `OSANO_RUN_SUBJECT_E2E` | Drives send/verify subject code invokes |
| Consent reads | `OSANO_RUN_CONSENT_E2E` | Covers unified consent, subjects, profiles, config, and collections |
| Consent writes | `OSANO_RUN_WRITE_E2E` | Submits a real consent record and validates read-back |
| Engine-level pipeline | `OSANO_RUN_PIPELINE_E2E` | Runs `pulumi up`, `preview`, `refresh`, and `destroy` against a mock Osano API |

The GitHub Actions acceptance workflow runs the read-only suite automatically when the required
repository secrets are configured, and the pipeline suite on every pull request, since it needs no
secrets. Write and subject-verification suites remain opt-in/manual because they either mutate live
data or require a one-time verification code.

CI compiles every build-tag set on each pull request, even without secrets, so a broken e2e suite
fails fast. Run the same check locally with:

```bash
make test_e2e_compile
```

The `acceptance_reads` CI job reads these repository secrets and skips itself (reporting success)
when any required one is missing:

| Secret | Required | Purpose |
| --- | --- | --- |
| `OSANO_UC_API_KEY` | Yes | Unified Consent API key |
| `OSANO_TEST_SUBJECT_REF` | Yes | Subject used for lookups |
| `OSANO_TEST_CONFIG_ID` | Yes | Config ID for consent-profile reads |
| `OSANO_TEST_HASHED_SUBJECT_ID` | Yes | Hashed subject for consent-profile reads |
| `OSANO_TEST_COLLECTION_ID` | Yes | Collection for the direct collection lookup |
| `OSANO_TEST_REFERENCE_TYPE` | No | `subject` (default) or `session`; `anonymous` is accepted and sent as `subject` |
| `OSANO_TEST_COLLECTIONS_JURISDICTION`, `OSANO_TEST_COLLECTIONS_TYPE` | No | Collection filters |
| `OSANO_API_BASE_URL` | No | Non-default Unified Consent host |

All live suites honor an optional `OSANO_API_BASE_URL` to target a non-default Unified Consent host.
Cookie Consent resources have no live e2e suite; their lifecycle is covered by mocked HTTP tests in
`provider/cookie_consent_*_test.go` and by the engine-level pipeline suite, and Unified Consent
invokes by `provider/unified_consent_test.go` and `provider/unified_consent_contract_test.go`.

### Subject verification flow

Test location: [tests/e2e/subject_verification_test.go](e2e/subject_verification_test.go)

Required environment:

1. API key: `OSANO_UC_API_KEY` (the provider sends `OSANO_API_KEY` too when it is set)
2. Delivery channel: `OSANO_VERIFICATION_CHANNEL` ("email" or "sms") and the matching contact:
   `OSANO_VERIFICATION_EMAIL` or `OSANO_VERIFICATION_PHONE`
3. Optional: `OSANO_HASHED_SUBJECT_ID`, sent only when set (Osano identifies the subject by email or
   phone)
4. SMS only: the challenge session Osano requires to verify an SMS code. The test takes it from
   `OSANO_VERIFICATION_SESSION` when set, otherwise from the `session` output of
   `sendSubjectCode`, and fails when neither has one
5. Optional: `OSANO_VERIFICATION_CODE` if you already know the code (otherwise the test prompts)
6. Opt-in: `OSANO_RUN_SUBJECT_E2E=1`

Run:

```bash
go test ./tests/e2e -run TestSendAndVerifySubjectCode -tags "e2e subjectverification"
```

The test sends a fresh verification code every time. Ensure the target inbox/device is under your
control and monitor it while the test runs.

### Unified consent read suite

Test location: [tests/e2e/unified_consent_read_test.go](e2e/unified_consent_read_test.go)

Required environment:

1. API key: `OSANO_UC_API_KEY`
2. Subject lookup: `OSANO_TEST_SUBJECT_REF` and optional `OSANO_TEST_REFERENCE_TYPE`: `subject`
   (the default) for a verified or anonymous ID, or `session` for a session ID. These are the only
   values Osano's `ref` parameter accepts; `anonymous` is still accepted and sent as `subject`
3. Consent profile: `OSANO_TEST_CONFIG_ID` and `OSANO_TEST_HASHED_SUBJECT_ID`
4. Collections: `OSANO_TEST_COLLECTION_ID` plus optional filters `OSANO_TEST_COLLECTIONS_JURISDICTION`
   and `OSANO_TEST_COLLECTIONS_TYPE`
5. Opt-in: `OSANO_RUN_CONSENT_E2E=1`

Run:

```bash
go test ./tests/e2e -run "TestUnifiedConsentLookups|TestConfigAndCollectionsReads" -tags "e2e consentread"
```

The first test exercises unified consent, subject lookup, consent existence checks, and consent
profile reads. The second verifies config, filtered collections, and a direct collection lookup.

### Consent write path

Test location: [tests/e2e/consent_write_test.go](e2e/consent_write_test.go)

Required environment:

1. API key: `OSANO_UC_API_KEY`
2. Subject definition: `OSANO_WRITE_SUBJECT_TYPE` ("verified" or "anonymous") and
   `OSANO_WRITE_SUBJECT_VALUE`
3. Action context: `OSANO_TEST_CONFIG_ID`, `OSANO_WRITE_PRIVACY_PROTOCOL_ID`, and `OSANO_WRITE_ACTION`
4. Opt-in: `OSANO_RUN_WRITE_E2E=1`

Run:

```bash
go test ./tests/e2e -run TestCreateConsentRecord -tags "e2e consentwrite"
```

Each run records a consent with a unique trace attribute, then immediately fetches the unified
consent state to verify the attribute was persisted. It reads the subject back with reference type
`subject` for both verified and anonymous IDs.

### Engine-level pipeline suite

Test location: [tests/e2e/pipeline](e2e/pipeline)

This suite drives the provider through the Pulumi engine the way a deployment pipeline does: it
builds and installs the provider plugin from this repository, then runs a Pulumi YAML program with
the `pulumi` CLI (`up`, `preview`, `refresh`, and `destroy`) against a stateful mock of the Osano
Customer REST API that runs in the test process. It needs no Osano credentials and never contacts
Osano, but it needs the Pulumi CLI and the Go toolchain on `PATH`. It uses a temporary `PULUMI_HOME`
and a local file state backend, so it does not touch your Pulumi account or installed plugins. CI
runs it on every pull request as the `pipeline_e2e` job of the acceptance workflow.

Required environment:

1. Opt-in: `OSANO_RUN_PIPELINE_E2E=1` (the Make target sets it)

Run:

```bash
make test_pipeline_e2e
# or
OSANO_RUN_PIPELINE_E2E=1 go test -tags "e2e pipeline" -count=1 -timeout 20m -v ./tests/e2e/pipeline/...
```

`make test_e2e_compile` vets this suite together with the live ones.

If you introduce new resources or invokes, add mocked HTTP unit tests inside `provider/` first, then
extend the relevant E2E file (or add a new one under `tests/e2e`) once you're ready to exercise the
real API.
