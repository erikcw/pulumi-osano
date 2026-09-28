package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	osanoclient "github.com/jflavan/pulumi-osano/provider/internal/osano"
)

// Only a 404 means "not found". Any other failure, such as an expired key or an outage, must fail
// the lookup: reporting exists=false would let a site be rendered without its consent script.
func TestCookieConsentFunctionsPropagateAPIErrors(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("getCookieConsentConfig %d", status), func(t *testing.T) {
			_, _, err := invokeCMP(t, map[string]func(*http.Request) (int, any){
				"/v1/cookie-consent/configs/config-123": respondJSON(status, map[string]any{"message": "no"}),
			}, "getCookieConsentConfig", map[string]property.Value{"configId": property.New("config-123")})
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("status=%d", status)) ||
				!strings.Contains(err.Error(), "read Cookie Consent config") {
				t.Fatalf("expected the %d to fail the lookup, got %v", status, err)
			}
		})
	}

	withConfig := map[string]property.Value{"configId": property.New("config-123")}
	for _, tc := range []struct {
		token, path, prefix string
		args                map[string]property.Value
	}{
		{
			token: "getCookieConsentConfigs", path: "/v1/cookie-consent/configs",
			prefix: "list Cookie Consent configs", args: map[string]property.Value{},
		},
		{
			token: "getCookieConsentRules", path: "/v1/cookie-consent/configs/config-123/rules",
			prefix: "list rules of Cookie Consent config", args: withConfig,
		},
		{
			token: "getCookieConsentDiscoveries", path: "/v1/cookie-consent/configs/config-123/discoveries",
			prefix: "list discoveries of Cookie Consent config", args: withConfig,
		},
		{
			token: "getCookieConsentAuditLog", path: "/v1/cookie-consent/audit-log",
			prefix: "query Cookie Consent audit log", args: map[string]property.Value{},
		},
	} {
		t.Run(tc.token+" 500", func(t *testing.T) {
			_, _, err := invokeCMP(t, map[string]func(*http.Request) (int, any){
				tc.path: respondJSON(http.StatusInternalServerError, map[string]any{"message": "boom"}),
			}, tc.token, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.prefix) || !strings.Contains(err.Error(), "status=500") {
				t.Fatalf("expected a wrapped 500 error, got %v", err)
			}
		})
	}
}

func TestGetCookieConsentAuditLogDefaultCap(t *testing.T) {
	events := make([]any, 0, cookieConsentAuditLogPageSize)
	for i := range cookieConsentAuditLogPageSize {
		events = append(events, map[string]any{"id": fmt.Sprintf("e%d", i), "eventType": "cmp.configUpdated"})
	}
	resp, requests, err := invokeCMP(t, map[string]func(*http.Request) (int, any){
		"/v1/cookie-consent/audit-log": respondJSON(http.StatusOK, map[string]any{"items": events, "next": "more"}),
	}, "getCookieConsentAuditLog", map[string]property.Value{})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 {
		t.Fatalf("the default cap must stop after one full page, got %d requests", len(requests))
	}
	if got := resp.Return.Get("events").AsArray().Len(); got != cookieConsentAuditLogPageSize {
		t.Fatalf("expected %d events, got %d", cookieConsentAuditLogPageSize, got)
	}
}

func TestGetCookieConsentRulesMaxResults(t *testing.T) {
	rules := make([]any, 0, 3)
	for i := range 3 {
		fixture := cmpRuleResponseFixture()
		fixture.RuleID = 100 + i
		rules = append(rules, fixture)
	}
	page := map[string]any{"items": rules, "next": "more"}
	resp, requests, err := invokeCMP(t, map[string]func(*http.Request) (int, any){
		"/v1/cookie-consent/configs/config-123/rules": respondJSON(http.StatusOK, page),
	}, "getCookieConsentRules", map[string]property.Value{
		"configId": property.New("config-123"), "maxResults": property.New(2.0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || !strings.Contains(requests[0].RawQuery, "limit=2") {
		t.Fatalf("expected one request with limit=2, got %#v", requests)
	}
	if got := resp.Return.Get("rules").AsArray().Len(); got != 2 {
		t.Fatalf("expected two rules, got %d", got)
	}
	if _, _, err := invokeCMP(t, nil, "getCookieConsentRules", map[string]property.Value{
		"configId": property.New("config-123"), "maxResults": property.New(-1.0),
	}); err == nil || !strings.Contains(err.Error(), "maxResults must not be negative") {
		t.Fatalf("expected a negative maxResults error, got %v", err)
	}
}

func TestPublishCookieConsentFailurePaths(t *testing.T) {
	t.Run("a rejected publish request fails without polling", func(t *testing.T) {
		var posts atomic.Int32
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				posts.Add(1)
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"webhookUrl is invalid"}`))
				return
			}
			writePublicationConfigResponse(t, w, "published", 100, 3)
		}))
		defer api.Close()

		_, err := publishCookieConsent(
			t.Context(), newCMPJSONClient(t, api.URL), publicationArgsFixture(), zeroPublicationPollOptions(),
		)
		if err == nil || !strings.Contains(err.Error(), "queue Cookie Consent publication for config \"config-id\"") ||
			!strings.Contains(err.Error(), "status=400") {
			t.Fatalf("expected the 400 to fail the publication, got %v", err)
		}
		if posts.Load() != 1 {
			t.Fatalf("expected exactly one POST, got %d", posts.Load())
		}
	})

	t.Run("a failing poll fails the publication", func(t *testing.T) {
		var gets atomic.Int32
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if gets.Add(1) == 1 {
				writePublicationConfigResponse(t, w, "published", 100, 3)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer api.Close()

		client := newCMPJSONClientWithOptions(t, api.URL, osanoclient.WithMaxRetries(0))
		_, err := publishCookieConsent(t.Context(), client, publicationArgsFixture(), zeroPublicationPollOptions())
		if err == nil || !strings.Contains(err.Error(), "poll Cookie Consent publication for config \"config-id\"") {
			t.Fatalf("expected the failing poll to be reported, got %v", err)
		}
	})
}

// A 2xx answer without a configId is a malformed response, not a deleted configuration: reporting
// deletion would make the next update create a duplicate that Osano cannot delete.
func TestCookieConsentReadsRejectResponsesWithoutAConfigID(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()
	server := newCMPProviderServer(t, api.URL)

	_, err := server.Read(p.ReadRequest{
		ID: "config-123", Urn: cmpURN("CookieConsentConfig", "odd"),
		Properties: configStateProperties(), Inputs: configInputProperties(),
	})
	if err == nil || !strings.Contains(err.Error(), "has no configId") {
		t.Fatalf("expected the config read to fail, got %v", err)
	}
	_, err = server.Read(p.ReadRequest{
		ID: "config-id", Urn: cmpURN("CookieConsentPublication", "odd"),
		Properties: publicationStateProperties(), Inputs: publicationInputProperties(),
	})
	if err == nil || !strings.Contains(err.Error(), "has no configId") {
		t.Fatalf("expected the publication read to fail, got %v", err)
	}
}

// Osano cannot delete a configuration, so a create whose answer was lost adopts the configuration
// it created instead of creating a second one on the next `pulumi up`.
func TestCookieConsentConfigCreateAdoptsAConfigurationFromALostResponse(t *testing.T) {
	var lists atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusBadGateway)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/cookie-consent/configs":
			lists.Add(1)
			if got := r.URL.Query().Get("name"); got != "cookie-consent" {
				t.Errorf("expected the lookup to filter on the name, got %q", got)
			}
			created := cmpConfigResponseFixture()
			created.Created = int(time.Now().Unix()) + 1
			older := cmpConfigResponseFixture()
			older.ConfigID = "config-old"
			older.Created = 100
			writeJSON(t, w, map[string]any{"items": []any{older, created}, "next": ""})
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()

	server := newCMPProviderServer(t, api.URL)
	resp, err := server.Create(p.CreateRequest{
		Urn: cmpURN("CookieConsentConfig", "lost"), Properties: configInputProperties(),
	})
	if err != nil {
		t.Fatalf("expected the created configuration to be adopted, got %v", err)
	}
	if resp.ID != "config-123" || lists.Load() != 1 {
		t.Fatalf("expected config-123 to be adopted after one lookup, got ID %q after %d lookups", resp.ID, lists.Load())
	}

	// A 4xx means nothing was created, so there is nothing to adopt and the error is reported as is.
	rejected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			t.Errorf("a rejected create must not look for a configuration to adopt: %s", r.URL)
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer rejected.Close()
	if _, err := newCMPProviderServer(t, rejected.URL).Create(p.CreateRequest{
		Urn: cmpURN("CookieConsentConfig", "rejected"), Properties: configInputProperties(),
	}); err == nil || !strings.Contains(err.Error(), "status=422") {
		t.Fatalf("expected the 422 to be reported, got %v", err)
	}
}

// A configuration key the program stops declaring is cleared with an explicit null, because refresh
// compares only declared keys and would otherwise never notice the value surviving in Osano.
func TestCookieConsentConfigUpdateClearsRemovedConfigurationKeys(t *testing.T) {
	var body atomic.Value
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertCMPRequest(t, r, http.MethodPatch, "/v1/cookie-consent/configs/config-123")
		body.Store(decodeJSONBody(t, r))
		writeCMPConfigResponse(t, w)
	}))
	defer api.Close()

	previous := configStateProperties().Set("configuration", property.New(map[string]property.Value{
		"storagePolicyHref": property.New("https://example.com/storage-policy"),
		"flag":              property.New(true),
		"palette":           property.New(map[string]property.Value{"a": property.New("1"), "b": property.New("2")}),
		"variantMapping":    property.New(map[string]property.Value{"behavior": property.New("fallbackToOsano")}),
	}))
	inputs := configInputProperties().Set("configuration", property.New(map[string]property.Value{
		"storagePolicyHref": property.New("https://example.com/storage-policy"),
		"palette":           property.New(map[string]property.Value{"a": property.New("1")}),
		"variantMapping":    property.New(map[string]property.Value{}),
	}))
	server := newCMPProviderServer(t, api.URL)
	resp, err := server.Update(p.UpdateRequest{
		ID: "config-123", Urn: cmpURN("CookieConsentConfig", "trimmed"), State: previous, Inputs: inputs,
	})
	if err != nil {
		t.Fatal(err)
	}
	sent, _ := body.Load().(map[string]any)
	configuration, _ := sent["configuration"].(map[string]any)
	if value, present := configuration["flag"]; !present || value != nil {
		t.Fatalf("expected the removed flag to be sent as null, got %v", configuration)
	}
	palette, _ := configuration["palette"].(map[string]any)
	if value, present := palette["b"]; !present || value != nil || palette["a"] != "1" {
		t.Fatalf("expected the removed palette key to be sent as null, got %v", palette)
	}
	// variantMapping is compared and sent as a whole: an emptied object clears it, no nested nulls.
	if mapping, _ := configuration["variantMapping"].(map[string]any); len(mapping) != 0 {
		t.Fatalf("expected the emptied variantMapping to be sent as {}, got %v", mapping)
	}
	if !resp.Properties.Get("configuration").AsMap().Get("flag").IsNull() &&
		resp.Properties.Get("configuration").AsMap().Get("flag").IsBool() {
		t.Fatalf("state must record the declared configuration only, got %#v", resp.Properties.Get("configuration"))
	}
}

func TestCookieConsentConfigCheckAcceptsNullOptionalKeys(t *testing.T) {
	failures, _ := validateCookieConsentConfiguration(map[string]any{
		"storagePolicyHref": "https://example.com/privacy",
		"timeoutSeconds":    nil,
		"googleConsent":     nil,
		"palette":           nil,
		"translations":      nil,
		"variantMapping":    nil,
		"additionalLinks":   nil,
	}, "production", true)
	if len(failures) != 0 {
		t.Fatalf("a declared null asks Osano for its default and must pass the check, got %#v", failures)
	}
	failures, _ = validateCookieConsentConfiguration(map[string]any{"storagePolicyHref": nil}, "production", true)
	assertFailureProperty(t, failures, "configuration.storagePolicyHref")
}

func TestCookieConsentRuleCheckRejectsOverlongRules(t *testing.T) {
	server := newCMPProviderServer(t, "http://127.0.0.1:1")
	resp, err := server.Check(p.CheckRequest{
		Urn:    cmpURN("CookieConsentRule", "long"),
		Inputs: ruleInputProperties().Set("rule", property.New(strings.Repeat("x", 1001))),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertFailureProperty(t, resp.Failures, "rule")
}

func TestGetCollectionReturnsTheCollection(t *testing.T) {
	resp, _, err := invokeUC(t, "getCollection", map[string]property.Value{"collectionId": property.New("c1")},
		http.StatusOK, map[string]any{"collectionId": "c1", "name": "EU collection"})
	if err != nil {
		t.Fatal(err)
	}
	assertBool(t, resp.Return, "exists", true)
	assertString(t, resp.Return.Get("collection").AsMap(), "name", "EU collection")
}

// A subject lookup must not turn a 400 (a malformed request or a rejected key) into "not found".
func TestGetSubjectReportsBadRequests(t *testing.T) {
	_, _, err := invokeUC(t, "getSubject", map[string]property.Value{"subjectRef": property.New("s")},
		http.StatusBadRequest, map[string]any{"message": "x-uc-api-key header is invalid"})
	if err == nil || !strings.Contains(err.Error(), "status=400") {
		t.Fatalf("expected the 400 to fail the lookup, got %v", err)
	}
	for _, token := range []string{"getSubjectProfile", "getSession"} {
		args := map[string]property.Value{"subjectId": property.New("s")}
		if token == "getSession" {
			args = map[string]property.Value{"sessionId": property.New("s")}
		}
		if _, _, err := invokeUC(t, token, args, http.StatusBadRequest, nil); err == nil ||
			!strings.Contains(err.Error(), "status=400") {
			t.Fatalf("%s: expected the 400 to fail the lookup, got %v", token, err)
		}
	}
}

func TestVerifySubjectCodeRejectsAnUnverifiedResponse(t *testing.T) {
	_, _, err := invokeUC(t, "verifySubjectCode", map[string]property.Value{
		"email": property.New("person@example.com"), "code": property.New("123456"),
	}, http.StatusOK, map[string]any{"verified": false, "message": "code expired"})
	if err == nil || !strings.Contains(err.Error(), "verified: false") {
		t.Fatalf("expected an explicit verified:false to fail, got %v", err)
	}
}

func TestGetSubjectTrimsTheReference(t *testing.T) {
	resp, requests, err := invokeUC(t, "getSubject", map[string]property.Value{"subjectRef": property.New(" user-1 ")},
		http.StatusOK, map[string]any{"id": "id-1"})
	if err != nil {
		t.Fatal(err)
	}
	if requests[0].Path != "/v2/subjects/user-1" {
		t.Fatalf("expected the trimmed reference in the path, got %q", requests[0].Path)
	}
	assertString(t, resp.Return, "subjectRef", "user-1")
}

func TestUnifiedConsentInvokeTokensStillRegistered(t *testing.T) {
	server := newUCProviderServer(t, "https://uc.invalid")
	for _, token := range []string{
		"getUnifiedConsent", "getSubject", "getConfig", "getCollections", "getCollection",
		"checkConsent", "getConsentProfile", "getSubjectProfile", "getSession", "sendSubjectCode", "verifySubjectCode",
	} {
		_, err := server.Invoke(p.InvokeRequest{Token: tokens.Type("osano:index:" + token), Args: property.NewMap(nil)})
		if err != nil && strings.Contains(err.Error(), "unknown function") {
			t.Fatalf("function %s is no longer registered: %v", token, err)
		}
	}
}
