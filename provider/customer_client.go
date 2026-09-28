package provider

import (
	"context"
	"net/url"
	"strings"
)

// jsonClient is the part of the Customer REST client the Cookie Consent code depends on, so tests
// can drive publication and pagination without a configured provider.
type jsonClient interface {
	DoJSON(context.Context, string, string, url.Values, any, any) error
}

// providerUserAgent identifies this provider and its version to the Osano APIs, for example
// pulumi-osano/0.1.0.
func providerUserAgent() string {
	return userAgentForVersion(Version)
}

// userAgentForVersion builds the provider user agent from a build version. Release builds are
// stamped with the bare version (0.1.0), but a leading "v" is dropped so a tag-stamped build sends
// the same pulumi-osano/0.1.0.
func userAgentForVersion(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" {
		return "pulumi-osano/dev"
	}
	return "pulumi-osano/" + version
}
