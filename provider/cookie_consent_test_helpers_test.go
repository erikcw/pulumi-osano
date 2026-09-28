package provider

import (
	"net/url"
	"testing"

	"github.com/blang/semver"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	osanoclient "github.com/jflavan/pulumi-osano/provider/internal/osano"
)

func newCMPJSONClient(t *testing.T, baseURL string) *osanoclient.Client {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	return osanoclient.NewClient(parsed, osanoclient.WithHeader("x-osano-api-key", "test-osano-key"))
}

func newCMPJSONClientWithOptions(
	t *testing.T, baseURL string, opts ...osanoclient.ClientOption,
) *osanoclient.Client {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	options := append([]osanoclient.ClientOption{osanoclient.WithHeader("x-osano-api-key", "test-osano-key")}, opts...)
	return osanoclient.NewClient(parsed, options...)
}

func cmpURN(resourceType, name string) resource.URN {
	return resource.NewURN(
		"test-stack", "test-project", "",
		tokens.Type("osano:index:"+resourceType), name,
	)
}

func newCMPProviderServer(t *testing.T, customerBaseURL string) integration.Server {
	t.Helper()
	// Clear every environment override the Customer REST client honors so a developer's shell
	// cannot change the credentials or the 2-second request timeout these tests rely on.
	for _, name := range []string{envOsanoAPIKey, envUnifiedConsent, envAPIBaseURL, envRequestTimeout} {
		t.Setenv(name, "")
	}
	server, err := integration.NewServer(
		t.Context(), Name, semver.MustParse("1.0.0"), integration.WithProvider(Provider()),
	)
	if err != nil {
		t.Fatal(err)
	}
	err = server.Configure(p.ConfigureRequest{Args: property.NewMap(map[string]property.Value{
		"osanoApiKey":           property.New("test-osano-key"),
		"customerBaseUrl":       property.New(customerBaseURL),
		"requestTimeoutSeconds": property.New(2.0),
	})})
	if err != nil {
		t.Fatal(err)
	}
	return server
}
