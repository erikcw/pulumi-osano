# Security Analysis

The provider calls two Osano APIs on behalf of your Pulumi program: the Customer REST API (Cookie Consent configurations, rules, publication, discoveries, and audit log) and the Unified Consent API (consent submissions and lookups). Treat it as part of your compliance boundary.

## Credentials

- Two independent keys exist, each sent as a request header:
  - `x-osano-api-key` (`osano:osanoApiKey` / `OSANO_API_KEY`): the Cookie Consent resources and functions, plus the subject send-code and verify routes.
  - `x-uc-api-key` (`osano:unifiedConsentApiKey` / `OSANO_UC_API_KEY`): the `Consent` resource and the Unified Consent lookups. The subject send-code and verify routes send every configured key, because Osano's guide and its OpenAPI spec name different keys for them.
- Store both with `pulumi config set osano:... --secret`. CI/CD can inject them through `OSANO_API_KEY` and `OSANO_UC_API_KEY`. An environment variable is used only when the stack does not configure the key, and the provider warns when both are set and differ, so an exported key cannot silently point a stack at another account.
- Rotate keys regularly. The provider resolves its configuration on every Pulumi operation, so the next `pulumi preview`, `up`, or `refresh` uses the rotated value, and rotating a key never replaces a resource.
- The keys are secrets in the provider's state and never appear in error messages or logs.

## Network Access

- Cookie Consent requests go to `https://api.osano.com` and Unified Consent requests to `https://uc.api.osano.com` unless `osano:customerBaseUrl` (`OSANO_CUSTOMER_BASE_URL`) or `osano:apiBaseUrl` (`OSANO_API_BASE_URL`) overrides them.
- A base URL must use `https`; `http` is accepted only for loopback hosts, as used by local mocks. Any other plain-`http` URL is rejected when the provider is configured, because the keys travel in headers.
- The HTTP client does not follow redirects, so a key is never re-sent to a host a response chose, and it reads at most 8 MiB of any response.
- Each request attempt times out after `requestTimeoutSeconds` (60 by default, at most 3600). Retries are bounded (three, honoring `Retry-After` up to one minute), and no POST other than the publish request is retried after a `5xx`, so the provider never submits a consent or creates a configuration or rule twice. [performance.md](performance.md#throughput) has the retry matrix.
- The provider uses Go's default HTTP transport, so it honors the standard proxy variables (`HTTPS_PROXY`, `NO_PROXY`) and requires TLS 1.2 or later.

## Data in Transit and at Rest

- Consent payloads contain subject identifiers and possibly IP addresses or tags. They exist in memory inside the provider and in flight to Osano. Without `countryCodeOverride` and `regionCodeOverride`, Osano geolocates the caller's IP address, which in a pipeline is the CI runner's.
- Pulumi state stores the inputs and outputs of every resource. The schema marks the personal data and credentials the provider handles as secret, so Pulumi encrypts them in state and masks them in output. The schema (`provider/cmd/pulumi-resource-osano/schema.json`, properties with `"secret": true`) is the complete list. In summary:
  - the provider's API keys;
  - `Consent.subject` and `Consent.sessionToken`;
  - the subject references, subject IDs, verified and anonymous IDs, session IDs, consent records, conflicts, and profiles that `getUnifiedConsent`, `getSubject`, `getSubjectProfile`, `getSession`, `checkConsent`, and `getConsentProfile` take and return;
  - the `email`, `phone`, `code`, `session`, `destination`, `profile`, and `verifiedId` values of `sendSubjectCode` and `verifySubjectCode`;
  - `CookieConsentPublication.webhookUrl`. Osano calls it without authentication and does not document or sign the payload, so the URL is the only protection: use an unguessable URL and treat the call only as a signal to check the configuration, for example with `getCookieConsentAuditLog`.
- `Consent.attributes` and `Consent.tags` are not secret. Do not put personal data in them, or mark them secret in the program.
- `getCookieConsentAuditLog` returns the email address of the Osano user behind each event (`actor`), which is not marked secret.
- `CookieConsentPublication.scriptSrc` and `scriptTag`, and the same outputs of `getCookieConsentConfig` and `getCookieConsentConfigs`, are public values intended for your site's HTML and are not secret.
- Error messages never include the request path or query, which can hold a session ID, and error responses from the subject verification routes are withheld, because they can echo the subject's email address or phone number. Other error bodies are reported, cut at 2 KiB, so a validation failure can be diagnosed; they can contain the identifiers of the request. Redact them before sharing logs, as [logging.md](logging.md) describes.

## Website Integration

- Osano's script changes with every publication, every Osano CMP release, and by visitor location, so Subresource Integrity and hash-based Content Security Policy sources cannot pin it. Nonces work. The [end-to-end workflow guide](end-to-end-workflow.md#8-content-security-policy) lists the Content Security Policy sources Osano needs.
- Every environment where `osano.js` loads counts toward Osano traffic; give each environment its own configuration.

## Auditing

- Use Pulumi's audit logs (stacks and deployments) to see when consent submissions and Cookie Consent publications were triggered.
- `getCookieConsentAuditLog` reads Osano's Cookie Consent audit log: publications and configuration and rule changes, with the actor and time, including edits made in the Osano dashboard.
- Osano keeps its own immutable consent log; cross-reference Pulumi deployment IDs with the `attributes` you include in consent requests.

## Supply Chain

- The SDKs set `pluginDownloadURL` to `github://api.github.com/jflavan/pulumi-osano`, so Pulumi downloads the provider plugin from this repository's GitHub releases.
- Each release archive, its SBOM, and the NuGet package attached to the release carry a GitHub build provenance attestation. Verify a file before you trust it with `gh attestation verify <file> --owner jflavan`. The copy of the package that nuget.org serves is re-signed by nuget.org and verifies with `dotnet nuget verify`. [PUBLISHING.md](PUBLISHING.md) lists every package, how it is published, and how to verify its provenance or signature.
- A release publishes only after lint, the unit tests, the e2e compilation, and the engine-level pipeline suite pass. Every third-party GitHub Action is pinned to a commit SHA, Dependabot proposes updates for every package ecosystem in the repository, and CodeQL scans the provider, the SDKs, the examples, and the workflows on every pull request.
- `make vulncheck` runs `govulncheck` against the provider's dependencies. As of this writing it reports `GO-2026-6443` in `google.golang.org/grpc`: a gRPC server can be made to panic by a request without an authority or Host header. The provider's gRPC server listens on a loopback port that only the Pulumi engine on the same machine connects to, so the exposure is a local crash of the provider process during a Pulumi run, by a process that can already reach that port. No released grpc version contains the fix yet (it is in a `v1.85.0-dev` pre-release); the dependency comes from the Pulumi SDK and is updated with it.

For vulnerability disclosures see [SECURITY.md](../SECURITY.md).
