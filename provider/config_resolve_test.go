package provider

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		envOsanoAPIKey, envUnifiedConsent, envAPIBaseURL, envCustomerBaseURL, envRequestTimeout,
	} {
		t.Setenv(name, "")
	}
}

// Stack configuration wins over the environment, as in other Pulumi providers, so two explicit
// providers with different keys never share one exported key.
func TestResolveSettingsPrefersStackConfigOverEnvironment(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv(envOsanoAPIKey, "environment-key")
	t.Setenv(envUnifiedConsent, "environment-uc-key")
	t.Setenv(envRequestTimeout, "7")
	t.Setenv(envAPIBaseURL, "https://env.example.test")
	t.Setenv(envCustomerBaseURL, "https://env-customer.example.test")

	resolved, err := resolveSettings(Config{ //nolint:gosec // Test fixtures, not credentials.
		OsanoAPIKey:           " config-key ",
		UnifiedConsentAPIKey:  "config-uc-key",
		CustomerBaseURL:       "https://customer.example.test/root",
		APIBaseURL:            "https://uc.example.test/prefix/",
		RequestTimeoutSeconds: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.osanoAPIKey != "config-key" || resolved.unifiedConsentAPIKey != "config-uc-key" {
		t.Fatalf("expected the configured keys, got %q and %q", resolved.osanoAPIKey, resolved.unifiedConsentAPIKey)
	}
	if resolved.timeout != 3*time.Second {
		t.Fatalf("expected the configured 3s timeout, got %s", resolved.timeout)
	}
	if resolved.customerBaseURL.String() != "https://customer.example.test/root" ||
		resolved.unifiedConsentBaseURL.String() != "https://uc.example.test/prefix/" {
		t.Fatalf("unexpected base URLs %s and %s", resolved.customerBaseURL, resolved.unifiedConsentBaseURL)
	}
	joined := strings.Join(resolved.warnings, "\n")
	for _, name := range []string{envOsanoAPIKey, envUnifiedConsent, envAPIBaseURL, envCustomerBaseURL} {
		if !strings.Contains(joined, name+" is set but") {
			t.Fatalf("expected a warning that %s is ignored, got %q", name, joined)
		}
	}
}

func TestResolveSettingsFallsBackToEnvironment(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv(envOsanoAPIKey, " environment-key ")
	t.Setenv(envUnifiedConsent, "environment-uc-key")
	t.Setenv(envRequestTimeout, "7")
	t.Setenv(envAPIBaseURL, "https://env.example.test")
	t.Setenv(envCustomerBaseURL, "https://env-customer.example.test")

	resolved, err := resolveSettings(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.osanoAPIKey != "environment-key" || resolved.unifiedConsentAPIKey != "environment-uc-key" {
		t.Fatalf("expected the environment keys, got %q and %q", resolved.osanoAPIKey, resolved.unifiedConsentAPIKey)
	}
	if resolved.timeout != 7*time.Second {
		t.Fatalf("expected the environment 7s timeout, got %s", resolved.timeout)
	}
	if resolved.customerBaseURL.String() != "https://env-customer.example.test" ||
		resolved.unifiedConsentBaseURL.String() != "https://env.example.test" {
		t.Fatalf("unexpected base URLs %s and %s", resolved.customerBaseURL, resolved.unifiedConsentBaseURL)
	}
	if len(resolved.warnings) != 0 {
		t.Fatalf("expected no warnings, got %q", resolved.warnings)
	}
}

func TestResolveSettingsDefaults(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv(envOsanoAPIKey, " \t ")

	resolved, err := resolveSettings(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.osanoAPIKey != "" || resolved.unifiedConsentAPIKey != "" {
		t.Fatalf("expected no keys, got %q and %q", resolved.osanoAPIKey, resolved.unifiedConsentAPIKey)
	}
	if resolved.customerBaseURL.String() != defaultCustomerBaseURL ||
		resolved.unifiedConsentBaseURL.String() != defaultAPIBaseURL {
		t.Fatalf("unexpected default base URLs %s and %s", resolved.customerBaseURL, resolved.unifiedConsentBaseURL)
	}
	if resolved.timeout != defaultRequestTimeoutSecs*time.Second {
		t.Fatalf("expected the default timeout, got %s", resolved.timeout)
	}
	if resolved.customer == nil || resolved.unifiedConsent == nil {
		t.Fatal("expected both API clients to be built")
	}
}

func TestResolveSettingsFoldsDeprecatedKeys(t *testing.T) {
	clearProviderEnv(t)

	resolved, err := resolveSettings(Config{UCAPIKey: "legacy-key", UCBaseURL: "https://legacy.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.unifiedConsentAPIKey != "legacy-key" ||
		resolved.unifiedConsentBaseURL.String() != "https://legacy.example.test" {
		t.Fatalf("expected the deprecated values to be used, got %q and %s",
			resolved.unifiedConsentAPIKey, resolved.unifiedConsentBaseURL)
	}
	joined := strings.Join(resolved.warnings, "\n")
	if !strings.Contains(joined, "osano:ucApiKey is deprecated") ||
		!strings.Contains(joined, "osano:ucBaseUrl is deprecated") {
		t.Fatalf("expected deprecation warnings, got %q", resolved.warnings)
	}

	resolved, err = resolveSettings(Config{UnifiedConsentAPIKey: "new-key", UCAPIKey: "legacy-key"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.unifiedConsentAPIKey != "new-key" || len(resolved.warnings) != 0 {
		t.Fatalf("the current key must win silently, got %q with warnings %q",
			resolved.unifiedConsentAPIKey, resolved.warnings)
	}
}

func TestResolveSettingsTimeouts(t *testing.T) {
	for _, test := range []struct {
		name        string
		environment string
		configured  int
		want        time.Duration
		wantWarning bool
	}{
		{name: "config wins", environment: "7", configured: 9, want: 9 * time.Second},
		{name: "environment when unset", environment: "7", configured: 0, want: 7 * time.Second},
		{name: "zero env uses default", environment: "0", configured: 0, want: 60 * time.Second, wantWarning: true},
		{name: "negative env uses default", environment: "-1", configured: 0, want: 60 * time.Second, wantWarning: true},
		{name: "invalid env uses default", environment: "invalid", configured: 0, want: 60 * time.Second, wantWarning: true},
		{name: "huge env uses default", environment: "10000000000", configured: 0, want: 60 * time.Second, wantWarning: true},
		{name: "whitespace env uses default", environment: " \t ", configured: 0, want: 60 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearProviderEnv(t)
			t.Setenv(envRequestTimeout, test.environment)
			resolved, err := resolveSettings(Config{RequestTimeoutSeconds: test.configured})
			if err != nil {
				t.Fatal(err)
			}
			if resolved.timeout != test.want {
				t.Fatalf("expected timeout %s, got %s", test.want, resolved.timeout)
			}
			if (len(resolved.warnings) > 0) != test.wantWarning {
				t.Fatalf("warnings %q, want warning=%v", resolved.warnings, test.wantWarning)
			}
		})
	}

	clearProviderEnv(t)
	_, err := resolveSettings(Config{RequestTimeoutSeconds: maxRequestTimeoutSecs + 1})
	if err == nil || !strings.Contains(err.Error(), "requestTimeoutSeconds must be at most 3600") {
		t.Fatalf("expected an out-of-range timeout error, got %v", err)
	}
}

func TestResolveSettingsValidatesBaseURLs(t *testing.T) {
	clearProviderEnv(t)
	for _, test := range []struct {
		name  string
		cfg   Config
		want  string
		valid bool
	}{
		{
			name: "malformed customer URL", cfg: Config{CustomerBaseURL: "not a URL"},
			want: `invalid customerBaseUrl "not a URL"`,
		},
		{
			name: "scheme-less customer URL", cfg: Config{CustomerBaseURL: "api.osano.com"},
			want: `invalid customerBaseUrl "api.osano.com"`,
		},
		{
			name: "scheme-less UC URL", cfg: Config{APIBaseURL: "uc.api.osano.com"},
			want: `invalid apiBaseUrl "uc.api.osano.com"`,
		},
		{
			name: "plain http to a remote host", cfg: Config{CustomerBaseURL: "http://proxy.example.test"},
			want: "only sent over https",
		},
		{
			name: "plain http to a remote UC host", cfg: Config{APIBaseURL: "http://collector.example.test/uc"},
			want: "only sent over https",
		},
		{name: "http to localhost", cfg: Config{CustomerBaseURL: "http://localhost:8080"}, valid: true},
		{name: "http to 127.0.0.1", cfg: Config{APIBaseURL: "http://127.0.0.1:18080/prefix"}, valid: true},
		{name: "http to ::1", cfg: Config{APIBaseURL: "http://[::1]:18080"}, valid: true},
		{name: "https anywhere", cfg: Config{CustomerBaseURL: "https://api.example.test/mount/"}, valid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolveSettings(test.cfg)
			if test.valid {
				if err != nil {
					t.Fatalf("expected the URL to be accepted, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected an error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestCustomerClientReportsMissingAPIKey(t *testing.T) {
	clearProviderEnv(t)

	_, err := Config{}.customerClient()
	if err == nil {
		t.Fatal("expected missing API key error")
	}
	if err.Error() != "Osano API key not configured; set osano:osanoApiKey or OSANO_API_KEY" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The Pulumi Registry asks providers to identify themselves to the vendor API, so Customer REST API
// calls send the same pulumi-osano/<version> user agent as Unified Consent calls.
func TestCustomerClientSendsProviderUserAgentAndKey(t *testing.T) {
	var userAgent, key atomic.Value
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent.Store(r.Header.Get("User-Agent"))
		key.Store(r.Header.Get("x-osano-api-key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()
	clearProviderEnv(t)

	client, err := Config{OsanoAPIKey: "config-key", CustomerBaseURL: api.URL}.customerClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DoJSON(t.Context(), http.MethodGet, "/v1/ping", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if ua, _ := userAgent.Load().(string); ua != providerUserAgent() || !strings.HasPrefix(ua, "pulumi-osano/") {
		t.Fatalf("expected the provider user agent %q, got %q", providerUserAgent(), ua)
	}
	if got, _ := key.Load().(string); got != "config-key" {
		t.Fatalf("expected the API key header, got %q", got)
	}
}

// Released binaries are stamped with the git tag (v0.1.0) and Makefile builds with the bare version
// (0.1.0); both must send pulumi-osano/0.1.0.
func TestUserAgentForVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version string
		want    string
	}{
		{version: "0.1.0", want: "pulumi-osano/0.1.0"},
		{version: "v0.1.0", want: "pulumi-osano/0.1.0"},
		{version: "v0.1.0-alpha.1727200000", want: "pulumi-osano/0.1.0-alpha.1727200000"},
		{version: "0.1.0-alpha.0+dev", want: "pulumi-osano/0.1.0-alpha.0+dev"},
		{version: " v1.2.3 ", want: "pulumi-osano/1.2.3"},
		{version: "", want: "pulumi-osano/dev"},
		{version: "  ", want: "pulumi-osano/dev"},
	}
	for _, tt := range tests {
		if got := userAgentForVersion(tt.version); got != tt.want {
			t.Errorf("userAgentForVersion(%q) = %q, want %q", tt.version, got, tt.want)
		}
	}
}

// diffProviderConfig compares exactly the keys the Config struct declares, so a new field can never
// be left out of the provider diff.
func TestProviderConfigKeysMatchTheConfigStruct(t *testing.T) {
	t.Parallel()
	want := []string{
		"osanoApiKey", "unifiedConsentApiKey", "ucApiKey",
		"customerBaseUrl", "apiBaseUrl", "ucBaseUrl", "requestTimeoutSeconds",
	}
	if !reflect.DeepEqual(providerConfigKeys, want) {
		t.Fatalf("providerConfigKeys = %v, want %v", providerConfigKeys, want)
	}
}
