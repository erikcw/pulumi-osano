package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/blang/semver"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func TestCookieConsentPublicationProviderPublishes(t *testing.T) {
	for _, op := range []string{"create", "update"} {
		t.Run(op, func(t *testing.T) {
			var requests []string
			getCount := 0
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				assertCMPRequest(t, r, r.Method, r.URL.Path)
				if r.Method == http.MethodPost {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				getCount++
				if getCount == 1 {
					writePublicationConfigResponse(t, w, "outdated", 100, 3)
					return
				}
				writePublicationConfigResponse(t, w, "published", 200, 4)
			}))
			defer api.Close()

			server := newCMPProviderServer(t, api.URL)
			var properties property.Map
			if op == "create" {
				resp, err := server.Create(p.CreateRequest{
					Urn:        cmpURN("CookieConsentPublication", "created"),
					Properties: publicationInputProperties(),
				})
				if err != nil {
					t.Fatal(err)
				}
				if resp.ID != "config-id" {
					t.Fatalf("expected publication ID config-id, got %q", resp.ID)
				}
				properties = resp.Properties
			} else {
				resp, err := server.Update(p.UpdateRequest{
					ID:     "config-id",
					Urn:    cmpURN("CookieConsentPublication", "updated"),
					State:  publicationStateProperties(),
					Inputs: publicationInputProperties().Set("changeToken", property.New("desired-state-v2")),
				})
				if err != nil {
					t.Fatal(err)
				}
				properties = resp.Properties
			}

			token := "desired-state-v1"
			if op == "update" {
				token = "desired-state-v2"
			}
			assertPublicationProperties(t, properties, token, 200, 4)
			want := []string{
				"GET /v1/cookie-consent/configs/config-id",
				"POST /v1/cookie-consent/configs/config-id/publish",
				"GET /v1/cookie-consent/configs/config-id",
			}
			if strings.Join(requests, "|") != strings.Join(want, "|") {
				t.Fatalf("unexpected requests: %v", requests)
			}
		})
	}
}

func TestCookieConsentResourcesRequireCustomerAPIKey(t *testing.T) {
	for _, env := range []string{envOsanoAPIKey, envUnifiedConsent, envAPIBaseURL, envRequestTimeout} {
		t.Setenv(env, "")
	}
	server, err := integration.NewServer(
		t.Context(), Name, semver.MustParse("1.0.0"), integration.WithProvider(Provider()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Configure(p.ConfigureRequest{Args: property.NewMap(map[string]property.Value{})}); err != nil {
		t.Fatal(err)
	}

	calls := map[string]func() error{
		"config create": func() error {
			_, err := server.Create(p.CreateRequest{
				Urn: cmpURN("CookieConsentConfig", "no-key"), Properties: configInputPropertiesForErrors(),
			})
			return err
		},
		"publication create": func() error {
			_, err := server.Create(p.CreateRequest{
				Urn: cmpURN("CookieConsentPublication", "no-key"), Properties: publicationInputProperties(),
			})
			return err
		},
		"publication read": func() error {
			_, err := server.Read(p.ReadRequest{
				ID: "config-id", Urn: cmpURN("CookieConsentPublication", "no-key"),
				Properties: publicationStateProperties(), Inputs: publicationInputProperties(),
			})
			return err
		},
		"rule delete": func() error {
			return server.Delete(p.DeleteRequest{
				ID: "config-abc/42", Urn: cmpURN("CookieConsentRule", "no-key"), Properties: ruleStateProperties(),
			})
		},
	}
	for name, call := range calls {
		err := call()
		if err == nil || !strings.Contains(err.Error(), "OSANO_API_KEY") {
			t.Fatalf("%s: expected missing-key diagnostic, got %v", name, err)
		}
	}
}

func TestCookieConsentAPIErrorsPropagate(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"invalid"}`))
	}))
	defer api.Close()
	server := newCMPProviderServer(t, api.URL)

	if _, err := server.Create(p.CreateRequest{
		Urn: cmpURN("CookieConsentConfig", "bad"), Properties: configInputPropertiesForErrors(),
	}); err == nil || !strings.Contains(err.Error(), "422") {
		t.Fatalf("expected config create 422 error, got %v", err)
	}
	if _, err := server.Update(p.UpdateRequest{
		ID: "config-id", Urn: cmpURN("CookieConsentConfig", "bad"),
		State:  configInputPropertiesForErrors().Set("configId", property.New("config-id")),
		Inputs: configInputPropertiesForErrors(),
	}); err == nil || !strings.Contains(err.Error(), "422") {
		t.Fatalf("expected config update 422 error, got %v", err)
	}
	if _, err := server.Create(p.CreateRequest{
		Urn: cmpURN("CookieConsentRule", "bad"), Properties: ruleInputProperties(),
	}); err == nil || !strings.Contains(err.Error(), "create rule") {
		t.Fatalf("expected rule create error, got %v", err)
	}
	if _, err := server.Update(p.UpdateRequest{
		ID: "config-abc/42", Urn: cmpURN("CookieConsentRule", "bad"),
		State: ruleStateProperties(), Inputs: ruleInputProperties(),
	}); err == nil || !strings.Contains(err.Error(), "update rule") {
		t.Fatalf("expected rule update error, got %v", err)
	}
	if _, err := server.Read(p.ReadRequest{
		ID: "config-abc/42", Urn: cmpURN("CookieConsentRule", "bad"),
		Properties: ruleStateProperties(), Inputs: ruleInputProperties(),
	}); err == nil || !strings.Contains(err.Error(), "read rule") {
		t.Fatalf("expected rule read error, got %v", err)
	}
	if _, err := server.Read(p.ReadRequest{
		ID: "config-id", Urn: cmpURN("CookieConsentPublication", "bad"),
		Properties: publicationStateProperties(), Inputs: publicationInputProperties(),
	}); err == nil || !strings.Contains(err.Error(), "read Cookie Consent publication") {
		t.Fatalf("expected publication read error, got %v", err)
	}
}

func TestCookieConsentRuleCreateRejectsEmptyResponse(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeCMPRulesListResponse(t, w, cmpRulesListResponse{})
	}))
	defer api.Close()

	server := newCMPProviderServer(t, api.URL)
	_, err := server.Create(p.CreateRequest{Urn: cmpURN("CookieConsentRule", "empty"), Properties: ruleInputProperties()})
	if err == nil || !strings.Contains(err.Error(), "empty items") {
		t.Fatalf("expected empty items error, got %v", err)
	}
}

func TestRuleStoreType(t *testing.T) {
	t.Parallel()
	for responseType, want := range map[string]string{
		"cookie": "cookies", "script": "scripts", "iframe": "iframes", "localStorage": "localStorage",
	} {
		got, err := ruleStoreType(responseType)
		if err != nil || got != want {
			t.Fatalf("ruleStoreType(%q) = %q, %v; want %q", responseType, got, err, want)
		}
	}
	if _, err := ruleStoreType("pixel"); err == nil {
		t.Fatal("expected unsupported rule type error")
	}
}

func TestPublicationPollHelpers(t *testing.T) {
	t.Parallel()

	defaults := defaultPublicationPollOptions(context.Background())
	if defaults.InitialInterval != time.Second || defaults.MaxInterval != 10*time.Second ||
		defaults.Timeout != 20*time.Minute || defaults.Sleep == nil {
		t.Fatalf("unexpected default poll options: %#v", defaults)
	}

	normalized := normalizePublicationPollOptions(publicationPollOptions{})
	if normalized.Timeout != defaultPublicationTimeout ||
		normalized.InitialInterval != defaultPublicationInitialInterval ||
		normalized.MaxInterval != defaultPublicationMaxInterval || normalized.Sleep == nil {
		t.Fatalf("expected zero options to normalize to defaults, got %#v", normalized)
	}
	clamped := normalizePublicationPollOptions(publicationPollOptions{
		InitialInterval: time.Minute, MaxInterval: time.Second, Timeout: time.Second,
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if clamped.InitialInterval != time.Second {
		t.Fatalf("expected initial interval clamped to max, got %s", clamped.InitialInterval)
	}

	for _, tc := range []struct{ current, maximum, want time.Duration }{
		{time.Second, 10 * time.Second, 2 * time.Second},
		{4 * time.Second, 10 * time.Second, 8 * time.Second},
		{8 * time.Second, 10 * time.Second, 10 * time.Second},
		{10 * time.Second, 10 * time.Second, 10 * time.Second},
		{0, 10 * time.Second, 0},
		{time.Second, 0, time.Second},
	} {
		if got := nextPublicationInterval(tc.current, tc.maximum); got != tc.want {
			t.Fatalf("nextPublicationInterval(%s, %s) = %s, want %s", tc.current, tc.maximum, got, tc.want)
		}
	}

	if err := sleepForPublication(context.Background(), 0); err != nil {
		t.Fatalf("zero sleep: %v", err)
	}
	if err := sleepForPublication(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("short sleep: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepForPublication(cancelled, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation for zero sleep, got %v", err)
	}
	if err := sleepForPublication(cancelled, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation for long sleep, got %v", err)
	}
}

func TestUnifiedConsentInvokeErrors(t *testing.T) {
	invokes := map[string]map[string]property.Value{
		"getUnifiedConsent": {"subjectRef": property.New("s")},
		"getSubject":        {"subjectRef": property.New("s")},
		"getConfig":         {},
		"getCollections":    {},
		"getCollection":     {"collectionId": property.New("c")},
		"checkConsent":      {"subjectId": property.New("s")},
		"getConsentProfile": {"hashedSubjectId": property.New("h"), "configId": property.New("c")},
		"sendSubjectCode":   {"hashedSubjectId": property.New("h"), "email": property.New("a@example.com")},
		"verifySubjectCode": {
			"hashedSubjectId": property.New("h"), "email": property.New("a@example.com"), "code": property.New("1"),
		},
	}

	for _, tc := range []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"server error", http.StatusInternalServerError, `{"message":"boom"}`, "status=500"},
		{"malformed response", http.StatusOK, `{not json`, "decode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				status := tc.status
				if status == http.StatusOK && r.Method == http.MethodPost && r.URL.Path == "/v2/subjects/send-code" {
					// sendSubjectCode ignores the response body, so only the error status applies to it.
					status = http.StatusInternalServerError
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer api.Close()

			server := newUCProviderServer(t, api.URL)
			for token, args := range invokes {
				_, err := server.Invoke(p.InvokeRequest{
					Token: tokens.Type("osano:index:" + token), Args: property.NewMap(args),
				})
				want := tc.wantErr
				if token == "sendSubjectCode" {
					want = "status=500"
				}
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("%s: expected error containing %q, got %v", token, want, err)
				}
			}
		})
	}
}

func TestUnifiedConsentInvokesValidateRequiredInputs(t *testing.T) {
	server := newUCProviderServer(t, "http://127.0.0.1:1")
	for token, args := range map[string]map[string]property.Value{
		"getUnifiedConsent": {"subjectRef": property.New(" ")},
		"getSubject":        {"subjectRef": property.New(" ")},
		"getCollection":     {"collectionId": property.New(" ")},
		"checkConsent":      {"subjectId": property.New(" ")},
		"getConsentProfile": {"hashedSubjectId": property.New("h"), "configId": property.New(" ")},
		"sendSubjectCode":   {"hashedSubjectId": property.New("h")},
		"verifySubjectCode": {
			"hashedSubjectId": property.New("h"), "email": property.New("a@example.com"), "code": property.New(" "),
		},
	} {
		_, err := server.Invoke(p.InvokeRequest{Token: tokens.Type("osano:index:" + token), Args: property.NewMap(args)})
		if err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("%s: expected required-input error, got %v", token, err)
		}
	}
}

func TestConsentResourceErrorsAndDelete(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer api.Close()
	server := newUCProviderServer(t, api.URL)

	inputs := property.NewMap(map[string]property.Value{
		"subject": property.New(map[string]property.Value{"anonymousId": property.New("anon-1")}),
		"actions": property.New([]property.Value{property.New(map[string]property.Value{
			"target": property.New("t"), "vendor": property.New("v"), "action": property.New("ACCEPT"),
		})}),
	})
	if _, err := server.Create(p.CreateRequest{Urn: cmpURN("Consent", "bad"), Properties: inputs}); err == nil ||
		!strings.Contains(err.Error(), "status=500") {
		t.Fatalf("expected consent create error, got %v", err)
	}
	state := inputs.Set("consentId", property.New("consent-1")).Set("lastSynced", property.New(""))
	if _, err := server.Read(p.ReadRequest{
		ID: "consent-1", Urn: cmpURN("Consent", "bad"), Properties: state, Inputs: inputs,
	}); err == nil || !strings.Contains(err.Error(), "status=500") {
		t.Fatalf("expected consent read error, got %v", err)
	}
	err := server.Delete(p.DeleteRequest{ID: "consent-1", Urn: cmpURN("Consent", "gone"), Properties: state})
	if err != nil {
		t.Fatalf("consent delete must be state-only, got %v", err)
	}
}

func configInputPropertiesForErrors() property.Map {
	return property.NewMap(map[string]property.Value{
		"name":    property.New("cookie-consent"),
		"domains": property.New([]property.Value{property.New("example.com")}),
		"mode":    property.New("debug"),
		"configuration": property.New(map[string]property.Value{
			"storagePolicyHref": property.New("https://example.com/storage-policy"),
		}),
	})
}

func TestUpdatePreviewsKeepIdentityOutputsKnown(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("preview must not call Osano: %s %s", r.Method, r.URL.Path)
	}))
	defer api.Close()
	server := newCMPProviderServer(t, api.URL)

	configState := configInputPropertiesForErrors().
		Set("configId", property.New("config-id")).
		Set("customerId", property.New("customer-id")).
		Set("created", property.New(1.0)).
		Set("updated", property.New(1.0))
	configResp, err := server.Update(p.UpdateRequest{
		ID: "config-id", Urn: cmpURN("CookieConsentConfig", "renamed"), State: configState,
		Inputs: configInputPropertiesForErrors().Set("name", property.New("renamed")), DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertKnownString(t, configResp.Properties, "configId", "config-id")
	assertKnownString(t, configResp.Properties, "customerId", "customer-id")
	assertKnownString(t, configResp.Properties, "name", "renamed")
	if !configResp.Properties.Get("updated").IsComputed() {
		t.Fatalf("expected server metadata to be unknown after an input change, got %#v",
			configResp.Properties.Get("updated"))
	}

	ruleResp, err := server.Update(p.UpdateRequest{
		ID: "config-abc/42", Urn: cmpURN("CookieConsentRule", "retitled"), State: ruleStateProperties(),
		Inputs: ruleInputProperties().Set("title", property.New("Retitled")), DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := ruleResp.Properties.Get("ruleId"); got.IsComputed() || got.AsNumber() != 42 {
		t.Fatalf("expected ruleId to stay known as 42, got %#v", got)
	}

	publicationResp, err := server.Update(p.UpdateRequest{
		ID: "config-id", Urn: cmpURN("CookieConsentPublication", "republished"), State: publicationStateProperties(),
		Inputs: publicationInputProperties().Set("changeToken", property.New("desired-state-v2")), DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertKnownString(t, publicationResp.Properties, "customerId", "customer-id")
	assertKnownString(t, publicationResp.Properties, "scriptSrc", "https://cmp.osano.com/customer-id/config-id/osano.js")
	if !publicationResp.Properties.Get("lastPublished").IsComputed() {
		t.Fatal("expected lastPublished to be unknown before republishing")
	}
}

func TestCookieConsentConfigUpdateWithEmptyResponseKeepsState(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertCMPRequest(t, r, http.MethodPatch, "/v1/cookie-consent/configs/config-id")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer api.Close()

	server := newCMPProviderServer(t, api.URL)
	resp, err := server.Update(p.UpdateRequest{
		ID: "config-id", Urn: cmpURN("CookieConsentConfig", "empty-patch"),
		State:  configInputPropertiesForErrors().Set("configId", property.New("config-id")),
		Inputs: configInputPropertiesForErrors().Set("name", property.New("renamed")),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertKnownString(t, resp.Properties, "configId", "config-id")
	assertKnownString(t, resp.Properties, "name", "renamed")
}

func TestConsentCheckDefersUnknownInputs(t *testing.T) {
	server := newUCProviderServer(t, "http://127.0.0.1:1")
	action := func(vendor property.Value) property.Value {
		return property.New([]property.Value{property.New(map[string]property.Value{
			"target": property.New("t"), "vendor": vendor, "action": property.New("ACCEPT"),
		})})
	}
	inputs := func(actions property.Value) property.Map {
		return property.NewMap(map[string]property.Value{
			"subject": property.New(map[string]property.Value{"verifiedId": property.New("user-1")}),
			"actions": actions,
		})
	}

	resp, err := server.Check(p.CheckRequest{
		Urn: cmpURN("Consent", "computed"), Inputs: inputs(action(property.New(property.Computed))),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Failures) != 0 {
		t.Fatalf("an unknown vendor must not fail Check: %#v", resp.Failures)
	}
	if _, err := server.Create(p.CreateRequest{
		Urn: cmpURN("Consent", "computed"), Properties: inputs(action(property.New(property.Computed))), DryRun: true,
	}); err != nil {
		t.Fatalf("preview with an unknown vendor must succeed, got %v", err)
	}

	resp, err = server.Check(p.CheckRequest{Urn: cmpURN("Consent", "invalid"), Inputs: inputs(action(property.New("")))})
	if err != nil {
		t.Fatal(err)
	}
	assertFailureProperty(t, resp.Failures, "actions")

	resp, err = server.Check(p.CheckRequest{
		Urn: cmpURN("Consent", "no-subject"),
		Inputs: property.NewMap(map[string]property.Value{
			"subject": property.New(map[string]property.Value{}), "actions": action(property.New("v")),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFailureProperty(t, resp.Failures, "subject")
}

func assertKnownString(t *testing.T, m property.Map, key, want string) {
	t.Helper()
	got := m.Get(key)
	if got.IsComputed() || !got.IsString() || got.AsString() != want {
		t.Fatalf("expected known %s=%q, got %#v", key, want, got)
	}
}

func TestCookieConsentConfigDiffTreatsEmptyListsAsUnset(t *testing.T) {
	t.Parallel()
	state := baseConfigState()
	state.OrgIDs = []string{}
	inputs := baseConfigArgs()
	inputs.OrgIDs = nil
	resp, err := (&CookieConsentConfig{}).Diff(context.Background(),
		infer.DiffRequest[CookieConsentConfigArgs, CookieConsentConfigState]{State: state, Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if resp.HasChanges {
		t.Fatalf("an echoed empty orgIds list must not produce a diff: %#v", resp.DetailedDiff)
	}
}
