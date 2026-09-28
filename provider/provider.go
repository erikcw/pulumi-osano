package provider

import (
	"fmt"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"

	provVersion "github.com/jflavan/pulumi-osano/provider/version"
)

// Version is exported so the gRPC server can announce the plugin version to Pulumi.
var Version = provVersion.Version

// Name controls the namespace used by all resources in this provider.
const Name = "osano"

// Provider wires up the Pulumi provider with its config, resources, and functions.
func Provider() p.Provider {
	prov, err := infer.NewProviderBuilder().
		WithDisplayName("Osano (Unofficial)").
		WithDescription(
			"Unofficial Pulumi provider for managing Osano Cookie Consent and Unified Consent resources. "+
				"Not affiliated with Pulumi Corporation or Osano, Inc.",
		).
		WithHomepage("https://github.com/jflavan/pulumi-osano").
		WithRepository("https://github.com/jflavan/pulumi-osano").
		WithLicense("MIT").
		// Pulumi Registry metadata. The registry requires a publisher and reads the category/ and kind/
		// keywords; the other keywords become search terms. The logo is an original mark kept in this
		// repository (assets/logo.png), not Osano's or Pulumi's.
		WithPublisher("John Flavan").
		WithLogoURL("https://raw.githubusercontent.com/jflavan/pulumi-osano/main/assets/logo.png").
		WithKeywords(
			"pulumi",
			"osano",
			"consent",
			"cookie-consent",
			"privacy",
			"cmp",
			"category/infrastructure",
			"kind/native",
		).
		WithPluginDownloadURL("github://api.github.com/jflavan/pulumi-osano").
		WithNamespace(Name).
		WithConfig(infer.Config(&Config{})).
		WithResources(
			infer.Resource(&CookieConsentConfig{}),
			infer.Resource(&CookieConsentRule{}),
			infer.Resource(&CookieConsentPublication{}),
			infer.Resource(&Consent{}),
		).
		WithFunctions(
			infer.Function(&GetCookieConsentConfig{}),
			infer.Function(&GetCookieConsentConfigs{}),
			infer.Function(&GetCookieConsentRules{}),
			infer.Function(&GetCookieConsentDiscoveries{}),
			infer.Function(&GetCookieConsentAuditLog{}),
			infer.Function(&GetUnifiedConsent{}),
			infer.Function(&GetSubject{}),
			infer.Function(&GetConfig{}),
			infer.Function(&GetCollections{}),
			infer.Function(&GetCollection{}),
			infer.Function(&CheckConsent{}),
			infer.Function(&GetConsentProfile{}),
			infer.Function(&GetSubjectProfile{}),
			infer.Function(&GetSession{}),
			infer.Function(&SendSubjectCode{}),
			infer.Function(&VerifySubjectCode{}),
		).
		WithModuleMap(map[tokens.ModuleName]tokens.ModuleName{
			"provider": "index",
		}).
		// WithLanguageMap replaces go-provider's per-language defaults, so every option the SDKs rely on
		// is listed here, including the Go import path.
		WithLanguageMap(map[string]any{
			"csharp": map[string]any{
				"rootNamespace":        "Community.Pulumi",
				"respectSchemaVersion": true,
			},
			"go": map[string]any{
				"importBasePath":                 "github.com/jflavan/pulumi-osano/sdk/go/osano",
				"generateResourceContainerTypes": true,
				// Bakes the SDK version into the Go SDK so Go programs request the matching plugin.
				"respectSchemaVersion": true,
			},
			"java": map[string]any{
				"basePackage": "io.github.jflavan.pulumi",
				"buildFiles":  "gradle",
			},
			"nodejs": map[string]any{
				"packageName":          "@jflavan/pulumi-osano",
				"packageDescription":   "Pulumi provider for Osano Cookie Consent and Unified Consent APIs.",
				"respectSchemaVersion": true,
			},
			"python": map[string]any{
				"packageName":          "pulumi_osano",
				"packageDescription":   "Pulumi provider for Osano Cookie Consent and Unified Consent APIs.",
				"respectSchemaVersion": true,
				// The pulumi Python SDK itself requires Python 3.10 or later.
				"pythonRequires": ">=3.10",
				"pyproject":      map[string]any{"enabled": true},
			},
		}).
		Build()
	if err != nil {
		panic(fmt.Errorf("unable to build provider: %w", err))
	}
	// The inferred default treats every provider config change as a replacement of the provider and
	// therefore of every resource it manages; see diffProviderConfig.
	prov.DiffConfig = diffProviderConfig
	return prov
}
