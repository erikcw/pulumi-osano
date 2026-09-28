//go:build e2e

// Package e2e holds the opt-in suites that call Osano's Unified Consent API through the provider
// itself: each suite boots the provider in-process, configures it from the OSANO_* environment, and
// drives its functions and the Consent resource the way the Pulumi engine would. The suites need
// credentials and are guarded by build tags and opt-in variables; see tests/README.md.
package e2e

import (
	"strings"
	"testing"

	"github.com/blang/semver"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/jflavan/pulumi-osano/provider"
	"github.com/jflavan/pulumi-osano/tests/e2e/internal/testenv"
)

// providerServer returns the provider configured with the Unified Consent API key, the Osano API
// key when present, and the API base URL override when set.
func providerServer(t *testing.T) integration.Server {
	t.Helper()
	args := map[string]property.Value{
		"unifiedConsentApiKey": property.New(
			testenv.Require(t, testenv.EnvUnifiedConsentAPIKey, "Unified Consent API key"),
		).WithSecret(true),
	}
	if key := testenv.MaybeGet(testenv.EnvOsanoAPIKey); key != "" {
		args["osanoApiKey"] = property.New(key).WithSecret(true)
	}
	if baseURL := testenv.MaybeGet(testenv.EnvAPIBaseURL); baseURL != "" {
		args["apiBaseUrl"] = property.New(baseURL)
	}

	server, err := integration.NewServer(
		t.Context(), provider.Name, semver.MustParse("0.0.0"), integration.WithProvider(provider.Provider()),
	)
	if err != nil {
		t.Fatalf("start the provider: %v", err)
	}
	if err := server.Configure(p.ConfigureRequest{Args: property.NewMap(args)}); err != nil {
		t.Fatalf("configure the provider: %v", err)
	}
	return server
}

// invoke calls a provider function and fails the test on an error or a check failure.
func invoke(t *testing.T, server integration.Server, token string, args map[string]property.Value) property.Map {
	t.Helper()
	resp, err := server.Invoke(p.InvokeRequest{
		Token: tokens.Type("osano:index:" + token),
		Args:  property.NewMap(args),
	})
	if err != nil {
		t.Fatalf("%s failed: %v", token, err)
	}
	if len(resp.Failures) != 0 {
		t.Fatalf("%s rejected its inputs: %v", token, resp.Failures)
	}
	return resp.Return
}

func consentURN(name string) resource.URN {
	return resource.NewURN("e2e", "pulumi-osano", "", tokens.Type("osano:index:Consent"), name)
}

func stringOutput(t *testing.T, m property.Map, key string) string {
	t.Helper()
	value := m.Get(key)
	if !value.IsString() {
		t.Fatalf("expected %s to be a string, got %#v", key, value)
	}
	return value.AsString()
}

func boolOutput(t *testing.T, m property.Map, key string) bool {
	t.Helper()
	value := m.Get(key)
	if !value.IsBool() {
		t.Fatalf("expected %s to be a boolean, got %#v", key, value)
	}
	return value.AsBool()
}

func mapOutput(t *testing.T, m property.Map, key string) property.Map {
	t.Helper()
	value := m.Get(key)
	if !value.IsMap() {
		t.Fatalf("expected %s to be an object, got %#v", key, value)
	}
	return value.AsMap()
}

func redactContact(contact string) string {
	trimmed := strings.TrimSpace(contact)
	if len(trimmed) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(trimmed)-4) + trimmed[len(trimmed)-4:]
}
