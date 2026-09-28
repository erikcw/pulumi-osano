package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/blang/semver"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const ucTestPathPrefix = "/uc-proxy"

type recordedUCRequest struct {
	Method string
	Path   string
	Query  map[string]string
	UCKey  string
	Key    string
	Body   map[string]any
	// Country and Region record the geolocation override headers.
	Country string
	Region  string
}

type ucMockAPI struct {
	t        *testing.T
	mu       sync.Mutex
	requests []recordedUCRequest
	status   int
	body     any
}

func (m *ucMockAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	record := recordedUCRequest{
		Method: r.Method,
		Path:   r.URL.EscapedPath(),
		Query:  map[string]string{},
		UCKey:  r.Header.Get("x-uc-api-key"),
		Key:    r.Header.Get("x-osano-api-key"),

		Country: r.Header.Get("x-country-code-override"),
		Region:  r.Header.Get("x-region-code-override"),
	}
	for key := range r.URL.Query() {
		record.Query[key] = r.URL.Query().Get(key)
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&record.Body); err != nil {
			m.t.Errorf("decode request body: %v", err)
		}
	}

	m.mu.Lock()
	m.requests = append(m.requests, record)
	status, body := m.status, m.body
	m.mu.Unlock()

	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	if body != nil {
		if err := json.NewEncoder(w).Encode(body); err != nil {
			m.t.Errorf("encode response: %v", err)
		}
	}
}

func (m *ucMockAPI) recorded() []recordedUCRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recordedUCRequest(nil), m.requests...)
}

func newUCProviderServer(t *testing.T, apiBaseURL string) integration.Server {
	t.Helper()
	for _, env := range []string{envOsanoAPIKey, envUnifiedConsent, envAPIBaseURL, envRequestTimeout} {
		t.Setenv(env, "")
	}
	server, err := integration.NewServer(
		t.Context(), Name, semver.MustParse("1.0.0"), integration.WithProvider(Provider()),
	)
	if err != nil {
		t.Fatal(err)
	}
	err = server.Configure(p.ConfigureRequest{Args: property.NewMap(map[string]property.Value{
		"osanoApiKey":           property.New("test-osano-key"),
		"unifiedConsentApiKey":  property.New("test-uc-key"),
		"apiBaseUrl":            property.New(apiBaseURL),
		"requestTimeoutSeconds": property.New(2.0),
	})})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestUnifiedConsentInvokes(t *testing.T) {
	cases := []struct {
		name        string
		token       string
		args        map[string]property.Value
		status      int
		response    any
		wantRequest recordedUCRequest
		assert      func(*testing.T, property.Map)
	}{
		{
			name:  "getUnifiedConsent escapes the subject and keeps the base path prefix",
			token: "getUnifiedConsent",
			args:  map[string]property.Value{"subjectRef": property.New(" user/1 ")},
			response: map[string]any{
				"unifiedConsent": map[string]any{
					"subjectId": "subject-1",
					"actions":   []any{map[string]any{"target": "t", "vendor": "v", "action": "ACCEPT"}},
				},
			},
			wantRequest: recordedUCRequest{
				Method: http.MethodGet,
				Path:   ucTestPathPrefix + "/v2/consents/unified/user%2F1",
				Query:  map[string]string{"ref": "subject"},
				UCKey:  "test-uc-key",
			},
			assert: func(t *testing.T, ret property.Map) {
				assertBool(t, ret, "exists", true)
				assertString(t, ret, "subjectRef", "user/1")
				if got := ret.Get("unifiedConsent").AsMap().Get("subjectId").AsString(); got != "subject-1" {
					t.Fatalf("expected unifiedConsent.subjectId subject-1, got %q", got)
				}
			},
		},
		{
			// Osano's ref parameter accepts only subject and session; anonymous IDs are subject references.
			name:  "getUnifiedConsent maps 400 to a missing subject and sends anonymous IDs as subject refs",
			token: "getUnifiedConsent",
			args: map[string]property.Value{
				"subjectRef":    property.New("anon-1"),
				"referenceType": property.New("anonymous"),
			},
			status: http.StatusBadRequest,
			wantRequest: recordedUCRequest{
				Method: http.MethodGet,
				Path:   ucTestPathPrefix + "/v2/consents/unified/anon-1",
				Query:  map[string]string{"ref": "subject"},
				UCKey:  "test-uc-key",
			},
			assert: func(t *testing.T, ret property.Map) { assertBool(t, ret, "exists", false) },
		},
		{
			name:     "getSubject returns identifiers",
			token:    "getSubject",
			args:     map[string]property.Value{"subjectRef": property.New("subject-ref")},
			response: map[string]any{"id": "id-1", "verifiedId": "verified-1", "anonymousId": "anon-1"},
			wantRequest: recordedUCRequest{
				Method: http.MethodGet, Path: ucTestPathPrefix + "/v2/subjects/subject-ref",
				Query: map[string]string{"ref": "subject"}, UCKey: "test-uc-key",
			},
			assert: func(t *testing.T, ret property.Map) {
				assertBool(t, ret, "exists", true)
				assertString(t, ret, "subjectId", "id-1")
				assertString(t, ret, "verifiedId", "verified-1")
				assertString(t, ret, "anonymousId", "anon-1")
			},
		},
		{
			name:   "getSubject maps 404 to a missing subject",
			token:  "getSubject",
			args:   map[string]property.Value{"subjectRef": property.New("missing")},
			status: http.StatusNotFound,
			wantRequest: recordedUCRequest{
				Method: http.MethodGet,
				Path:   ucTestPathPrefix + "/v2/subjects/missing",
				Query:  map[string]string{"ref": "subject"},
				UCKey:  "test-uc-key",
			},
			assert: func(t *testing.T, ret property.Map) { assertBool(t, ret, "exists", false) },
		},
		{
			name:     "getConfig returns the configuration payload",
			token:    "getConfig",
			args:     map[string]property.Value{},
			response: map[string]any{"configId": "uc-config"},
			wantRequest: recordedUCRequest{
				Method: http.MethodGet,
				Path:   ucTestPathPrefix + "/v2/config",
				Query:  map[string]string{},
				UCKey:  "test-uc-key",
			},
			assert: func(t *testing.T, ret property.Map) {
				if got := ret.Get("config").AsMap().Get("configId").AsString(); got != "uc-config" {
					t.Fatalf("expected config.configId uc-config, got %q", got)
				}
			},
		},
		{
			name:     "getCollections sends jurisdiction and type filters",
			token:    "getCollections",
			args:     map[string]property.Value{"jurisdiction": property.New("us"), "type": property.New("published")},
			response: map[string]any{"jurisdictions": []any{"us"}, "collection": map[string]any{"id": "c1"}},
			wantRequest: recordedUCRequest{
				Method: http.MethodGet, Path: ucTestPathPrefix + "/v2/collections",
				Query: map[string]string{"jurisdiction": "us", "type": "published"}, UCKey: "test-uc-key",
			},
			assert: func(t *testing.T, ret property.Map) {
				jurisdictions := ret.Get("jurisdictions").AsArray()
				if jurisdictions.Len() != 1 || jurisdictions.Get(0).AsString() != "us" {
					t.Fatalf("unexpected jurisdictions: %#v", jurisdictions)
				}
			},
		},
		{
			name:   "getCollection maps 404 to a missing collection",
			token:  "getCollection",
			args:   map[string]property.Value{"collectionId": property.New("c-missing")},
			status: http.StatusNotFound,
			wantRequest: recordedUCRequest{
				Method: http.MethodGet,
				Path:   ucTestPathPrefix + "/v2/collections/c-missing",
				Query:  map[string]string{},
				UCKey:  "test-uc-key",
			},
			assert: func(t *testing.T, ret property.Map) { assertBool(t, ret, "exists", false) },
		},
		{
			name:     "checkConsent reports existence",
			token:    "checkConsent",
			args:     map[string]property.Value{"subjectId": property.New("subject-1")},
			response: map[string]any{"exists": true},
			wantRequest: recordedUCRequest{
				Method: http.MethodGet,
				Path:   ucTestPathPrefix + "/v2/consents/check/subject-1",
				Query:  map[string]string{},
				UCKey:  "test-uc-key",
			},
			assert: func(t *testing.T, ret property.Map) { assertBool(t, ret, "exists", true) },
		},
		{
			name:     "getConsentProfile sends the config ID query",
			token:    "getConsentProfile",
			args:     map[string]property.Value{"hashedSubjectId": property.New("hash-1"), "configId": property.New("cfg-1")},
			response: map[string]any{"status": "active"},
			wantRequest: recordedUCRequest{
				Method: http.MethodGet, Path: ucTestPathPrefix + "/v2/consent-profiles/hash-1",
				Query: map[string]string{"configId": "cfg-1"}, UCKey: "test-uc-key",
			},
			assert: func(t *testing.T, ret property.Map) {
				assertBool(t, ret, "exists", true)
				if got := ret.Get("profile").AsMap().Get("status").AsString(); got != "active" {
					t.Fatalf("expected profile.status active, got %q", got)
				}
			},
		},
		{
			name:  "sendSubjectCode uses the SMS route and sends every configured key",
			token: "sendSubjectCode",
			args:  map[string]property.Value{"hashedSubjectId": property.New("hash-1"), "phone": property.New("+15555550100")},
			wantRequest: recordedUCRequest{
				Method: http.MethodPost, Path: ucTestPathPrefix + "/v2/subjects/send-code/sms",
				Query: map[string]string{}, Key: "test-osano-key", UCKey: "test-uc-key",
				Body: map[string]any{"hashedSubjectId": "hash-1", "phone": "+15555550100"},
			},
			assert: func(t *testing.T, ret property.Map) {
				assertString(t, ret, "channel", "sms")
				assertString(t, ret, "destination", "+15555550100")
			},
		},
		{
			name:  "verifySubjectCode uses the email route and returns the profile",
			token: "verifySubjectCode",
			args: map[string]property.Value{
				"hashedSubjectId": property.New("hash-1"),
				"email":           property.New("person@example.com"),
				"code":            property.New("123456"),
			},
			response: map[string]any{"verified": true},
			wantRequest: recordedUCRequest{
				Method: http.MethodPost, Path: ucTestPathPrefix + "/v2/subjects/profile/verify",
				Query: map[string]string{}, Key: "test-osano-key", UCKey: "test-uc-key",
				Body: map[string]any{"hashedSubjectId": "hash-1", "email": "person@example.com", "code": "123456"},
			},
			assert: func(t *testing.T, ret property.Map) {
				assertBool(t, ret, "verified", true)
				assertString(t, ret, "channel", "email")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &ucMockAPI{t: t, status: tc.status, body: tc.response}
			api := httptest.NewServer(mock)
			defer api.Close()

			server := newUCProviderServer(t, api.URL+ucTestPathPrefix+"/")
			resp, err := server.Invoke(p.InvokeRequest{
				Token: tokens.Type("osano:index:" + tc.token),
				Args:  property.NewMap(tc.args),
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.Failures) != 0 {
				t.Fatalf("unexpected invoke failures: %#v", resp.Failures)
			}

			requests := mock.recorded()
			if len(requests) != 1 {
				t.Fatalf("expected one request, got %#v", requests)
			}
			if !reflect.DeepEqual(requests[0], tc.wantRequest) {
				t.Fatalf("unexpected request:\n got: %#v\nwant: %#v", requests[0], tc.wantRequest)
			}
			tc.assert(t, resp.Return)
		})
	}
}

// consentInputs is a valid Consent input map as the engine sends it.
func consentInputs() property.Map {
	return property.NewMap(map[string]property.Value{
		"subject": property.New(map[string]property.Value{"verifiedId": property.New("user-1")}),
		"actions": property.New([]property.Value{property.New(map[string]property.Value{
			"target": property.New("protocol-1"),
			"vendor": property.New("config-1"),
			"action": property.New("ACCEPT"),
		})}),
		"attributes": property.New(map[string]property.Value{"count": property.New("1000000")}),
	})
}

func TestConsentResourceLifecycle(t *testing.T) {
	t.Run("preview makes no HTTP calls", func(t *testing.T) {
		mock := &ucMockAPI{t: t}
		api := httptest.NewServer(mock)
		defer api.Close()

		server := newUCProviderServer(t, api.URL)
		if _, err := server.Create(p.CreateRequest{
			Urn: cmpURN("Consent", "preview"), Properties: consentInputs(), DryRun: true,
		}); err != nil {
			t.Fatal(err)
		}
		if got := mock.recorded(); len(got) != 0 {
			t.Fatalf("expected no requests during preview, got %#v", got)
		}
	})

	t.Run("create posts the consent payload", func(t *testing.T) {
		mock := &ucMockAPI{t: t, status: http.StatusCreated}
		api := httptest.NewServer(mock)
		defer api.Close()

		server := newUCProviderServer(t, api.URL)
		resp, err := server.Create(p.CreateRequest{Urn: cmpURN("Consent", "created"), Properties: consentInputs()})
		if err != nil {
			t.Fatal(err)
		}
		requests := mock.recorded()
		if len(requests) != 1 || requests[0].Method != http.MethodPost || requests[0].Path != "/v2/consents" ||
			requests[0].UCKey != "test-uc-key" {
			t.Fatalf("unexpected create request: %#v", requests)
		}
		if got := requests[0].Body["subject"]; !reflect.DeepEqual(got, map[string]any{"verifiedId": "user-1"}) {
			t.Fatalf("unexpected subject payload: %#v", got)
		}
		if resp.Properties.Get("consentId").AsString() == "" {
			t.Fatal("expected a consentId output")
		}
	})

	t.Run("refresh keeps submitted inputs instead of the merged subject view", func(t *testing.T) {
		mock := &ucMockAPI{t: t, body: map[string]any{
			"unifiedConsent": map[string]any{
				"subjectId":  "subject-1",
				"actions":    []any{map[string]any{"target": "other", "vendor": "other", "action": "REJECT"}},
				"attributes": map[string]any{"count": 1000000},
			},
		}}
		api := httptest.NewServer(mock)
		defer api.Close()

		server := newUCProviderServer(t, api.URL)
		state := consentInputs().
			Set("consentId", property.New("consent-1")).
			Set("lastSynced", property.New("2026-01-01T00:00:00Z"))
		resp, err := server.Read(p.ReadRequest{
			ID: "consent-1", Urn: cmpURN("Consent", "refreshed"), Properties: state, Inputs: consentInputs(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "consent-1" {
			t.Fatalf("expected the consent to remain tracked, got ID %q", resp.ID)
		}
		actions := resp.Inputs.Get("actions").AsArray()
		if actions.Len() != 1 {
			t.Fatalf("refresh must not rewrite actions: %#v", resp.Inputs)
		}
		action := actions.Get(0).AsMap()
		assertString(t, action, "target", "protocol-1")
		assertString(t, action, "action", "ACCEPT")
		assertString(t, resp.Inputs.Get("attributes").AsMap(), "count", "1000000")
		if got := resp.Properties.Get("lastSynced").AsString(); got != "2026-01-01T00:00:00Z" {
			t.Fatalf("refresh must not rewrite lastSynced (a perpetual diff), got %q", got)
		}
		requests := mock.recorded()
		if len(requests) != 1 || requests[0].Path != "/v2/consents/unified/user-1" || requests[0].Query["ref"] != "subject" {
			t.Fatalf("unexpected refresh request: %#v", requests)
		}
	})

	// Dropping the resource would make the next `pulumi up` submit the consent again without the
	// subject's involvement, for example after Osano honored an erasure request.
	t.Run("refresh keeps the resource when the subject has no consent", func(t *testing.T) {
		mock := &ucMockAPI{t: t, status: http.StatusBadRequest}
		api := httptest.NewServer(mock)
		defer api.Close()

		server := newUCProviderServer(t, api.URL)
		state := consentInputs().Set("consentId", property.New("consent-1")).Set("lastSynced", property.New(""))
		resp, err := server.Read(p.ReadRequest{
			ID: "consent-1", Urn: cmpURN("Consent", "gone"), Properties: state, Inputs: consentInputs(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "consent-1" {
			t.Fatalf("a consent must stay tracked when Osano reports none, got ID %q", resp.ID)
		}
	})

	t.Run("import is rejected with a clear message", func(t *testing.T) {
		mock := &ucMockAPI{t: t}
		api := httptest.NewServer(mock)
		defer api.Close()

		server := newUCProviderServer(t, api.URL)
		_, err := server.Read(p.ReadRequest{ID: "consent-1", Urn: cmpURN("Consent", "imported")})
		if err == nil || !strings.Contains(err.Error(), "cannot be imported") {
			t.Fatalf("expected an import error, got %v", err)
		}
		if got := mock.recorded(); len(got) != 0 {
			t.Fatalf("an import must not call Osano, got %#v", got)
		}
	})

	t.Run("preview leaves the consent ID unknown", func(t *testing.T) {
		server := newUCProviderServer(t, "https://uc.invalid")
		resp, err := server.Create(p.CreateRequest{
			Urn: cmpURN("Consent", "preview"), Properties: consentInputs(), DryRun: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !resp.Properties.Get("consentId").IsComputed() {
			t.Fatalf("expected consentId to be unknown during preview, got %#v", resp.Properties.Get("consentId"))
		}
	})
}

func assertBool(t *testing.T, m property.Map, key string, want bool) {
	t.Helper()
	if got := m.Get(key); !got.IsBool() || got.AsBool() != want {
		t.Fatalf("expected %s=%v, got %#v", key, want, got)
	}
}

func assertString(t *testing.T, m property.Map, key, want string) {
	t.Helper()
	if got := m.Get(key); !got.IsString() || got.AsString() != want {
		t.Fatalf("expected %s=%q, got %#v", key, want, got)
	}
}
