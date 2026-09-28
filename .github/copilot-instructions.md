# Copilot onboarding for `pulumi-osano`

Concise, task-agnostic instructions so an agent can work efficiently without extra repo exploration. Trust these steps first; search only when something here is missing or incorrect. Keep changes minimal and follow the Pulumi provider boilerplate patterns.

## What this repo is
- Unofficial Pulumi **native provider** for Osano's Cookie Consent Customer REST API and Unified Consent API (Go provider built on `pulumi-go-provider` v1.6.0 `infer`, plus generated SDKs for Node/TS, Python, Go, .NET, Java).
- Published from v0.1.0 (2026-09-25): npm `@jflavan/pulumi-osano`, PyPI `pulumi-osano` (import `pulumi_osano`), NuGet `Community.Pulumi.Osano`, Go `github.com/jflavan/pulumi-osano/sdk/go/osano` (tags `sdk/go/osano/vX.Y.Z`), Maven Central `io.github.jflavan.pulumi:pulumi-osano`. The provider plugin ships as GitHub release archives (`github://api.github.com/jflavan/pulumi-osano`), and the SDKs download it automatically. The provider is not in the Pulumi Registry yet. See `docs/PUBLISHING.md`.
- Large repo with many docs/examples; core source lives in `provider/`, generated SDKs in `sdk/`, examples in `examples/`, docs in `docs/`.
- Toolchain versions managed by **mise** (`.config/mise.toml`): Go from the `go.mod` toolchain line (currently go1.27.1; the `go` directive is 1.26.6) and Pulumi from the `github.com/pulumi/pulumi/pkg/v3` version in `go.mod` (currently 3.264.0; both via `scripts/get-versions.sh`), Node `24.13.0`, Python `3.11.8`, .NET `10.0.401`, Java `corretto-11`, Gradle `7.6.6`, pulumictl `0.0.50`, schema-tools `0.6.0`, golangci-lint `2.14.0` (2.7.2 cannot lint a go 1.26+ module), yarn `1.22.22`. Pulumi home pinned to `.pulumi` in repo.

## Bootstrap (do this first)
1. From repo root, activate tools: `eval "$(mise activate bash)" && mise install`. This installs pulumictl/schema-tools etc. and avoids “pulumictl: not found” during builds.
2. Ensure Go/Pulumi in PATH: `go version`, `pulumi version`.
3. For IDE/tests, export Osano credentials if needed:
  - `export OSANO_API_KEY="customer-rest-cmp-key"`
  - `export OSANO_UC_API_KEY="unified-consent-key"`
  These are required only for examples or integration tests that hit the real API.

## Build, lint, and test (validated commands)
- **Provider unit tests (validated locally):**
  - `make test_provider` (runs `go test -short` in `provider/`, ~2–3 minutes). Succeeds locally; saw harmless preamble `pulumictl: not found` when tools not installed—run mise to suppress.
- **Code generation rule (critical):** After any change in `provider/`, run `make codegen` to refresh `provider/cmd/.../schema.json` and all SDKs. CI fails if the worktree is dirty.
- **Build provider only:** `make provider` (outputs `bin/pulumi-resource-osano`).
- **Full build (provider + SDKs):** `make build` (calls `make build_sdks`; heavier).
- **Lint:** `make lint` (golangci-lint using `.golangci.yml`; never rewrites files). `make lint_fix` applies the fixable findings. `make vulncheck` runs `govulncheck`.
- **Examples:** Example programs in `examples/` serve as documentation. `make build_examples` compiles the Cookie Consent examples and the Go quickstart without contacting Osano; run `pulumi up` only for an intentional live test with customer credentials.
- **E2E compile check:** `make test_e2e_compile` vets every `tests/e2e` build-tag set without credentials.
- **Engine-level pipeline e2e:** `make test_pipeline_e2e` builds the provider and runs a Pulumi YAML program with the `pulumi` CLI (`up`, `preview`, `refresh`, `destroy`) against an in-process mock Osano API (`tests/e2e/pipeline`, build tags `e2e pipeline`, opt-in `OSANO_RUN_PIPELINE_E2E=1`, which the target sets). Needs the Pulumi CLI on `PATH`, no credentials; CI runs it as the `pipeline_e2e` job.
- **Language SDK builds (after codegen):** `make build_nodejs|build_python|build_dotnet|build_java|build_go` (`build_go` compiles and vets the Go SDK and the Go quickstart); they expect dependencies from mise and may write artifacts under `sdk/*`.
- **Everything:** `make test_all` runs the provider tests, the e2e compilation, the script tests (`make test_scripts`), and the pipeline suite. `make test_provider` passes `-race`; set `RACE=` on a toolchain without cgo.

## Project layout shortcuts
- `provider/`: Go provider implementation (`provider.go`; `config.go` resolves the provider configuration once per run (stack config wins over `OSANO_*` variables, base URLs must be https, timeout 1..3600) and holds its `DiffConfig`; `unified_consent_client.go`, `unified_consent_resource.go`, and `unified_consent_functions.go` for Unified Consent; `customer_client.go` and `cookie_consent_*.go` for Cookie Consent (`cookie_consent_functions.go` holds the `getCookieConsent*` invokes, `cookie_consent_configuration.go` the configuration checks and refresh projection); `internal/osano` is the one HTTP client both APIs share (retries, redirects, size caps, error redaction); plus tests). `schema_test.go` fails when any resource, function, input, or output lacks a description or a personal-data property is not secret. Entry binary at `provider/cmd/pulumi-resource-osano/main.go`; schema extracted to `provider/cmd/.../schema.json` (it has no `version`; the release stamps it).
- `sdk/`: Generated; do not hand-edit. Language-specific READMEs under each SDK.
- `examples/`: Extensive Pulumi programs serving as documentation and reference implementations.
- `docs/`: Guides, troubleshooting, the release guide and checklist, Pulumi Registry pages, and point-in-time design records under `docs/design/` (historical; the guides are authoritative).
- `tests/`: opt-in live e2e suites that run the provider in-process (`tests/e2e`), the engine-level pipeline suite (`tests/e2e/pipeline`), and the .NET compatibility project (`tests/dotnet`).
- `scripts/`: SDK post-processing scripts with unittest tests (`make test_scripts`) and `get-versions.sh`.
- `Makefile`: All build/test targets and codegen steps; uses `pulumictl convert-version` and `pulumi package gen-sdk`.
- Config & lint: `.config/mise.toml`, `.golangci.yml`. The Pulumi CLI version is derived from `go.mod` by `scripts/get-versions.sh`.
- GitHub Actions (hand-maintained; every third-party action pinned to a commit SHA, Dependabot updates them): `build.yml` (push to main) and `run-acceptance-tests.yml` (every pull request, plus the `/run-acceptance-tests` comment command for fork PRs) run codegen, provider build/tests, the SDK matrix build, example and e2e compilation (`build_examples` job), lint, the engine-level pipeline suite (`pipeline_e2e` job, no secrets), and read-only acceptance tests when secrets exist; the acceptance workflow's `Sentinel` job is the one required status. `pull-request.yml` posts a PR comment for maintainers on fork PRs; `release.yml` publishes on a `vX.Y.Z` tag push after its `verify` job (lint, e2e compilation, script tests, pipeline suite) passes: the GitHub release with archives, SBOMs, checksums and build provenance attestations (GoReleaser); npm, PyPI and NuGet with trusted publishing (the `.nupkg` is attested and attached to the GitHub release, because nuget.org re-signs the copy it serves); Maven Central with the `MAVEN_CENTRAL_*` and `JAVA_SIGNING_*` secrets; and the `sdk/go/osano/vX.Y.Z` tag. A manual run is always a dry run. Every publishing step skips a version the registry already reports, so **Re-run failed jobs** is safe; right after a publish the npm and Maven Central lookups can miss the new version, and that step then fails without republishing (see `docs/RELEASE_GUIDE.md`). `claude.yml` and `claude-code-review.yml` run only when the `CLAUDE_ENABLED` repository variable is `true`. `codeql.yml` (CodeQL Advanced) runs CodeQL `security-extended` for Actions, Go, JavaScript/TypeScript, Python, C#, and Java on pull requests and pushes to `main`, weekly, and on demand. Pull requests are analyzed in full (diff-informed analysis off); each `Analyze` job fails when the PR introduces a CodeQL alert with no counterpart open or dismissed on `main` (`.github/scripts/fail-on-new-codeql-alerts.sh`), and the `main` ruleset requires the `CodeQL gate` job plus the `CodeQL` results check and up-to-date branches. Keep its file name, job ids, the `CodeQL gate` name, matrix languages, and categories stable, keep `codeql-action/init` and `analyze` on the same SHA, and leave code scanning default setup off.

## Working tips to avoid CI friction
- Always run `make codegen` after touching `provider/` Go code, then commit generated changes.
- Ensure tools from mise are installed before any Make target to avoid missing pulumictl/schema-tools.
- Keep changes surgical; avoid editing generated `sdk/` files directly—regenerate instead.
- Provider unit tests use mocked HTTP and do not require Osano API credentials; they clear the `OSANO_*` variables so a developer's shell cannot change them.
- Send every Osano request through the shared client (`customerClient(ctx)` / `newAPIClient(ctx)`); never build an `http.Client` in a resource. Mark personal data `provider:"secret"`, and give a resource a value-based `Diff` when an input becomes secret, so existing state does not diff.
- CI checks for a clean worktree; ensure `git status` clean after codegen/build.
- Package READMEs: npm, PyPI, NuGet, and pkg.go.dev show `docs/package-readmes/{nodejs,python,dotnet,go}.md`, not `README.md`; each shows code only in its own language (`scripts/test_package_readmes.py`). Codegen copies them byte for byte to `sdk/nodejs/README.md`, `sdk/python/README.md`, `sdk/dotnet/README.md`, and `sdk/go/osano/README.md`. Update the copies in the same commit (`make codegen`, or copy the files preserving LF line endings), or CI fails the worktree-clean check.
- Never bump versions in files. The committed schema and SDKs stay at `0.1.0-alpha.0+dev` (Makefile `PROVIDER_VERSION`), and `release.yml` stamps the version from the `vX.Y.Z` tag. See `docs/RELEASE_GUIDE.md`.

## When to search
Use search only if these instructions lack a needed detail or observed behavior diverges (e.g., new targets or errors). Otherwise rely on this file for fast execution.
