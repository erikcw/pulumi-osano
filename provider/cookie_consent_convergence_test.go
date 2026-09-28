package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func decodeJSONBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

// Osano documents orgIds as optional ("if omitted or empty, the configuration is associated with
// the root organization"); a JSON null is neither and may be rejected.
func TestCookieConsentConfigOrgIDsPayload(t *testing.T) {
	t.Run("create omits orgIds when unset", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodPost, "/v1/cookie-consent/configs")
			if body := decodeJSONBody(t, r); body["orgIds"] != nil {
				t.Fatalf("expected orgIds to be omitted, got %#v", body["orgIds"])
			} else if _, present := body["orgIds"]; present {
				t.Fatal("expected orgIds to be omitted, got an explicit null")
			}
			writeCMPConfigResponse(t, w)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		if _, err := server.Create(p.CreateRequest{
			Urn:        cmpURN("CookieConsentConfig", "no-orgs"),
			Properties: configInputProperties().Delete("orgIds"),
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("update sends an empty list to clear previously set orgIds", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodPatch, "/v1/cookie-consent/configs/config-123")
			body := decodeJSONBody(t, r)
			orgIDs, present := body["orgIds"]
			if !present || orgIDs == nil {
				t.Fatalf("expected an explicit empty orgIds list, got %#v (present=%v)", orgIDs, present)
			}
			if list, ok := orgIDs.([]any); !ok || len(list) != 0 {
				t.Fatalf("expected an empty orgIds list, got %#v", orgIDs)
			}
			writeCMPConfigResponse(t, w)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		if _, err := server.Update(p.UpdateRequest{
			ID:     "config-123",
			Urn:    cmpURN("CookieConsentConfig", "clear-orgs"),
			State:  configStateProperties(),
			Inputs: configInputProperties().Delete("orgIds"),
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("update omits orgIds when never set", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodPatch, "/v1/cookie-consent/configs/config-123")
			if _, present := decodeJSONBody(t, r)["orgIds"]; present {
				t.Fatal("expected orgIds to be omitted")
			}
			writeCMPConfigResponse(t, w)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		if _, err := server.Update(p.UpdateRequest{
			ID:     "config-123",
			Urn:    cmpURN("CookieConsentConfig", "still-no-orgs"),
			State:  configStateProperties().Delete("orgIds"),
			Inputs: configInputProperties().Delete("orgIds"),
		}); err != nil {
			t.Fatal(err)
		}
	})
}

// Osano may normalize the CMP configuration object (for example by adding default keys). State
// must track only the keys the program declares, or every `pulumi up` would report a diff and
// PATCH the config again, marking the publication outdated each time.
func TestCookieConsentConfigConfigurationConverges(t *testing.T) {
	normalizedFixture := func() cmpConfigResponse {
		fixture := cmpConfigResponseFixture()
		fixture.Configuration = map[string]any{
			"storagePolicyHref":  "https://example.com/storage-policy",
			"flag":               true,
			"crossDomainEnabled": false,
		}
		return fixture
	}

	t.Run("create keeps only declared configuration keys", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, normalizedFixture())
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Create(p.CreateRequest{
			Urn:        cmpURN("CookieConsentConfig", "normalized"),
			Properties: configInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		configuration := resp.Properties.Get("configuration").AsMap()
		if _, present := configuration.GetOk("crossDomainEnabled"); present {
			t.Fatalf("state must not carry server-added configuration keys: %#v", configuration)
		}
		if !configuration.Get("flag").AsBool() {
			t.Fatalf("expected declared keys to be kept: %#v", configuration)
		}
	})

	t.Run("update keeps only declared configuration keys", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, normalizedFixture())
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Update(p.UpdateRequest{
			ID:     "config-123",
			Urn:    cmpURN("CookieConsentConfig", "normalized-update"),
			State:  configStateProperties(),
			Inputs: configInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, present := resp.Properties.Get("configuration").AsMap().GetOk("crossDomainEnabled"); present {
			t.Fatalf("state must not carry server-added configuration keys: %#v", resp.Properties)
		}
	})

	t.Run("refresh projects Osano values onto declared keys", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fixture := normalizedFixture()
			fixture.Configuration["storagePolicyHref"] = "https://example.com/changed"
			writeJSON(t, w, fixture)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-123",
			Urn:        cmpURN("CookieConsentConfig", "refresh"),
			Properties: configStateProperties(),
			Inputs:     configInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, configuration := range []property.Map{
			resp.Inputs.Get("configuration").AsMap(),
			resp.Properties.Get("configuration").AsMap(),
		} {
			if _, present := configuration.GetOk("crossDomainEnabled"); present {
				t.Fatalf("refresh must not adopt server-added configuration keys: %#v", configuration)
			}
			if got := configuration.Get("storagePolicyHref").AsString(); got != "https://example.com/changed" {
				t.Fatalf("refresh must surface drift in declared keys, got %q", got)
			}
			if !configuration.Get("flag").AsBool() {
				t.Fatalf("expected declared keys to be kept: %#v", configuration)
			}
		}
	})

	t.Run("refresh keeps declared keys Osano omits", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fixture := cmpConfigResponseFixture()
			fixture.Configuration = map[string]any{"storagePolicyHref": "https://example.com/storage-policy"}
			writeJSON(t, w, fixture)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-123",
			Urn:        cmpURN("CookieConsentConfig", "refresh-omitted"),
			Properties: configStateProperties(),
			Inputs:     configInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		flag, present := resp.Inputs.Get("configuration").AsMap().GetOk("flag")
		if !present || !flag.AsBool() {
			t.Fatalf("expected the declared flag key to be kept, got %#v", resp.Inputs.Get("configuration"))
		}
	})

	t.Run("import reads the full Osano configuration", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, normalizedFixture())
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-123",
			Urn:        cmpURN("CookieConsentConfig", "import-full"),
			Properties: configStateProperties(),
			Inputs:     emptyConfigInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, present := resp.Inputs.Get("configuration").AsMap().GetOk("crossDomainEnabled"); !present {
			t.Fatalf("import must adopt every server configuration key, got %#v", resp.Inputs.Get("configuration"))
		}
	})
}

// Optional rule fields the program leaves unset stay unmanaged: a server default must not create
// a perpetual diff that PATCHes an explicit null on every `pulumi up`.
func TestCookieConsentRuleOptionalFieldsConverge(t *testing.T) {
	sparseInputs := func() property.Map {
		return property.NewMap(map[string]property.Value{
			"configId":       property.New("config-abc"),
			"storeType":      property.New("cookies"),
			"classification": property.New("ANALYTICS"),
			"rule":           property.New("_ga"),
			"disclosure":     property.New(true),
		})
	}
	optionalKeys := []string{"title", "vendorName", "ruleType", "description", "expiry"}

	t.Run("create keeps unset optional fields null when Osano defaults them", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{cmpRuleResponseFixture()}})
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Create(p.CreateRequest{
			Urn:        cmpURN("CookieConsentRule", "sparse"),
			Properties: sparseInputs(),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range optionalKeys {
			if !resp.Properties.Get(key).IsNull() {
				t.Fatalf("expected unset %s to stay null, got %#v", key, resp.Properties.Get(key))
			}
		}
		if got := resp.Properties.Get("ruleId").AsNumber(); got != 42 {
			t.Fatalf("expected server rule ID 42, got %v", got)
		}
	})

	t.Run("refresh keeps unset optional fields null and adopts managed values", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fixture := cmpRuleResponseFixture()
			fixture.Classification = "MARKETING"
			writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{fixture}})
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-abc/42",
			Urn:        cmpURN("CookieConsentRule", "sparse-refresh"),
			Properties: sparseInputs().Set("ruleId", property.New(42.0)),
			Inputs:     sparseInputs(),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range optionalKeys {
			if !resp.Inputs.Get(key).IsNull() {
				t.Fatalf("expected unset %s to stay null after refresh, got %#v", key, resp.Inputs.Get(key))
			}
		}
		if got := resp.Inputs.Get("classification").AsString(); got != "MARKETING" {
			t.Fatalf("expected refresh to surface classification drift, got %q", got)
		}
	})

	// An update must not send null for a field the program never managed: Osano would clear the
	// default or dashboard value that Create allowed and refresh deliberately never reads.
	t.Run("update omits optional fields the program never managed", func(t *testing.T) {
		var body atomic.Value
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodPatch, "/v1/cookie-consent/rules/42")
			body.Store(decodeJSONBody(t, r))
			fixture := cmpRuleResponseFixture()
			fixture.Classification = "MARKETING"
			writeJSON(t, w, fixture)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		if _, err := server.Update(p.UpdateRequest{
			ID:     "config-abc/42",
			Urn:    cmpURN("CookieConsentRule", "sparse-update"),
			State:  sparseInputs().Set("ruleId", property.New(42.0)),
			Inputs: sparseInputs().Set("classification", property.New("MARKETING")),
		}); err != nil {
			t.Fatal(err)
		}
		sent, _ := body.Load().(map[string]any)
		for _, key := range optionalKeys {
			if _, present := sent[key]; present {
				t.Fatalf("update must not send %s for a field the program never set: %v", key, sent)
			}
		}
		if sent["classification"] != "MARKETING" {
			t.Fatalf("expected the changed classification to be sent, got %v", sent)
		}
	})

	t.Run("update clears a field the program removed", func(t *testing.T) {
		var body atomic.Value
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body.Store(decodeJSONBody(t, r))
			writeJSON(t, w, cmpRuleResponseFixture())
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		if _, err := server.Update(p.UpdateRequest{
			ID:     "config-abc/42",
			Urn:    cmpURN("CookieConsentRule", "cleared-title"),
			State:  sparseInputs().Set("ruleId", property.New(42.0)).Set("title", property.New("Analytics")),
			Inputs: sparseInputs(),
		}); err != nil {
			t.Fatal(err)
		}
		sent, _ := body.Load().(map[string]any)
		if value, present := sent["title"]; !present || value != nil {
			t.Fatalf("expected title to be cleared with an explicit null, got %v", sent)
		}
		if _, present := sent["vendorName"]; present {
			t.Fatalf("a never-managed field must still be omitted, got %v", sent)
		}
	})

	t.Run("refresh adopts Osano values for declared optional fields", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fixture := cmpRuleResponseFixture()
			renamed := "Renamed in Osano"
			fixture.Title = &renamed
			writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{fixture}})
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-abc/42",
			Urn:        cmpURN("CookieConsentRule", "declared-refresh"),
			Properties: ruleStateProperties(),
			Inputs:     ruleInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := resp.Inputs.Get("title").AsString(); got != "Renamed in Osano" {
			t.Fatalf("expected refresh to surface title drift, got %q", got)
		}
	})
}

// A create POST has already succeeded by the time the response is decoded; failing on an
// unexpected rule type would orphan the new rule and create a duplicate on the next `pulumi up`.
func TestCookieConsentRuleCreateToleratesUnknownResponseType(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fixture := cmpRuleResponseFixture()
		fixture.Type = "pixel"
		writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{fixture}})
	}))
	defer api.Close()

	server := newCMPProviderServer(t, api.URL)
	resp, err := server.Create(p.CreateRequest{
		Urn:        cmpURN("CookieConsentRule", "unknown-type"),
		Properties: ruleInputProperties(),
	})
	if err != nil {
		t.Fatalf("create must keep the rule tracked despite an unknown response type: %v", err)
	}
	if resp.ID != "config-abc/42" {
		t.Fatalf("expected composite rule ID, got %q", resp.ID)
	}
	if got := resp.Properties.Get("storeType").AsString(); got != "cookies" {
		t.Fatalf("expected the declared storeType to be kept, got %q", got)
	}
}

func TestCookieConsentPublicationCheckDefersComputedWebhookURL(t *testing.T) {
	t.Parallel()

	resp, err := (&CookieConsentPublication{}).Check(context.Background(), infer.CheckRequest{
		NewInputs: property.NewMap(map[string]property.Value{
			"configId":    property.New("config-id"),
			"changeToken": property.New("desired-state-v1"),
			"webhookUrl":  property.New(property.Computed),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range resp.Failures {
		if failure.Property == "webhookUrl" {
			t.Fatalf("computed webhookUrl must not fail validation: %#v", resp.Failures)
		}
	}
}

func TestCookieConsentPublicationCheckTrimsIdentifiers(t *testing.T) {
	t.Parallel()

	resp, err := (&CookieConsentPublication{}).Check(context.Background(), infer.CheckRequest{
		NewInputs: property.NewMap(map[string]property.Value{
			"configId":    property.New(" config-id "),
			"changeToken": property.New(" desired-state-v1 "),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Failures) != 0 {
		t.Fatalf("expected no failures, got %#v", resp.Failures)
	}
	if resp.Inputs.ConfigID != "config-id" {
		t.Fatalf("expected trimmed configId, got %q", resp.Inputs.ConfigID)
	}
	if resp.Inputs.ChangeToken != "desired-state-v1" {
		t.Fatalf("expected trimmed changeToken, got %q", resp.Inputs.ChangeToken)
	}
}

// A resource-level customTimeouts longer than the provider default is an explicit user choice.
func TestCookieConsentPublicationTimeoutHonorsLongerEngineDeadline(t *testing.T) {
	engineDeadline := time.Now().Add(45 * time.Minute)
	ctx, engineCancel := context.WithDeadline(t.Context(), engineDeadline)
	defer engineCancel()

	publicationCtx, cancel := withPublicationTimeout(ctx, 20*time.Minute)
	defer cancel()
	got, ok := publicationCtx.Deadline()
	if !ok {
		t.Fatal("expected publication context deadline")
	}
	if !got.Equal(engineDeadline) {
		t.Fatalf("expected the engine deadline %s to be honored, got %s", engineDeadline, got)
	}
}
