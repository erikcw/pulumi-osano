package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	osanoclient "github.com/jflavan/pulumi-osano/provider/internal/osano"
)

const (
	defaultCustomerBaseURL    = "https://api.osano.com"
	defaultAPIBaseURL         = "https://uc.api.osano.com"
	defaultRequestTimeoutSecs = 60
	// maxRequestTimeoutSecs bounds requestTimeoutSeconds so the timeout cannot overflow a Duration,
	// which would disable it.
	maxRequestTimeoutSecs = 3600

	envOsanoAPIKey     = "OSANO_API_KEY" //nolint:gosec // Environment variable name, not a credential.
	envUnifiedConsent  = "OSANO_UC_API_KEY"
	envAPIBaseURL      = "OSANO_API_BASE_URL"
	envCustomerBaseURL = "OSANO_CUSTOMER_BASE_URL"
	envRequestTimeout  = "OSANO_API_TIMEOUT_SECONDS"
)

// Config defines provider-level settings for talking to the Osano APIs. A stack configuration
// value wins over the matching environment variable, which is read only when the value is unset.
type Config struct {
	OsanoAPIKey           string `pulumi:"osanoApiKey,optional" provider:"secret"`
	UnifiedConsentAPIKey  string `pulumi:"unifiedConsentApiKey,optional" provider:"secret"`
	UCAPIKey              string `pulumi:"ucApiKey,optional" provider:"secret"`
	CustomerBaseURL       string `pulumi:"customerBaseUrl,optional"`
	APIBaseURL            string `pulumi:"apiBaseUrl,optional"`
	UCBaseURL             string `pulumi:"ucBaseUrl,optional"`
	RequestTimeoutSeconds int    `pulumi:"requestTimeoutSeconds,optional"`

	// settings holds the configuration resolved by Configure: defaults applied, deprecated keys
	// folded, environment fallbacks read, base URLs validated, and one client per Osano API.
	settings *settings
}

// Annotate documents the configuration schema exposed to Pulumi users.
func (c *Config) Annotate(a infer.Annotator) {
	a.Describe(
		&c.OsanoAPIKey,
		"Osano Customer REST API key, sent as x-osano-api-key by the Cookie Consent resources and functions "+
			"and by sendSubjectCode and verifySubjectCode. Set it with `pulumi config set osano:osanoApiKey "+
			"--secret`; the OSANO_API_KEY environment variable is used when it is unset.",
	)
	a.Describe(
		&c.UnifiedConsentAPIKey,
		"Unified Consent API key, sent as x-uc-api-key by the Consent resource and the Unified Consent "+
			"functions. Set it with `pulumi config set osano:unifiedConsentApiKey --secret`; the "+
			"OSANO_UC_API_KEY environment variable is used when it is unset.",
	)
	a.Describe(&c.UCAPIKey, "Former name of unifiedConsentApiKey, read only when that key is unset.")
	a.Deprecate(&c.UCAPIKey, "use unifiedConsentApiKey instead")
	a.Describe(
		&c.CustomerBaseURL,
		"Base URL of the Customer REST API (default https://api.osano.com). The OSANO_CUSTOMER_BASE_URL "+
			"environment variable is used when it is unset. Must use https, except for loopback hosts.",
	)
	a.Describe(
		&c.APIBaseURL,
		"Base URL of the Unified Consent API, including any path prefix (default https://uc.api.osano.com). "+
			"The OSANO_API_BASE_URL environment variable is used when it is unset. Must use https, except for "+
			"loopback hosts.",
	)
	a.Describe(&c.UCBaseURL, "Former name of apiBaseUrl, read only when apiBaseUrl is unset.")
	a.Deprecate(&c.UCBaseURL, "use apiBaseUrl instead")
	a.Describe(&c.RequestTimeoutSeconds,
		"Timeout in seconds for each HTTP request attempt to the Osano APIs, from 1 to 3600 (default 60). "+
			"Retried requests wait for each attempt separately. The OSANO_API_TIMEOUT_SECONDS environment "+
			"variable is used when it is unset.",
	)
}

// Configure resolves and validates the configuration once per provider process, so an invalid
// base URL or timeout fails before any resource operation, and builds the API clients.
func (c *Config) Configure(ctx context.Context) error {
	resolved, err := resolveSettings(*c)
	if err != nil {
		return err
	}
	logger := p.GetLogger(ctx)
	for _, warning := range resolved.warnings {
		logger.Warning(warning)
	}
	c.settings = resolved
	return nil
}

// providerConfigKeys lists the provider inputs that diffProviderConfig compares: every field of
// Config with a pulumi tag. Engine-managed keys such as version, pluginDownloadURL, and the
// __internal map are deliberately absent.
var providerConfigKeys = configPropertyNames()

func configPropertyNames() []string {
	configType := reflect.TypeFor[Config]()
	names := make([]string, 0, configType.NumField())
	for i := range configType.NumField() {
		tag := configType.Field(i).Tag.Get("pulumi")
		if tag == "" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		names = append(names, name)
	}
	return names
}

// diffProviderConfig reports provider configuration changes as in-place updates, never replacements.
//
// The framework default marks every changed config key as a replacement, and the Pulumi engine then
// replaces every resource that uses the provider. Credentials, endpoints, and timeouts do not change
// the identity of any Osano resource, and Osano cannot delete Cookie Consent configs, so rotating an
// API key or tuning a timeout must not recreate configs, rules, or publications.
//
// Values are compared by their string form so the unchecked inputs recorded by `pulumi import`
// (missing keys, numbers still encoded as strings) compare equal to the checked inputs a program run
// records; otherwise the first `pulumi up` after an import would replace every imported resource.
func diffProviderConfig(_ context.Context, req p.DiffRequest) (p.DiffResponse, error) {
	diff := map[string]p.PropertyDiff{}
	for _, key := range providerConfigKeys {
		if configValueString(req.State.Get(key)) != configValueString(req.Inputs.Get(key)) {
			diff[key] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
		}
	}
	if kind, changed := providerVersionDiff(req.State.Get("version"), req.Inputs.Get("version")); changed {
		diff["version"] = p.PropertyDiff{Kind: kind, InputDiff: true}
	}
	return p.DiffResponse{HasChanges: len(diff) > 0, DetailedDiff: diff}, nil
}

// providerVersionDiff reports a change of the engine-managed version input. Without it, an explicit
// provider resource diffs as unchanged after an SDK upgrade and its state keeps the old version, which
// state-driven operations such as destroy then request (pulumi/pulumi-go-provider#592). Default
// providers are named after their version, so they are replaced by name instead. A version change is
// never a replacement.
func providerVersionDiff(previous, current property.Value) (p.DiffKind, bool) {
	if previous.Equals(current) {
		return p.Update, false
	}
	switch {
	case previous.IsNull():
		return p.Add, true
	case current.IsNull():
		return p.Delete, true
	default:
		return p.Update, true
	}
}

func configValueString(value property.Value) string {
	switch {
	case value.IsComputed():
		return "<unknown>"
	case value.IsString():
		return value.AsString()
	case value.IsNumber():
		// The only numeric input is requestTimeoutSeconds, where 0 and unset both mean the default.
		if number := value.AsNumber(); number != 0 {
			return strconv.FormatFloat(number, 'f', -1, 64)
		}
		return ""
	case value.IsBool():
		return strconv.FormatBool(value.AsBool())
	default:
		return ""
	}
}

// settings is the provider configuration resolved to concrete values.
type settings struct {
	osanoAPIKey           string
	unifiedConsentAPIKey  string
	customerBaseURL       *url.URL
	unifiedConsentBaseURL *url.URL
	timeout               time.Duration
	// warnings are reported once, by Configure.
	warnings []string

	customer       *osanoclient.Client
	unifiedConsent *osanoclient.Client
}

// resolved returns the settings Configure resolved, or resolves them now for a Config that was
// built directly, as tests do.
func (c Config) resolved() (*settings, error) {
	if c.settings != nil {
		return c.settings, nil
	}
	return resolveSettings(c)
}

// customerClient returns the Customer REST API client, or an error naming the missing key.
func (c Config) customerClient() (*osanoclient.Client, error) {
	resolved, err := c.resolved()
	if err != nil {
		return nil, err
	}
	if resolved.osanoAPIKey == "" {
		return nil, errors.New("Osano API key not configured; set osano:osanoApiKey or OSANO_API_KEY")
	}
	return resolved.customer, nil
}

// customerClient returns the Customer REST API client of the configured provider.
func customerClient(ctx context.Context) (*osanoclient.Client, error) {
	return infer.GetConfig[Config](ctx).customerClient()
}

func resolveSettings(cfg Config) (*settings, error) {
	resolved := &settings{}

	resolved.osanoAPIKey = resolved.pick("osanoApiKey", cfg.OsanoAPIKey, envOsanoAPIKey)
	unifiedConsentKey := strings.TrimSpace(cfg.UnifiedConsentAPIKey)
	if unifiedConsentKey == "" && strings.TrimSpace(cfg.UCAPIKey) != "" {
		unifiedConsentKey = strings.TrimSpace(cfg.UCAPIKey)
		resolved.warn("osano:ucApiKey is deprecated; set osano:unifiedConsentApiKey instead")
	}
	resolved.unifiedConsentAPIKey = resolved.pick("unifiedConsentApiKey", unifiedConsentKey, envUnifiedConsent)

	customerBaseURL := resolved.pick("customerBaseUrl", cfg.CustomerBaseURL, envCustomerBaseURL)
	if customerBaseURL == "" {
		customerBaseURL = defaultCustomerBaseURL
	}
	unifiedConsentBaseURL := strings.TrimSpace(cfg.APIBaseURL)
	if unifiedConsentBaseURL == "" && strings.TrimSpace(cfg.UCBaseURL) != "" {
		unifiedConsentBaseURL = strings.TrimSpace(cfg.UCBaseURL)
		resolved.warn("osano:ucBaseUrl is deprecated; set osano:apiBaseUrl instead")
	}
	unifiedConsentBaseURL = resolved.pick("apiBaseUrl", unifiedConsentBaseURL, envAPIBaseURL)
	if unifiedConsentBaseURL == "" {
		unifiedConsentBaseURL = defaultAPIBaseURL
	}
	var err error
	if resolved.customerBaseURL, err = parseBaseURL("customerBaseUrl", customerBaseURL); err != nil {
		return nil, err
	}
	if resolved.unifiedConsentBaseURL, err = parseBaseURL("apiBaseUrl", unifiedConsentBaseURL); err != nil {
		return nil, err
	}

	if resolved.timeout, err = resolved.resolveTimeout(cfg.RequestTimeoutSeconds); err != nil {
		return nil, err
	}

	userAgent := providerUserAgent()
	resolved.customer = osanoclient.NewClient(
		resolved.customerBaseURL,
		osanoclient.WithHTTPClient(osanoclient.NewHTTPClient(resolved.timeout)),
		osanoclient.WithUserAgent(userAgent),
		osanoclient.WithInitialBackoff(retryInitialBackoff),
		osanoclient.WithHeader("x-osano-api-key", resolved.osanoAPIKey),
	)
	// Unified Consent routes choose their key per request: most send the Unified Consent key, the
	// subject verification routes send every configured key.
	resolved.unifiedConsent = osanoclient.NewClient(
		resolved.unifiedConsentBaseURL,
		osanoclient.WithHTTPClient(osanoclient.NewHTTPClient(resolved.timeout)),
		osanoclient.WithUserAgent(userAgent),
		osanoclient.WithInitialBackoff(retryInitialBackoff),
	)
	return resolved, nil
}

// retryInitialBackoff is the first delay before a retried request; tests shorten it.
var retryInitialBackoff = time.Second

func (s *settings) warn(format string, args ...any) {
	s.warnings = append(s.warnings, fmt.Sprintf(format, args...))
}

// pick returns the stack configuration value when it is set, otherwise the environment variable.
// An environment variable that disagrees with a set configuration value is reported, because a
// pipeline that exports it usually expects it to be used.
func (s *settings) pick(key, configValue, envName string) string {
	configValue = strings.TrimSpace(configValue)
	envValue := strings.TrimSpace(os.Getenv(envName))
	if configValue != "" {
		if envValue != "" && envValue != configValue {
			s.warn("%s is set but osano:%s is configured; the stack configuration is used", envName, key)
		}
		return configValue
	}
	return envValue
}

func (s *settings) resolveTimeout(configured int) (time.Duration, error) {
	if configured > maxRequestTimeoutSecs {
		return 0, fmt.Errorf("requestTimeoutSeconds must be at most %d, got %d", maxRequestTimeoutSecs, configured)
	}
	seconds := configured
	if seconds <= 0 {
		seconds = defaultRequestTimeoutSecs
		if value := strings.TrimSpace(os.Getenv(envRequestTimeout)); value != "" {
			parsed, err := strconv.Atoi(value)
			if err == nil && parsed > 0 && parsed <= maxRequestTimeoutSecs {
				seconds = parsed
			} else {
				s.warn("%s=%q is not a whole number of seconds from 1 to %d; using %d",
					envRequestTimeout, value, maxRequestTimeoutSecs, defaultRequestTimeoutSecs)
			}
		}
	}
	return time.Duration(seconds) * time.Second, nil
}

// parseBaseURL accepts an absolute https URL, or an http URL for a loopback host as used by local
// mocks. API keys are sent as headers, so they must never travel in clear text to another host.
func parseBaseURL(key, raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, fmt.Errorf("invalid %s %q: must be an absolute http(s) URL", key, raw)
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return nil, fmt.Errorf(
			"invalid %s %q: API keys are only sent over https (http is accepted for loopback hosts)", key, raw,
		)
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
