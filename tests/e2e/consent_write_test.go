//go:build e2e && consentwrite

package e2e

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/jflavan/pulumi-osano/tests/e2e/internal/testenv"
)

// TestCreateConsentRecord submits a consent through the Consent resource, then reads the subject's
// unified consent back and checks that the trace attribute the resource sent was recorded.
func TestCreateConsentRecord(t *testing.T) {
	testenv.RequireOptIn(t, testenv.EnvRunWriteE2E, "enable consent write tests")
	server := providerServer(t)

	action := testenv.Require(t, testenv.EnvWriteAction, "consent action (ACCEPT, REJECT, or UNSELECTED)")
	privacyProtocolID := testenv.Require(t, testenv.EnvWritePrivacyProtocolID, "privacy protocol identifier")
	configID := testenv.Require(t, testenv.EnvTestConfigID, "configuration identifier for vendor field")
	subjectField, subjectRef := consentSubject(t)

	traceID := uuid.NewString()
	inputs := property.NewMap(map[string]property.Value{
		"subject": property.New(map[string]property.Value{subjectField: property.New(subjectRef)}),
		"actions": property.New([]property.Value{property.New(map[string]property.Value{
			"target": property.New(privacyProtocolID),
			"vendor": property.New(configID),
			"action": property.New(action),
		})}),
		"attributes": property.New(map[string]property.Value{"pulumiTraceId": property.New(traceID)}),
		"origin":     property.New("api"),
	})

	checked, err := server.Check(p.CheckRequest{Urn: consentURN("write"), Inputs: inputs})
	if err != nil {
		t.Fatalf("check the consent inputs: %v", err)
	}
	if len(checked.Failures) != 0 {
		t.Fatalf("the consent inputs were rejected: %v", checked.Failures)
	}
	created, err := server.Create(p.CreateRequest{Urn: consentURN("write"), Properties: checked.Inputs})
	if err != nil {
		t.Fatalf("create consent failed: %v", err)
	}
	if created.ID == "" || stringOutput(t, created.Properties, "consentId") == "" {
		t.Fatal("the created consent has no ID")
	}

	// Osano's ref parameter accepts only subject and session: verified and anonymous IDs are both
	// subject references.
	readback := invoke(t, server, "getUnifiedConsent", map[string]property.Value{
		"subjectRef": property.New(subjectRef), "referenceType": property.New("subject"),
	})
	if !boolOutput(t, readback, "exists") {
		t.Fatal("unified consent state missing after write")
	}
	attributes := mapOutput(t, mapOutput(t, readback, "unifiedConsent"), "attributes")
	if value := attributes.Get("pulumiTraceId"); !value.IsString() || value.AsString() != traceID {
		t.Fatalf("expected pulumiTraceId attribute %s, got %#v", traceID, value)
	}
}

// consentSubject returns the Consent subject field to set (verifiedId or anonymousId) and its value.
func consentSubject(t *testing.T) (field, value string) {
	t.Helper()
	subjectType := strings.ToLower(testenv.Require(t, testenv.EnvWriteSubjectType, "subject type (verified|anonymous)"))
	subjectValue := testenv.Require(t, testenv.EnvWriteSubjectValue, "subject identifier value")

	switch subjectType {
	case "verified", "verifiedid", "subject":
		return "verifiedId", subjectValue
	case "anonymous", "anonymousid":
		return "anonymousId", subjectValue
	default:
		t.Fatalf("unsupported subject type %q", subjectType)
		return "", ""
	}
}
