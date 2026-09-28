package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// Marking an input secret in the schema changes how the SDKs send it: the generated code wraps the
// value in a secret. State written by an earlier release holds the plain value, so the first diff
// after an SDK upgrade compares a plain old input with a secret new one. That must not count as a
// change: for Consent it would be a replacement, which submits the consent again.
func TestSecretInputsDoNotDiff(t *testing.T) {
	t.Run("Consent subject", func(t *testing.T) {
		mock := &ucMockAPI{t: t, status: http.StatusCreated}
		api := httptest.NewServer(mock)
		defer api.Close()

		server := newUCProviderServer(t, api.URL)
		inputs := consentInputs()
		urn := cmpURN("Consent", "secret-subject")
		created, err := server.Create(p.CreateRequest{Urn: urn, Properties: inputs})
		if err != nil {
			t.Fatal(err)
		}
		diff, err := server.Diff(p.DiffRequest{
			Urn: urn, ID: created.ID, State: created.Properties,
			Inputs: inputs.Set("subject", inputs.Get("subject").WithSecret(true)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if diff.HasChanges {
			t.Fatalf("a secret subject must not diff against the plain one in state, got %#v", diff.DetailedDiff)
		}
	})

	t.Run("CookieConsentPublication webhookUrl", func(t *testing.T) {
		server := newCMPProviderServer(t, "https://127.0.0.1:1")
		inputs := property.NewMap(map[string]property.Value{
			"configId":    property.New("config-id"),
			"changeToken": property.New("desired-state-v1"),
			"webhookUrl":  property.New("https://example.com/publish-complete"),
		})
		state := inputs.
			Set("customerId", property.New("customer-id")).
			Set("lastPublished", property.New(1.0)).
			Set("publishedRevision", property.New(1.0)).
			Set("publishStatus", property.New("published")).
			Set("scriptSrc", property.New("https://cmp.osano.com/customer-id/config-id/osano.js")).
			Set("scriptTag", property.New(`<script src="https://cmp.osano.com/customer-id/config-id/osano.js"></script>`))
		diff, err := server.Diff(p.DiffRequest{
			Urn: cmpURN("CookieConsentPublication", "secret-webhook"), ID: "config-id", State: state,
			Inputs: inputs.Set("webhookUrl", inputs.Get("webhookUrl").WithSecret(true)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if diff.HasChanges {
			t.Fatalf("a secret webhookUrl must not diff against the plain one in state, got %#v", diff.DetailedDiff)
		}
	})
}
