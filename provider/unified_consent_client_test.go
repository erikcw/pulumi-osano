package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	osanoclient "github.com/jflavan/pulumi-osano/provider/internal/osano"
)

func TestValidateConsentArgs(t *testing.T) {
	t.Parallel()

	valid := ConsentArgs{
		Subject: ConsentSubject{VerifiedID: "verified-123"},
		Actions: []ConsentAction{
			{Target: "protocol-1", Vendor: "config-1", Action: "ACCEPT"},
		},
	}

	cases := []struct {
		name    string
		args    ConsentArgs
		wantErr string
	}{
		{
			name: "valid",
			args: valid,
		},
		{
			name:    "missing actions",
			args:    ConsentArgs{Subject: valid.Subject},
			wantErr: "at least one consent action is required",
		},
		{
			name: "missing subject",
			args: ConsentArgs{
				Actions: valid.Actions,
			},
			wantErr: "either subject.verifiedId or subject.anonymousId must be set",
		},
		{
			name: "missing target",
			args: ConsentArgs{
				Subject: valid.Subject,
				Actions: []ConsentAction{{Vendor: "config-1", Action: "ACCEPT"}},
			},
			wantErr: "actions[0].target is required",
		},
		{
			name: "privacy policy url required",
			args: ConsentArgs{
				Subject: valid.Subject,
				Actions: valid.Actions,
				Compliance: &ConsentCompliance{
					PrivacyPolicy: &ConsentPrivacyPolicy{},
				},
			},
			wantErr: "compliance.privacyPolicy.url is required when privacyPolicy is provided",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateConsentArgs(tc.args)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestResolveVerificationContact(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		email       string
		phone       string
		wantChannel string
		wantValue   string
		wantErr     string
	}{
		{name: "email", email: "person@example.com", wantChannel: "email", wantValue: "person@example.com"},
		{name: "sms", phone: "+15551234567", wantChannel: "sms", wantValue: "+15551234567"},
		{name: "missing", wantErr: "either email or phone is required"},
		{
			name:    "both",
			email:   "person@example.com",
			phone:   "+15551234567",
			wantErr: "only one of email or phone can be provided",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			channel, value, err := resolveVerificationContact(tc.email, tc.phone)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				if channel != tc.wantChannel || value != tc.wantValue {
					t.Fatalf("expected %q/%q, got %q/%q", tc.wantChannel, tc.wantValue, channel, value)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestSendAndVerifyPayloads(t *testing.T) {
	t.Parallel()

	sendEmail := sendCodeRequest{
		HashedSubjectID: "hashed-1",
		Channel:         "email",
		Contact:         "person@example.com",
	}
	if got := sendEmail.Payload(); !reflect.DeepEqual(got, map[string]string{
		"hashedSubjectId": "hashed-1",
		"email":           "person@example.com",
	}) {
		t.Fatalf("unexpected email payload: %#v", got)
	}

	sendSMS := sendCodeRequest{
		HashedSubjectID: "hashed-2",
		Channel:         "sms",
		Contact:         "+15551234567",
	}
	if got := sendSMS.Payload(); !reflect.DeepEqual(got, map[string]string{
		"hashedSubjectId": "hashed-2",
		"phone":           "+15551234567",
	}) {
		t.Fatalf("unexpected sms payload: %#v", got)
	}

	verifySMS := verifyRequest{
		HashedSubjectID: "hashed-3",
		Channel:         "sms",
		Contact:         "+15551234567",
		Code:            "123456",
	}
	if got := verifySMS.Payload(); !reflect.DeepEqual(got, map[string]string{
		"hashedSubjectId": "hashed-3",
		"phone":           "+15551234567",
		"code":            "123456",
	}) {
		t.Fatalf("unexpected verify payload: %#v", got)
	}
}

func TestAPIClientSendsHeadersQueryAndBody(t *testing.T) {
	t.Parallel()

	var got recordedUCRequest
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		got = recordedUCRequest{
			Method: r.Method, Path: r.URL.Path, Query: map[string]string{}, UCKey: r.Header.Get("x-uc-api-key"),
		}
		got.Query["ref"] = r.URL.Query().Get("ref")
		got.Country = r.Header.Get("x-country-code-override")
		_ = json.NewDecoder(r.Body).Decode(&got.Body)
		if r.Header.Get("User-Agent") != "pulumi-osano/test" || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := newTestAPIClient(t, server.URL, "uc-key", "")
	geo := http.Header{}
	geo.Set("x-country-code-override", "US")
	body, status, err := client.doJSONWithHeaders(
		context.Background(), http.MethodPost, "/v2/test", url.Values{"ref": {"subject"}},
		map[string]string{"hello": "world"}, geo, headerUnifiedConsent,
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if status != http.StatusCreated || string(body) != `{"ok":true}` {
		t.Fatalf("unexpected response %d %s", status, body)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.Method != http.MethodPost || got.Path != "/v2/test" || got.Query["ref"] != "subject" ||
		got.UCKey != "uc-key" || got.Country != "US" || got.Body["hello"] != "world" {
		t.Fatalf("unexpected request %#v", got)
	}
}

func TestAPIClientRequiresConfiguredKey(t *testing.T) {
	t.Parallel()

	client := newTestAPIClient(t, "https://example.com", "", "")
	_, _, err := client.doJSON(context.Background(), http.MethodGet, "/v2/config", nil, nil, headerUnifiedConsent)
	if err == nil || !strings.Contains(err.Error(), "Unified Consent API key not configured") {
		t.Fatalf("expected a missing-key error, got %v", err)
	}
	_, _, err = client.doJSON(context.Background(), http.MethodPost, "/v2/subjects/send-code", nil, nil, headerSubject)
	if err == nil || !strings.Contains(err.Error(), "no Osano API key configured") {
		t.Fatalf("expected a missing-key error for subject routes, got %v", err)
	}
}

// A validation error from a subject-verification route may echo the email address or phone number
// the request carried, so its body is withheld.
func TestSubjectRouteErrorsWithholdTheBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"phone +15551234567 is not valid"}`))
	}))
	defer server.Close()

	client := newTestAPIClient(t, server.URL, "uc-key", "osano-key")
	_, err := client.VerifySubjectCode(context.Background(), verifyRequest{
		Channel: "sms", Contact: "+15551234567", Code: "1",
	})
	if !osanoclient.IsHTTPStatus(err, http.StatusBadRequest) {
		t.Fatalf("expected a 400 error, got %v", err)
	}
	if strings.Contains(err.Error(), "5551234567") {
		t.Fatalf("error echoes the phone number: %v", err)
	}
	_, err = client.SendVerificationCode(context.Background(), sendCodeRequest{Channel: "sms", Contact: "+15551234567"})
	if err == nil || strings.Contains(err.Error(), "5551234567") {
		t.Fatalf("send-code error must withhold the body, got %v", err)
	}
}

func TestGeoOverrideHeadersAreUpperCase(t *testing.T) {
	t.Parallel()

	headers := geoOverride{CountryCode: " de ", RegionCode: "de-by"}.headers()
	if headers.Get("x-country-code-override") != "DE" || headers.Get("x-region-code-override") != "DE-BY" {
		t.Fatalf("expected upper-case ISO codes, got %v", headers)
	}
	if len(geoOverride{}.headers()) != 0 {
		t.Fatal("expected no headers for an empty override")
	}
}

// newTestAPIClient builds a Unified Consent client for a test server without going through Configure.
func newTestAPIClient(t *testing.T, baseURL, unifiedConsentKey, osanoKey string) *apiClient {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	resolved := &settings{
		unifiedConsentAPIKey:  unifiedConsentKey,
		osanoAPIKey:           osanoKey,
		unifiedConsentBaseURL: parsed,
		timeout:               time.Second,
	}
	return &apiClient{
		settings: resolved,
		client: osanoclient.NewClient(parsed,
			osanoclient.WithHTTPClient(osanoclient.NewHTTPClient(time.Second)),
			osanoclient.WithUserAgent("pulumi-osano/test"),
			osanoclient.WithInitialBackoff(time.Millisecond),
		),
	}
}
