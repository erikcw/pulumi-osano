# Cookie Consent Publication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Pulumi create an Osano Cookie Consent config and rules, publish the completed config, wait for publication, and export the exact hosted CMP script URL and HTML tag from C# IaC.

**Architecture:** Keep `CookieConsentConfig` and `CookieConsentRule` composable, and add `CookieConsentPublication` as the explicit post-configuration lifecycle boundary. A shared Customer REST client resolves credentials and timeouts consistently; the publication resource queues Osano's asynchronous operation, polls config metadata until that accepted operation completes, and derives portable script outputs from server-returned IDs.

**Tech Stack:** Go 1.24, `pulumi-go-provider/infer`, Go `net/http`/`httptest`, Pulumi schema/code generation, generated Node.js/Python/Go/.NET/Java SDKs, Pulumi C# on .NET 8, Pulumi TypeScript, Markdown.

## Global Constraints

- Target the hosted Osano CMP script exactly as `<script src="https://cmp.osano.com/{customerId}/{configId}/osano.js"></script>`; never add `async` or `defer`.
- Never publish during preview, read/refresh, import, or delete.
- `configId` replacement and `changeToken` updates are the only identity/republish triggers; an unchanged `pulumi up` must not publish.
- Default `keepUnclassifiedTattles` to `true` so the provider does not unexpectedly delete discoveries.
- Treat `409` as an existing operation to join; retry `429` and transient `5xx` responses with bounded, context-aware backoff.
- Resolve Customer API credentials from `OSANO_API_KEY` before `osano:osanoApiKey`, and apply `requestTimeoutSeconds`/`OSANO_API_TIMEOUT_SECONDS` to Customer API calls.
- Keep current resource tokens, fields, and existing numeric rule IDs compatible; normalize new/imported rules to `<configId>/<ruleId>`.
- CMP config/publication deletion remains state-only because Osano exposes no delete/unpublish endpoint.
- Generated SDK files must only be changed through `make codegen`; do not hand-edit files under `sdk/` or `provider/cmd/pulumi-resource-osano/schema.json`.
- Before editing an existing symbol, run GitNexus `impact` upstream on that symbol and warn before proceeding if risk is HIGH or CRITICAL. Before every commit, run GitNexus `detect_changes` and confirm the affected scope.
- Preserve the existing untracked `.claude/`, `AGENTS.md`, and `CLAUDE.md` files; never stage them with feature commits.

## File Structure

- Create `provider/customer_client.go`: Customer REST credential/base URL/timeout resolution and client construction.
- Create `provider/customer_client_test.go`: environment precedence, timeout, missing-key, and base URL tests.
- Modify `provider/internal/osano/client.go`: injectable HTTP client option while retaining retry behavior.
- Modify `provider/internal/osano/client_test.go`: custom HTTP client/timeout and retry coverage.
- Modify `provider/internal/osano/errors.go`: typed status inspection helper.
- Modify `provider/internal/osano/errors_test.go`: wrapped-error status checks.
- Modify `provider/config.go`: accurate Customer API configuration descriptions and shared timeout semantics.
- Modify `provider/cookie_consent_config.go`: use the shared client, full-map diff, and `404` deletion behavior.
- Modify `provider/cookie_consent_config_test.go`: config diff and mocked lifecycle coverage.
- Modify `provider/cookie_consent_rule.go`: supported fields, nullable clearing, pagination, composite IDs, and `404` behavior.
- Modify `provider/cookie_consent_rule_test.go`: validation, payload, pagination, import, compatibility, and lifecycle coverage.
- Create `provider/cookie_consent_publication.go`: publication contract, diff, publish/poll/read/import/delete lifecycle, and script derivation.
- Create `provider/cookie_consent_publication_test.go`: deterministic asynchronous publication lifecycle coverage.
- Create `provider/cookie_consent_test_helpers_test.go`: in-memory provider server, CMP URNs, config, and JSON API helpers shared by lifecycle tests.
- Modify `provider/provider.go`: register and describe `CookieConsentPublication` and broaden package descriptions to Cookie Consent plus Unified Consent.
- Regenerate `provider/cmd/pulumi-resource-osano/schema.json` and `sdk/{dotnet,go,java,nodejs,python}`.
- Create `examples/cookie-consent/csharp/{Pulumi.yaml,CookieConsent.csproj,Program.cs}`: canonical C# end-to-end program.
- Create `examples/cookie-consent/typescript/{Pulumi.yaml,package.json,yarn.lock,tsconfig.json,index.ts}`: repository-policy TypeScript companion.
- Create `examples/cookie-consent/README.md`: setup, publish lifecycle, output installation, live smoke test, and retained-upstream-state guidance.
- Modify `README.md`, `EXAMPLES.md`, `CONTRIBUTING.md`, `examples/README.md`: advertise, index, and validate the completed CMP workflow.
- Modify `docs/{IMPORTING.md,faq.md,state-management.md,troubleshooting.md,UPGRADE.md,RELEASE_CHECKLIST.md,RELEASE_GUIDE.md}`: replace future-work text and document the entire lifecycle.
- Regenerate `sdk/{dotnet,nodejs,python}/README.md` after the root README changes so published packages carry the same lifecycle documentation.
- Modify `Makefile`: add example compilation targets and include them in repeatable verification without contacting Osano.

---

### Task 1: Unify and test Customer REST API client settings

**Files:**
- Create: `provider/customer_client.go`
- Create: `provider/customer_client_test.go`
- Modify: `provider/internal/osano/client.go`
- Modify: `provider/internal/osano/client_test.go`
- Modify: `provider/internal/osano/errors.go`
- Modify: `provider/internal/osano/errors_test.go`
- Modify: `provider/config.go`
- Modify: `provider/cookie_consent_config.go`

**Interfaces:**
- Produces: `customerClientFromConfig(Config) (*osano.Client, error)` used by every CMP resource.
- Produces: `customerSettingsFromConfig(Config) (customerSettings, error)` with `baseURL *url.URL`, `apiKey string`, and `timeout time.Duration`.
- Produces: `osano.WithHTTPClient(*http.Client) ClientOption` for request-timeout injection.
- Produces: `osano.IsHTTPStatus(error, int) bool` for `404`, `409`, and retry exhaustion decisions.

- [ ] **Step 1: Run pre-edit impact analysis**

Run GitNexus upstream impact for `NewClient`, `customerClientFromConfig`, `Config.Annotate`, and `Config.Configure`. Record direct callers and affected processes. Stop and warn the user before editing if any result is HIGH or CRITICAL.

- [ ] **Step 2: Write failing internal-client and settings tests**

Add these behaviors to the named test files:

```go
func TestNewClientUsesProvidedHTTPClient(t *testing.T) {
	t.Parallel()
	baseURL, err := url.Parse("https://api.example.com")
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Timeout: 17 * time.Second}
	client := NewClient(baseURL, "x-osano-api-key", "key", WithHTTPClient(httpClient))
	if client.http != httpClient {
		t.Fatal("expected NewClient to retain the provided HTTP client")
	}
}

func TestIsHTTPStatusUnwrapsErrors(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("read config: %w", &HTTPError{StatusCode: http.StatusNotFound})
	if !IsHTTPStatus(err, http.StatusNotFound) {
		t.Fatal("expected wrapped 404 to match")
	}
	if IsHTTPStatus(err, http.StatusConflict) {
		t.Fatal("did not expect wrapped 404 to match 409")
	}
}

func TestCustomerSettingsFromConfig(t *testing.T) {
	t.Setenv(envOsanoAPIKey, "environment-key")
	t.Setenv(envRequestTimeout, "7")
	settings, err := customerSettingsFromConfig(Config{
		OsanoAPIKey:           "config-key",
		CustomerBaseURL:       "https://customer.example.test/root",
		RequestTimeoutSeconds: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.apiKey != "environment-key" {
		t.Fatalf("expected environment key, got %q", settings.apiKey)
	}
	if settings.timeout != 7*time.Second {
		t.Fatalf("expected 7s timeout, got %s", settings.timeout)
	}
	if settings.baseURL.String() != "https://customer.example.test/root" {
		t.Fatalf("unexpected base URL %s", settings.baseURL)
	}
}
```

Also test whitespace-only environment values falling back to Pulumi config, invalid/non-positive environment timeouts falling back to config/default 60 seconds, integer and HTTP-date `Retry-After` forms, malformed `customerBaseUrl`, and the exact missing-key diagnostic mentioning both `osano:osanoApiKey` and `OSANO_API_KEY`.

- [ ] **Step 3: Run the focused tests and confirm RED**

Run:

```bash
mise exec -- go test ./provider/internal/osano ./provider -run 'Test(NewClientUsesProvidedHTTPClient|IsHTTPStatusUnwrapsErrors|CustomerSettingsFromConfig)$' -count=1
```

Expected: compilation failures for `WithHTTPClient`, `IsHTTPStatus`, and `customerSettingsFromConfig`.

- [ ] **Step 4: Implement the shared settings and typed status surface**

Move `customerClientFromConfig` out of `cookie_consent_config.go` into `customer_client.go` and implement the following concrete shape:

```go
type customerSettings struct {
	baseURL *url.URL
	apiKey  string
	timeout time.Duration
}

func customerSettingsFromConfig(cfg Config) (customerSettings, error) {
	apiKey := getFirstNonEmpty(os.Getenv(envOsanoAPIKey), cfg.OsanoAPIKey)
	if apiKey == "" {
		return customerSettings{}, errors.New(
			"Osano API key not configured; set osano:osanoApiKey or OSANO_API_KEY",
		)
	}

	baseURL, err := url.Parse(cfg.customerBaseURL())
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return customerSettings{}, fmt.Errorf("invalid customerBaseUrl %q", cfg.customerBaseURL())
	}

	timeoutSeconds := cfg.RequestTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultRequestTimeoutSecs
	}
	if value := strings.TrimSpace(os.Getenv(envRequestTimeout)); value != "" {
		if parsed, parseErr := strconv.Atoi(value); parseErr == nil && parsed > 0 {
			timeoutSeconds = parsed
		}
	}

	return customerSettings{
		baseURL: baseURL,
		apiKey:  strings.TrimSpace(apiKey),
		timeout: time.Duration(timeoutSeconds) * time.Second,
	}, nil
}

func customerClientFromConfig(cfg Config) (*osanoclient.Client, error) {
	settings, err := customerSettingsFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return osanoclient.NewClient(
		settings.baseURL,
		"x-osano-api-key",
		settings.apiKey,
		osanoclient.WithHTTPClient(newHTTPClient(settings.timeout)),
	), nil
}
```

Add the client option and status helper:

```go
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		if httpClient != nil {
			c.http = httpClient
		}
	}
}

func IsHTTPStatus(err error, statusCode int) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == statusCode
}
```

Update `Config.Annotate` so `osanoApiKey` explicitly covers Customer REST API/CMP operations as well as administrative routes, and `requestTimeoutSeconds` explicitly covers both Customer and Unified Consent calls.

- [ ] **Step 5: Run focused and package tests and confirm GREEN**

Run:

```bash
mise exec -- gofmt -w provider/customer_client.go provider/customer_client_test.go provider/config.go provider/cookie_consent_config.go provider/internal/osano/client.go provider/internal/osano/client_test.go provider/internal/osano/errors.go provider/internal/osano/errors_test.go
mise exec -- go test ./provider/internal/osano ./provider -count=1
```

Expected: both packages pass; no test contacts a real Osano endpoint.

- [ ] **Step 6: Inspect scope and commit**

Run GitNexus `detect_changes({scope: "all"})`, confirm only Customer-client/config paths and their known CMP callers are affected, then commit only the task files:

```bash
git add provider/customer_client.go provider/customer_client_test.go provider/config.go provider/cookie_consent_config.go provider/internal/osano/client.go provider/internal/osano/client_test.go provider/internal/osano/errors.go provider/internal/osano/errors_test.go
git diff --cached --check
git commit -m "fix(cmp): honor customer API credentials and timeouts"
```

### Task 2: Correct Cookie Consent config diff and read lifecycle

**Files:**
- Modify: `provider/cookie_consent_config.go`
- Modify: `provider/cookie_consent_config_test.go`
- Create: `provider/cookie_consent_test_helpers_test.go`

**Interfaces:**
- Consumes: `customerClientFromConfig` and `osano.IsHTTPStatus` from Task 1.
- Produces: config reads that return an empty Pulumi ID on Osano `404`.
- Produces: full JSON-semantic comparison through `jsonValuesEqual(any, any) bool`.
- Produces: `newCMPProviderServer(*testing.T, string) integration.Server` and `cmpURN(string, string) resource.URN` for later lifecycle tests.

- [ ] **Step 1: Run pre-edit impact analysis**

Run GitNexus upstream impact for `CookieConsentConfig.Read`, `CookieConsentConfig.Diff`, `cookieConsentConfigStateFromResponse`, and `Provider`. Report the blast radius and stop for HIGH/CRITICAL risk.

- [ ] **Step 2: Add failing diff and mocked read tests**

Extend `TestCookieConsentConfigDiff` with removal detection:

```go
t.Run("removed configuration key", func(t *testing.T) {
	state := baseConfigState()
	inputs := baseConfigArgs()
	inputs.Configuration = map[string]any{
		"storagePolicyHref": "https://example.com/storage-policy",
	}
	resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentConfigArgs, CookieConsentConfigState]{
		State: state, Inputs: inputs,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertDiffKind(t, resp, "configuration", p.Update)
})
```

Add an in-memory provider helper that configures the real inferred provider against an `httptest.Server`:

```go
func cmpURN(resourceType, name string) resource.URN {
	return resource.NewURN(
		"test-stack", "test-project", "",
		tokens.Type("osano:index:"+resourceType), name,
	)
}

func newCMPProviderServer(t *testing.T, customerBaseURL string) integration.Server {
	t.Helper()
	t.Setenv(envOsanoAPIKey, "")
	server, err := integration.NewServer(
		t.Context(), Name, semver.MustParse("1.0.0"), integration.WithProvider(Provider()),
	)
	if err != nil {
		t.Fatal(err)
	}
	err = server.Configure(p.ConfigureRequest{Args: property.NewMap(map[string]property.Value{
		"osanoApiKey": property.New("test-osano-key"),
		"customerBaseUrl": property.New(customerBaseURL),
		"requestTimeoutSeconds": property.New(2.0),
	})})
	if err != nil {
		t.Fatal(err)
	}
	return server
}
```

Use it to call `Read` for `CookieConsentConfig` while the mock returns `404`, asserting `resp.ID == ""`. Add a successful import-style read with empty prior inputs and assert both returned state and returned inputs are reconstructed from Osano (`name`, `domains`, `mode`, `orgIds`, and `configuration`). Add mocked create/update cases that assert POST/PATCH payloads and response metadata, plus a delete case proving no HTTP request is made. Assert request paths and the `x-osano-api-key` header throughout.

- [ ] **Step 3: Run focused tests and confirm RED**

Run:

```bash
mise exec -- go test ./provider -run 'TestCookieConsentConfig(Diff|ReadLifecycle)$' -count=1
```

Expected: the removed-key case reports no diff and the `404` read returns an error.

- [ ] **Step 4: Implement full-map comparison and deleted-state handling**

Replace the one-sided key loop with complete JSON comparison:

```go
func jsonValuesEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

if !jsonValuesEqual(req.Inputs.Configuration, req.State.Configuration) {
	diff["configuration"] = p.PropertyDiff{Kind: p.Update}
}
```

In `Read`, handle the typed error before wrapping:

```go
err = client.DoJSON(ctx, http.MethodGet, cookieConsentConfigPath(req.ID), nil, nil, &out)
if osanoclient.IsHTTPStatus(err, http.StatusNotFound) {
	return infer.ReadResponse[CookieConsentConfigArgs, CookieConsentConfigState]{ID: ""}, nil
}
if err != nil {
	return infer.ReadResponse[CookieConsentConfigArgs, CookieConsentConfigState]{},
		fmt.Errorf("read Cookie Consent config %q: %w", req.ID, err)
}
```

Build `state := cookieConsentConfigStateFromResponse(out)` and return `Inputs: state.CookieConsentConfigArgs`, not `req.Inputs`, so import and refresh both reconstruct accurate desired inputs. Use `http.MethodGet/Post/Patch` constants and a small `cookieConsentConfigPath(configID string) string` helper so publication reuses the exact escaped path. Expand `CookieConsentConfigState.Annotate` to describe customer ID, timestamps, publish status, revision, and retained-upstream delete behavior in generated SDK documentation.

- [ ] **Step 5: Run focused and provider tests**

Run:

```bash
mise exec -- gofmt -w provider/cookie_consent_config.go provider/cookie_consent_config_test.go provider/cookie_consent_test_helpers_test.go
mise exec -- go test ./provider -run 'TestCookieConsentConfig' -count=1
mise exec -- go test ./provider -count=1
```

Expected: all tests pass and the mock sees no request beyond the expected GET.

- [ ] **Step 6: Inspect scope and commit**

Run GitNexus `detect_changes({scope: "all"})`, review config lifecycle impacts, then:

```bash
git add provider/cookie_consent_config.go provider/cookie_consent_config_test.go provider/cookie_consent_test_helpers_test.go
git diff --cached --check
git commit -m "fix(cmp): correct cookie consent config lifecycle"
```

### Task 3: Complete Cookie Consent rule fields, imports, and pagination

**Files:**
- Modify: `provider/cookie_consent_rule.go`
- Modify: `provider/cookie_consent_rule_test.go`
- Modify: `provider/cookie_consent_test_helpers_test.go`

**Interfaces:**
- Consumes: shared Customer client/status helper and in-memory provider test server.
- Produces: optional `ruleType`, `description`, and `expiry` Pulumi inputs/outputs.
- Produces: `parseRuleResourceID(id, stateConfigID string) (configID string, ruleID int, canonicalID string, error)`.
- Produces: cursor-based `findCookieConsentRule(context.Context, jsonClient, string, int) (cmpRuleResponse, bool, error)`.

- [ ] **Step 1: Run pre-edit impact analysis**

Run GitNexus upstream impact for every existing rule lifecycle symbol: `CookieConsentRule.Check`, `CookieConsentRule.Create`, `CookieConsentRule.Read`, `CookieConsentRule.Update`, `CookieConsentRule.Delete`, `CookieConsentRule.Diff`, and `ruleResponseToState`. Stop and report any HIGH/CRITICAL result.

- [ ] **Step 2: Add failing validation, diff, payload, pagination, and import tests**

Extend args and base fixtures to exercise the new fields:

```go
type CookieConsentRuleArgs struct {
	ConfigID       string  `pulumi:"configId"`
	StoreType      string  `pulumi:"storeType"`
	Classification string  `pulumi:"classification"`
	Rule           string  `pulumi:"rule"`
	Disclosure     bool    `pulumi:"disclosure,optional"`
	Title          *string `pulumi:"title,optional"`
	VendorName     *string `pulumi:"vendorName,optional"`
	RuleType       *string `pulumi:"ruleType,optional"`
	Description    *string `pulumi:"description,optional"`
	Expiry         *string `pulumi:"expiry,optional"`
}
```

Add table cases for all documented `ruleType` values, invalid rule type, title length 65, vendor length 101, description length 1001, expiry length 51, and `description`/`expiry` rejected when `storeType != "cookies"`. Add diff assertions for each new field.

Add mocked lifecycle cases that assert:

```json
{
  "classification": "ANALYTICS",
  "rule": "_ga",
  "disclosure": true,
  "title": null,
  "vendorName": null,
  "ruleType": null,
  "description": null,
  "expiry": null
}
```

is sent by PATCH when previously set nullable values are removed. For pagination, return page one with `{"items":[],"next":"page-2"}` and page two containing rule `42`; assert the second request contains `?next=page-2&limit=500`. Test imports with `config-abc/42`, existing-state reads with legacy ID `42` plus `state.ConfigID`, malformed composite IDs, a read `404` returning empty state, and a delete `404` succeeding idempotently. For import, assert `type: "cookie"` reconstructs input `storeType: "cookies"` and that returned Pulumi inputs contain every field read from Osano.

- [ ] **Step 3: Run focused tests and confirm RED**

Run:

```bash
mise exec -- go test ./provider -run 'TestCookieConsentRule|TestParseRuleResourceID|TestFindCookieConsentRule' -count=1
```

Expected: new fields and helpers are undefined; pagination/import/clear cases fail.

- [ ] **Step 4: Implement supported rule fields and validation**

Add fields to args/response/state mapping, annotate them with documented constraints, and define:

```go
var validRuleTypes = map[string]bool{
	"FILENAME": true, "DOMAIN": true, "PATH": true, "REGEXP": true,
	"STARTS_WITH": true, "ENDS_WITH": true, "CONTAINS": true, "EXACT_MATCH": true,
}
```

Validate pointer values only when non-nil. Require `description` and `expiry` to be nil unless `StoreType == "cookies"`. Include all new fields in `Diff` with `p.Update`.

- [ ] **Step 5: Implement explicit-null payloads, composite IDs, and cursor reads**

Use a payload builder that always includes nullable keys on update and includes non-nil keys on create:

```go
func cookieConsentRulePayload(args CookieConsentRuleArgs, includeNulls bool) map[string]any {
	payload := map[string]any{
		"classification": args.Classification,
		"rule":           args.Rule,
		"disclosure":     args.Disclosure,
	}
	putNullable := func(key string, value *string) {
		if value != nil {
			payload[key] = *value
		} else if includeNulls {
			payload[key] = nil
		}
	}
	putNullable("title", args.Title)
	putNullable("vendorName", args.VendorName)
	putNullable("ruleType", args.RuleType)
	if args.StoreType == "cookies" {
		putNullable("description", args.Description)
		putNullable("expiry", args.Expiry)
	}
	return payload
}
```

Implement IDs with `strings.LastIndex` so config IDs remain opaque:

```go
func canonicalRuleID(configID string, ruleID int) string {
	return fmt.Sprintf("%s/%d", configID, ruleID)
}
```

`parseRuleResourceID` accepts a composite ID directly; for a numeric ID it requires `stateConfigID`. Create returns the canonical composite ID. Read normalizes legacy IDs to canonical IDs. Delete accepts either form and uses only the parsed rule ID for `/v1/cookie-consent/rules/{ruleId}`.

Map response types back to request buckets during refresh/import:

```go
func ruleStoreType(responseType string) (string, error) {
	switch responseType {
	case "cookie":
		return "cookies", nil
	case "script":
		return "scripts", nil
	case "iframe":
		return "iframes", nil
	case "localStorage":
		return "localStorage", nil
	default:
		return "", fmt.Errorf("unsupported Cookie Consent rule type %q", responseType)
	}
}
```

`ruleResponseToState` must prefer server-returned `ConfigID` and mapped `Type` when reconstructing import inputs. Read returns `Inputs: state.CookieConsentRuleArgs`. Delete treats typed `404` as success, while other errors still fail.

Extend the list response with its cursor:

```go
type cmpRulesListResponse struct {
	Items []cmpRuleResponse `json:"items"`
	Next  string            `json:"next"`
}
```

Query with `limit=500`, and loop until found or `Next == ""`. Reject a repeated cursor with an explicit error to prevent infinite loops.

- [ ] **Step 6: Run focused and provider tests**

Run:

```bash
mise exec -- gofmt -w provider/cookie_consent_rule.go provider/cookie_consent_rule_test.go provider/cookie_consent_test_helpers_test.go
mise exec -- go test ./provider -run 'TestCookieConsentRule|TestParseRuleResourceID|TestFindCookieConsentRule' -count=1
mise exec -- go test ./provider -count=1
```

Expected: validation, payload, import, pagination, legacy-ID, and `404` cases pass.

- [ ] **Step 7: Inspect scope and commit**

Run GitNexus `detect_changes({scope: "all"})`, verify affected flows are limited to CMP rule CRUD, then:

```bash
git add provider/cookie_consent_rule.go provider/cookie_consent_rule_test.go provider/cookie_consent_test_helpers_test.go
git diff --cached --check
git commit -m "fix(cmp): complete cookie consent rule lifecycle"
```

### Task 4: Add the asynchronous CookieConsentPublication resource

**Files:**
- Create: `provider/cookie_consent_publication.go`
- Create: `provider/cookie_consent_publication_test.go`
- Modify: `provider/cookie_consent_test_helpers_test.go`
- Modify: `provider/provider.go`

**Interfaces:**
- Consumes: `cmpConfigResponse`, `cookieConsentConfigPath`, Customer client/status helpers, and test server.
- Produces: `CookieConsentPublication`, `CookieConsentPublicationArgs`, and `CookieConsentPublicationState` for schema generation.
- Produces: `publishCookieConsent(context.Context, jsonClient, CookieConsentPublicationArgs, publicationPollOptions) (CookieConsentPublicationState, error)`.
- Produces: `cookieConsentScript(customerID, configID string) (src, tag string, error)`.

- [ ] **Step 1: Run pre-edit impact analysis**

The resource symbols are new. Run GitNexus upstream impact for the existing `Provider` registration function, `cmpConfigResponse`, `cookieConsentConfigPath`, and `customerClientFromConfig` before editing their consumers/registration. Stop and warn on HIGH/CRITICAL risk.

- [ ] **Step 2: Write failing contract and diff tests**

Define tests against this public contract:

```go
type CookieConsentPublicationArgs struct {
	ConfigID                  string  `pulumi:"configId"`
	ChangeToken               string  `pulumi:"changeToken"`
	KeepUnclassifiedTattles   *bool   `pulumi:"keepUnclassifiedTattles,optional"`
	Description               *string `pulumi:"description,optional"`
	WebhookURL                *string `pulumi:"webhookUrl,optional"`
}

type CookieConsentPublicationState struct {
	CookieConsentPublicationArgs
	CustomerID        string `pulumi:"customerId"`
	PublishStatus     string `pulumi:"publishStatus"`
	LastPublished     int    `pulumi:"lastPublished"`
	PublishedRevision int    `pulumi:"publishedRevision"`
	ScriptSrc         string `pulumi:"scriptSrc"`
	ScriptTag         string `pulumi:"scriptTag"`
}
```

Test missing `configId`/`changeToken`, `keepUnclassifiedTattles` defaulting to true, invalid webhook URL, no diff for identical args, `UpdateReplace` for `configId`, and `Update` for token/options. Assert exact script formatting:

```go
src, tag, err := cookieConsentScript("xpYRGjjVrA", "4c5ae68c-68d7-41ac-8ab3-08ff308bf254")
if err != nil {
	t.Fatal(err)
}
if src != "https://cmp.osano.com/xpYRGjjVrA/4c5ae68c-68d7-41ac-8ab3-08ff308bf254/osano.js" {
	t.Fatalf("unexpected src %q", src)
}
if tag != `<script src="https://cmp.osano.com/xpYRGjjVrA/4c5ae68c-68d7-41ac-8ab3-08ff308bf254/osano.js"></script>` {
	t.Fatalf("unexpected tag %q", tag)
}
```

- [ ] **Step 3: Write failing asynchronous lifecycle tests**

Use `httptest.Server` and `publishCookieConsent` with zero-duration deterministic polling to cover:

- Baseline `outdated`, POST body/options, `204`, `in-progress`, then `published` with advanced metadata.
- Baseline `published`, POST `204`, a stale `published` GET with unchanged metadata, then `in-progress`, then completed `published`; assert the stale response is not accepted.
- POST `409` followed by `in-progress`/`published`, with no second POST.
- POST `429`, then `500`, then `204`, verifying the internal client retries and the final state succeeds.
- Terminal config `error`, config `404`, repeated stale `published` until context cancellation, and missing customer/config IDs.
- Create/update preview makes zero HTTP calls.
- Read/import makes only one GET and never POSTs.
- Delete makes zero HTTP calls.
- Imported state gets `changeToken = "import:<lastPublished>:<publishedRevision>"` and defaults `keepUnclassifiedTattles` to true.

For the stale-response test, assert request order exactly:

```text
GET /v1/cookie-consent/configs/config-id
POST /v1/cookie-consent/configs/config-id/publish
GET /v1/cookie-consent/configs/config-id
GET /v1/cookie-consent/configs/config-id
GET /v1/cookie-consent/configs/config-id
```

- [ ] **Step 4: Run focused tests and confirm RED**

Run:

```bash
mise exec -- go test ./provider -run 'TestCookieConsentPublication|TestPublishCookieConsent|TestCookieConsentScript' -count=1
```

Expected: publication types/functions/resource registration do not exist.

- [ ] **Step 5: Implement the resource contract and script derivation**

Add annotations for every input/output, including import ID, state-only delete, change-token behavior, public script outputs, and default preservation of discoveries. Record the schema default and retain a defensive check fallback:

```go
func (args *CookieConsentPublicationArgs) Annotate(a infer.Annotator) {
	a.SetDefault(&args.KeepUnclassifiedTattles, true)
}

if args.KeepUnclassifiedTattles == nil {
	keep := true
	args.KeepUnclassifiedTattles = &keep
}
```

Validate `webhookUrl` as absolute HTTP/HTTPS when set. Build the script only after both IDs are non-empty and escape each as a URL path segment:

```go
func cookieConsentScript(customerID, configID string) (string, string, error) {
	if strings.TrimSpace(customerID) == "" || strings.TrimSpace(configID) == "" {
		return "", "", errors.New("customerId and configId are required to build the CMP script")
	}
	src := fmt.Sprintf(
		"https://cmp.osano.com/%s/%s/osano.js",
		url.PathEscape(customerID), url.PathEscape(configID),
	)
	return src, `<script src="` + src + `"></script>`, nil
}
```

Register `infer.Resource(&CookieConsentPublication{})` in `Provider()` and update provider/package descriptions to mention both Cookie Consent and Unified Consent.

- [ ] **Step 6: Implement queueing, polling, completion proof, and deadlines**

Use these internal types:

```go
type jsonClient interface {
	DoJSON(context.Context, string, string, url.Values, any, any) error
}

type publicationBaseline struct {
	Status            string
	LastPublished     int
	PublishedRevision int
}

type publicationPollOptions struct {
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Timeout         time.Duration
	Sleep           func(context.Context, time.Duration) error
}
```

Production defaults are one-second initial polling, ten-second maximum polling, and twenty-minute provider fallback timeout. `withPublicationTimeout` must retain an earlier engine deadline.

Completion requires `PublishStatus == "published"` and at least one of:

```go
seenInProgress ||
baseline.Status != "published" ||
current.LastPublished > baseline.LastPublished ||
current.PublishedRevision > baseline.PublishedRevision
```

POST a body that always includes `keepUnclassifiedTattles` (including explicit `false`) and includes `description`/`webhookUrl` only when non-nil, because those optional strings are not nullable in Osano's OpenAPI contract. Treat `409` as accepted elsewhere and enter polling. Let the shared client retry `429`/`5xx`; wrap final exhaustion with the config ID. Poll `unpublished`, `outdated`, and `in-progress`; fail on `error` or unknown terminal status with the last metadata in the diagnostic. Every sleep selects on the operation context.

Create/update call the same helper, read only GETs and preserves prior inputs, import seeds the adoption token, and delete immediately returns success.

- [ ] **Step 7: Run focused, provider, and race tests**

Run:

```bash
mise exec -- gofmt -w provider/cookie_consent_publication.go provider/cookie_consent_publication_test.go provider/cookie_consent_test_helpers_test.go provider/provider.go
mise exec -- go test ./provider -run 'TestCookieConsentPublication|TestPublishCookieConsent|TestCookieConsentScript' -count=1
mise exec -- go test -race ./provider -count=1
```

Expected: exact tag, retry, conflict, stale-read, terminal error, import/read/delete, cancellation, and diff tests pass under race detection.

- [ ] **Step 8: Inspect scope and commit**

Run GitNexus `detect_changes({scope: "all"})`, review all reported CMP publication/config flows, then:

```bash
git add provider/cookie_consent_publication.go provider/cookie_consent_publication_test.go provider/cookie_consent_test_helpers_test.go provider/provider.go
git diff --cached --check
git commit -m "feat(cmp): publish configs and return install scripts"
```

### Task 5: Regenerate and verify schema and all SDKs

**Files:**
- Modify by generation: `provider/cmd/pulumi-resource-osano/schema.json`
- Modify by generation: `sdk/dotnet/**`
- Modify by generation: `sdk/go/**`
- Modify by generation: `sdk/java/**`
- Modify by generation: `sdk/nodejs/**`
- Modify by generation: `sdk/python/**`

**Interfaces:**
- Consumes: inferred Go resource contract from Tasks 1–4.
- Produces: idiomatic `CookieConsentPublication` classes/functions and new rule properties in each supported SDK.

- [ ] **Step 1: Generate artifacts from the provider**

Run:

```bash
mise exec -- make codegen
```

Expected: schema and five SDK directories regenerate successfully at version `1.0.0-alpha.0+dev`.

- [ ] **Step 2: Verify the schema contract exactly**

Run:

```bash
jq -e '
  .resources["osano:index:CookieConsentPublication"]
  | (.requiredInputs | index("configId") != null)
    and (.requiredInputs | index("changeToken") != null)
    and (.required | index("scriptSrc") != null)
    and (.required | index("scriptTag") != null)
' provider/cmd/pulumi-resource-osano/schema.json
jq -e '
  .resources["osano:index:CookieConsentRule"].inputProperties
  | has("ruleType") and has("description") and has("expiry")
' provider/cmd/pulumi-resource-osano/schema.json
```

Expected: both commands print `true` and exit zero. Inspect descriptions to ensure no API key is marked as an output and no generated script field is secret.

- [ ] **Step 3: Verify generated language surfaces**

Run:

```bash
rg -n "CookieConsentPublication|scriptSrc|scriptTag|changeToken" sdk/{dotnet,go,java,nodejs,python}
rg -n "ruleType|description|expiry" sdk/{dotnet,go,java,nodejs,python}
```

Expected: every SDK exposes publication and new rule fields; C# names are `CookieConsentPublication`, `ScriptSrc`, `ScriptTag`, and `ChangeToken`.

- [ ] **Step 4: Build every SDK**

Run:

```bash
mise exec -- make build_sdks
```

Expected: Go generation, Node TypeScript, Python packaging, .NET build, and Java Gradle build all succeed.

- [ ] **Step 5: Inspect generated scope and commit**

Run GitNexus `detect_changes({scope: "all"})`. Generated fan-out is expected across all SDK modules; investigate any hand-written provider file in this diff. Then:

```bash
git add provider/cmd/pulumi-resource-osano/schema.json sdk
git diff --cached --check
git commit -m "feat(sdk): generate cookie consent publication APIs"
```

### Task 6: Add canonical C# and companion TypeScript end-to-end examples

**Files:**
- Create: `examples/cookie-consent/csharp/Pulumi.yaml`
- Create: `examples/cookie-consent/csharp/CookieConsent.csproj`
- Create: `examples/cookie-consent/csharp/Program.cs`
- Create: `examples/cookie-consent/typescript/Pulumi.yaml`
- Create: `examples/cookie-consent/typescript/package.json`
- Create: `examples/cookie-consent/typescript/yarn.lock`
- Create: `examples/cookie-consent/typescript/tsconfig.json`
- Create: `examples/cookie-consent/typescript/index.ts`
- Create: `examples/cookie-consent/README.md`
- Modify: `Makefile`

**Interfaces:**
- Consumes: generated C# and Node.js SDK resource names from Task 5.
- Produces: `cookieConsentScriptSrc` and `cookieConsentScriptTag` stack outputs in both examples.
- Produces: `build_examples` and `build_cookie_consent_examples` Make targets that compile only and never contact Osano.

- [ ] **Step 1: Run pre-edit impact analysis**

The example files are new. Run GitNexus upstream impact for the existing `build_sdks` Make target before adding dependent targets. Warn on HIGH/CRITICAL risk.

- [ ] **Step 2: Create the failing C# project shell and build target**

Use this project file:

```xml
<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
    <Nullable>enable</Nullable>
    <ImplicitUsings>enable</ImplicitUsings>
  </PropertyGroup>
  <ItemGroup>
    <ProjectReference Include="../../../sdk/dotnet/Community.Pulumi.Osano.csproj" />
    <PackageReference Include="Pulumi" Version="[3.76.1,4.0.0)" />
  </ItemGroup>
</Project>
```

Create `Pulumi.yaml` with `name: osano-cookie-consent-csharp`, `runtime: dotnet`, and a description that says it creates, publishes, and exports a hosted Osano CMP script. Add Make targets:

```make
.PHONY: build_cookie_consent_examples build_examples
build_cookie_consent_examples: dotnet_sdk nodejs_sdk
	dotnet build examples/cookie-consent/csharp/CookieConsent.csproj
	cd examples/cookie-consent/typescript && yarn install --frozen-lockfile && yarn run tsc --noEmit

build_examples: build_cookie_consent_examples
```

Before creating `Program.cs`, run `mise exec -- dotnet build examples/cookie-consent/csharp/CookieConsent.csproj`. Confirm RED with compiler error `CS5001` (no suitable entry point), while NuGet and the local provider project reference restore successfully.

- [ ] **Step 3: Implement deterministic C# desired state and hash**

The C# program must synchronously load non-secret site inputs, use a `SortedDictionary<string, object>` for CMP configuration, and hash the exact desired config/rule descriptor:

```csharp
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using Community.Pulumi.Osano;
using Pulumi;

return await Deployment.RunAsync(() =>
{
    var settings = new Pulumi.Config();
    var domain = settings.Require("domain");
    var storagePolicyHref = settings.Require("storagePolicyHref");
    var mode = settings.Get("mode") ?? "permissive";

    var cmpSettings = new SortedDictionary<string, object>
    {
        ["managePreferencesEnabled"] = true,
        ["storagePolicyHref"] = storagePolicyHref,
    };
    var ruleDefinitions = new[]
    {
        new
        {
            Name = "google-analytics-cookie",
            StoreType = "cookies",
            Classification = "ANALYTICS",
            Rule = "_ga",
            Disclosure = true,
            Title = "Google Analytics",
            VendorName = "Google",
            RuleType = "EXACT_MATCH",
            Description = "Measures site usage.",
            Expiry = "2 years",
        },
    };
    var publishDescriptor = new
    {
        Name = "pulumi-cookie-consent",
        Domains = new[] { domain },
        Mode = mode,
        Configuration = cmpSettings,
        Rules = ruleDefinitions,
    };
    var changeToken = Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(
        JsonSerializer.Serialize(publishDescriptor)
    ))).ToLowerInvariant();
```

Finish the program with generated resource types:

```csharp
    var consentConfig = new CookieConsentConfig("cookie-consent", new()
    {
        Name = publishDescriptor.Name,
        Domains = { domain },
        Mode = mode,
        Configuration =
        {
            { "managePreferencesEnabled", true },
            { "storagePolicyHref", storagePolicyHref },
        },
    });

    var rules = ruleDefinitions.Select(definition => new CookieConsentRule(definition.Name, new()
    {
        ConfigId = consentConfig.ConfigId,
        StoreType = definition.StoreType,
        Classification = definition.Classification,
        Rule = definition.Rule,
        Disclosure = definition.Disclosure,
        Title = definition.Title,
        VendorName = definition.VendorName,
        RuleType = definition.RuleType,
        Description = definition.Description,
        Expiry = definition.Expiry,
    })).ToArray();

    var publicationOptions = new CustomResourceOptions
    {
        CustomTimeouts = new CustomTimeouts
        {
            Create = TimeSpan.FromMinutes(20),
            Update = TimeSpan.FromMinutes(20),
        },
    };
    publicationOptions.DependsOn.Add(consentConfig);
    foreach (var rule in rules)
    {
        publicationOptions.DependsOn.Add(rule);
    }
    var publication = new CookieConsentPublication("publication", new()
    {
        ConfigId = consentConfig.ConfigId,
        ChangeToken = changeToken,
        KeepUnclassifiedTattles = true,
    }, publicationOptions);

    return new Dictionary<string, object?>
    {
        ["cookieConsentScriptSrc"] = publication.ScriptSrc,
        ["cookieConsentScriptTag"] = publication.ScriptTag,
    };
});
```

- [ ] **Step 4: Implement the repository-policy TypeScript companion**

Use Node's `createHash("sha256")` over `JSON.stringify(publishDescriptor)` and the same desired values. Its publication must be:

```ts
const publication = new osano.CookieConsentPublication("publication", {
    configId: consentConfig.configId,
    changeToken,
    keepUnclassifiedTattles: true,
}, {
    dependsOn: [consentConfig, ...rules],
    customTimeouts: { create: "20m", update: "20m" },
});

export const cookieConsentScriptSrc = publication.scriptSrc;
export const cookieConsentScriptTag = publication.scriptTag;
```

Reference `@jflavan/pulumi-osano` as `file:../../../sdk/nodejs/bin`, follow the existing strict TypeScript settings, and add `@types/node` so the crypto import is typed. Run `yarn install` once to create and commit `yarn.lock`; subsequent Make verification uses `--frozen-lockfile`.

- [ ] **Step 5: Write the end-to-end README and opt-in live smoke path**

Document both clone-local and released NuGet workflows, with C# first. Include these exact stack settings:

```bash
export OSANO_API_KEY="replace-with-a-customer-rest-api-key"
pulumi config set domain example.com
pulumi config set storagePolicyHref https://example.com/privacy/cookies
pulumi config set mode permissive
pulumi up
pulumi stack output cookieConsentScriptTag
```

Explain that `pulumi preview` and compilation never publish; `pulumi up` creates the config/rules, then queues publication; unchanged updates do not publish. State that a live smoke test creates and publishes real customer resources, Osano/CDN propagation may continue for up to 15 minutes after the API reports published, and destroy leaves the config/publication active while deleting managed rules. Show the returned tag with no `async`/`defer` and require placing it first in the site `<head>`.

- [ ] **Step 6: Compile both examples and confirm GREEN**

Run:

```bash
mise exec -- dotnet build examples/cookie-consent/csharp/CookieConsent.csproj
cd examples/cookie-consent/typescript && mise exec -- yarn install && mise exec -- yarn run tsc --noEmit
cd ../../.. && mise exec -- make build_cookie_consent_examples
```

Expected: both examples compile against local generated SDKs; no Osano HTTP request occurs.

- [ ] **Step 7: Inspect scope and commit**

Run GitNexus `detect_changes({scope: "all"})`, verify only example/build surfaces are new, then:

```bash
git add Makefile examples/cookie-consent
git diff --cached --check
git commit -m "docs(examples): add cookie consent deployment walkthroughs"
```

### Task 7: Update every capability and lifecycle document

**Files:**
- Modify: `README.md`
- Modify: `EXAMPLES.md`
- Modify: `examples/README.md`
- Modify: `docs/IMPORTING.md`
- Modify: `docs/faq.md`
- Modify: `docs/state-management.md`
- Modify: `docs/troubleshooting.md`
- Modify: `docs/UPGRADE.md`
- Modify: `docs/RELEASE_CHECKLIST.md`
- Modify: `docs/RELEASE_GUIDE.md`
- Modify: `CONTRIBUTING.md`
- Modify after README update by generation: `sdk/dotnet/README.md`
- Modify after README update by generation: `sdk/nodejs/README.md`
- Modify after README update by generation: `sdk/python/README.md`

**Interfaces:**
- Consumes: final public resource/schema/example behavior.
- Produces: one consistent user story from authentication through site installation, refresh/import, troubleshooting, upgrade, and release validation.

- [ ] **Step 1: Inventory stale statements before editing**

Run:

```bash
rg -n "future work|future roadmap|Managed configuration|optional today|subject profile routes|Cookie Consent|cookie consent|publish|import" README.md EXAMPLES.md CONTRIBUTING.md examples docs
```

Record every stale capability/authentication/import statement so it is either corrected or deliberately retained with current wording.

- [ ] **Step 2: Update the primary README and example indexes**

Add Cookie Consent to the opening capability list before Unified Consent details. Add a concise C# end-to-end section that links to `examples/cookie-consent`, shows the exact `CookieConsentPublication` outputs, and states `OSANO_API_KEY` is required. Correct the authentication table so the Osano key covers Customer REST CMP configuration/rules/publication plus administrative routes.

In `EXAMPLES.md`, retain TypeScript as baseline policy but identify C# as the canonical language for the Cookie Consent workflow and add the new TypeScript/C# coverage row. In `examples/README.md`, list both quickstart and Cookie Consent, and separate their credential/config requirements. Update `CONTRIBUTING.md` so `OSANO_API_KEY` is described as a Customer REST/CMP key and `make build_cookie_consent_examples` is part of resource-change validation.

Link directly to the authoritative references used by the implementation:

- `https://developers.osano.com/customer-rest-api`
- `https://developers.osano.com/cmp/javascript-api/developer-documentation-consent-javascript-api`
- `https://docs.osano.com/hc/en-us/articles/24425173212308-Publish-or-Republish-Cookie-Consent`
- `https://www.pulumi.com/docs/iac/concepts/resources/options/dependson/`
- `https://www.pulumi.com/docs/iac/concepts/resources/options/customtimeouts/`

- [ ] **Step 3: Rewrite import, state, FAQ, and troubleshooting lifecycle guidance**

`docs/IMPORTING.md` must contain these exact formats:

```bash
pulumi import osano:index:CookieConsentConfig consentConfig <configId>
pulumi import osano:index:CookieConsentRule analyticsRule <configId>/<ruleId>
pulumi import osano:index:CookieConsentPublication publication <configId>
```

Explain that config/publication import does not publish, publication adopts `import:<lastPublished>:<publishedRevision>`, and applying the program's intended token may cause one republish. Retain the statement that immutable Unified Consent records are not importable.

Update state/FAQ content to explain token-driven republishing, refresh exposing `outdated` without mutating, config/publication state-only deletion, rule deletion, API keys in encrypted config, and public script outputs. Remove all claims that managed Cookie Consent is future work.

Troubleshooting must have separate sections for missing Customer API key, publication `409`, `429`, terminal `error`, timeout/cancellation, persistent `outdated`, and delayed CDN propagation. Recommend a twenty-minute custom timeout and never recommend repeated immediate publish attempts.

- [ ] **Step 4: Update upgrade and release gates**

Document additive `CookieConsentPublication`, rule fields, environment/timeout fixes, composite rule imports, and no-op upstream delete in `docs/UPGRADE.md`.

Add these release checks to both release documents:

```bash
make codegen
make test_provider
make build_sdks
make build_cookie_consent_examples
git diff --exit-code
```

Require release notes to call out `scriptSrc`, `scriptTag`, `changeToken`, preservation of discoveries, composite rule imports, and retained configs/publications on destroy.

- [ ] **Step 5: Validate links, stale-text removal, and formatting**

Run:

```bash
rg -n "future work|future roadmap|Managed configuration resources are still" README.md EXAMPLES.md examples docs
rg -n "CookieConsentPublication|scriptSrc|scriptTag|changeToken|OSANO_API_KEY" README.md EXAMPLES.md examples docs
git diff --check
```

Expected: the stale-text command returns no matches; the lifecycle terms appear in primary, example, import/state/troubleshooting/upgrade/release documentation; Markdown has no whitespace errors.

- [ ] **Step 6: Propagate the final root README into generated packages**

Run:

```bash
mise exec -- make codegen
git diff --exit-code -- provider/cmd/pulumi-resource-osano/schema.json sdk ':!sdk/dotnet/README.md' ':!sdk/nodejs/README.md' ':!sdk/python/README.md'
```

Expected: only `sdk/dotnet/README.md`, `sdk/nodejs/README.md`, and `sdk/python/README.md` change, each matching the final root README copied by generation.

- [ ] **Step 7: Inspect scope and commit**

Run GitNexus `detect_changes({scope: "all"})`, confirm this commit changes documentation only, then:

```bash
git add README.md EXAMPLES.md CONTRIBUTING.md examples/README.md docs/IMPORTING.md docs/faq.md docs/state-management.md docs/troubleshooting.md docs/UPGRADE.md docs/RELEASE_CHECKLIST.md docs/RELEASE_GUIDE.md sdk/dotnet/README.md sdk/nodejs/README.md sdk/python/README.md
git diff --cached --check
git commit -m "docs(cmp): document publication and script lifecycle"
```

### Task 8: Run final end-to-end verification and consistency review

**Files:**
- Modify only if verification exposes a defect: files from Tasks 1–7.

**Interfaces:**
- Consumes: complete provider, generated artifacts, examples, and docs.
- Produces: evidence that the accepted end-to-end workflow is buildable and deterministic without live credentials.

- [ ] **Step 1: Run formatting, static checks, and provider tests from a clean command context**

Run:

```bash
mise exec -- gofmt -w $(find provider tests -name '*.go')
mise exec -- make lint
mise exec -- make test_provider
```

Expected: lint succeeds and the race-enabled provider suite passes with mocked HTTP only.

- [ ] **Step 2: Regenerate and prove artifact consistency**

Run:

```bash
mise exec -- make codegen
git diff --exit-code -- provider/cmd/pulumi-resource-osano/schema.json sdk
```

Expected: generation produces no diff, proving schema/SDK files are current.

- [ ] **Step 3: Build provider, SDKs, and examples**

Run:

```bash
mise exec -- make provider
mise exec -- make build_sdks
mise exec -- make build_cookie_consent_examples
```

Expected: provider and all five SDKs build; C# and TypeScript Cookie Consent examples compile; no external Osano operation occurs.

- [ ] **Step 4: Assert the public contract from generated artifacts**

Run:

```bash
jq -r '.resources["osano:index:CookieConsentPublication"].properties.scriptTag.description' provider/cmd/pulumi-resource-osano/schema.json
rg -n 'https://cmp\.osano\.com/.+/osano\.js|<script src=' provider examples README.md docs
rg -n 'async|defer' examples/cookie-consent README.md docs/faq.md
```

Expected: schema describes the full script tag; examples/docs show the correct hosted URL; any `async`/`defer` match is an explicit warning not to use those attributes.

- [ ] **Step 5: Review the complete branch with GitNexus and Git**

Run GitNexus `detect_changes({scope: "compare", base_ref: "main"})` from the implementation worktree, review every affected process, and run:

```bash
git status --short
git diff --check main...HEAD
git diff --stat main...HEAD
git log --oneline main..HEAD
```

Expected: only planned provider/generated/example/doc files differ; no `.claude/`, `AGENTS.md`, `CLAUDE.md`, credential file, Pulumi stack state, or build output is staged/tracked.

- [ ] **Step 6: Fix any verification defect test-first, then make the final verification commit only if needed**

For each defect, add or tighten the smallest failing test, reproduce it, patch the owning file, and rerun the narrow test before repeating Steps 1–5. Before a corrective commit, run GitNexus impact for each edited existing symbol and `detect_changes({scope: "all"})`. Use a scoped commit message describing the actual correction; if verification produces no source change, do not create an empty commit.

- [ ] **Step 7: Prepare the completion report**

Report:

- the exact new resource and script outputs;
- how C# computes and uses `changeToken` and `dependsOn`;
- publication polling/retry/delete/import semantics;
- config/rule/client correctness fixes;
- tests/build commands and their observed results;
- whether live Osano publication was skipped due to absent credentials;
- any residual operational caveat, especially CDN propagation and retained upstream config/publication state.
