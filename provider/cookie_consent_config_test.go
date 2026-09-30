package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func TestCookieConsentConfigCheck(t *testing.T) {
	t.Parallel()

	resource := &CookieConsentConfig{}
	ctx := context.Background()

	t.Run("valid inputs", func(t *testing.T) {
		inputs := property.NewMap(validConfigCheckInputValues())
		resp, err := resource.Check(ctx, infer.CheckRequest{NewInputs: inputs})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Failures) != 0 {
			t.Fatalf("expected no failures, got: %#v", resp.Failures)
		}
	})

	for _, propertyName := range []string{"name", "domains", "mode", "configuration"} {
		t.Run("computed "+propertyName+" defers validation", func(t *testing.T) {
			values := validConfigCheckInputValues()
			values[propertyName] = property.New(property.Computed)
			resp, err := resource.Check(ctx, infer.CheckRequest{NewInputs: property.NewMap(values)})
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.Failures) != 0 {
				t.Fatalf("computed %s must not fail validation: %#v", propertyName, resp.Failures)
			}
		})
	}

	cases := []struct {
		name       string
		inputs     property.Map
		failureKey string
	}{
		{
			name:       "missing name",
			inputs:     property.NewMap(deleteKey(validConfigCheckInputValues(), "name")),
			failureKey: "name",
		},
		{
			name:       "missing domains",
			inputs:     property.NewMap(deleteKey(validConfigCheckInputValues(), "domains")),
			failureKey: "domains",
		},
		{
			name:       "missing mode",
			inputs:     property.NewMap(deleteKey(validConfigCheckInputValues(), "mode")),
			failureKey: "mode",
		},
		{
			name: "missing storagePolicyHref",
			inputs: property.NewMap(func() map[string]property.Value {
				values := validConfigCheckInputValues()
				values["configuration"] = property.New(map[string]property.Value{
					"foo": property.New("bar"),
				})
				return values
			}()),
			failureKey: "configuration.storagePolicyHref",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := resource.Check(ctx, infer.CheckRequest{NewInputs: tc.inputs})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(resp.Failures) == 0 {
				t.Fatalf("expected failures for %s, got none", tc.failureKey)
			}
			assertFailureProperty(t, resp.Failures, tc.failureKey)
		})
	}
}

func TestCookieConsentConfigDiff(t *testing.T) {
	t.Parallel()

	resource := &CookieConsentConfig{}
	ctx := context.Background()

	t.Run("same inputs", func(t *testing.T) {
		state := baseConfigState()
		inputs := baseConfigArgs()
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentConfigArgs, CookieConsentConfigState]{
			State:  state,
			Inputs: inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.HasChanges {
			t.Fatalf("expected no changes, got: %#v", resp.DetailedDiff)
		}
	})

	t.Run("changed name", func(t *testing.T) {
		state := baseConfigState()
		inputs := baseConfigArgs()
		inputs.Name = "updated"
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentConfigArgs, CookieConsentConfigState]{
			State:  state,
			Inputs: inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDiffKind(t, resp, "name", p.Update)
	})

	t.Run("changed mode", func(t *testing.T) {
		state := baseConfigState()
		inputs := baseConfigArgs()
		inputs.Mode = "production"
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentConfigArgs, CookieConsentConfigState]{
			State:  state,
			Inputs: inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDiffKind(t, resp, "mode", p.Update)
	})

	t.Run("changed domains", func(t *testing.T) {
		state := baseConfigState()
		inputs := baseConfigArgs()
		inputs.Domains = []string{"shop.example.com"}
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentConfigArgs, CookieConsentConfigState]{
			State:  state,
			Inputs: inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDiffKind(t, resp, "domains", p.Update)
	})

	t.Run("changed orgIds", func(t *testing.T) {
		state := baseConfigState()
		inputs := baseConfigArgs()
		inputs.OrgIDs = []string{"org-123", "org-456"}
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentConfigArgs, CookieConsentConfigState]{
			State:  state,
			Inputs: inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDiffKind(t, resp, "orgIds", p.Update)
	})

	t.Run("changed configuration key", func(t *testing.T) {
		state := baseConfigState()
		inputs := baseConfigArgs()
		inputs.Configuration = map[string]any{
			"storagePolicyHref": "https://example.com/policy",
			"flag":              false,
		}
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentConfigArgs, CookieConsentConfigState]{
			State:  state,
			Inputs: inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDiffKind(t, resp, "configuration", p.Update)
	})

	t.Run("removed configuration key", func(t *testing.T) {
		state := baseConfigState()
		inputs := baseConfigArgs()
		inputs.Configuration = map[string]any{
			"storagePolicyHref": "https://example.com/storage-policy",
		}
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentConfigArgs, CookieConsentConfigState]{
			State: state, Inputs: inputs,
		})
		if err != nil {
			t.Fatal(err)
		}
		assertDiffKind(t, resp, "configuration", p.Update)
	})
}

func TestCookieConsentConfigReadLifecycle(t *testing.T) {
	t.Run("read returns empty ID when config is missing", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodGet, "/v1/cookie-consent/configs/config-404")
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-404",
			Urn:        cmpURN("CookieConsentConfig", "missing"),
			Properties: configStateProperties(),
			Inputs:     emptyConfigInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "" {
			t.Fatalf("expected missing resource ID, got %q", resp.ID)
		}
	})

	t.Run("import read reconstructs inputs and state", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodGet, "/v1/cookie-consent/configs/config-123")
			writeCMPConfigResponse(t, w)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-123",
			Urn:        cmpURN("CookieConsentConfig", "imported"),
			Properties: configStateProperties(),
			Inputs:     emptyConfigInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "config-123" {
			t.Fatalf("expected config ID, got %q", resp.ID)
		}
		assertCMPConfigProperties(t, resp.Properties)
		assertCMPConfigInputs(t, resp.Inputs)
	})

	t.Run("create sends config payload and returns response metadata", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodPost, "/v1/cookie-consent/configs")
			assertCMPRequestBody(t, r)
			writeCMPConfigResponse(t, w)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Create(p.CreateRequest{
			Urn:        cmpURN("CookieConsentConfig", "created"),
			Properties: configInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "config-123" {
			t.Fatalf("expected config ID, got %q", resp.ID)
		}
		assertCMPConfigProperties(t, resp.Properties)
	})

	t.Run("update sends config payload and returns response metadata", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodPatch, "/v1/cookie-consent/configs/config-123")
			assertCMPRequestBody(t, r)
			writeCMPConfigResponse(t, w)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Update(p.UpdateRequest{
			ID:     "config-123",
			Urn:    cmpURN("CookieConsentConfig", "updated"),
			State:  configStateProperties(),
			Inputs: configInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		assertCMPConfigProperties(t, resp.Properties)
	})

	t.Run("delete makes no upstream request", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			t.Fatalf("delete must not call upstream, received %s %s", r.Method, r.URL.Path)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		err := server.Delete(p.DeleteRequest{
			ID:         "config-123",
			Urn:        cmpURN("CookieConsentConfig", "deleted"),
			Properties: configStateProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func assertCMPRequest(t *testing.T, r *http.Request, method, path string) {
	t.Helper()
	if r.Method != method {
		t.Fatalf("expected %s request, got %s", method, r.Method)
	}
	if r.URL.Path != path {
		t.Fatalf("expected path %q, got %q", path, r.URL.Path)
	}
	if got := r.Header.Get("x-osano-api-key"); got != "test-osano-key" {
		t.Fatalf("expected x-osano-api-key header, got %q", got)
	}
}

func assertCMPRequestBody(t *testing.T, r *http.Request) {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"name":    "cookie-consent",
		"domains": []any{"example.com"},
		"mode":    "debug",
		"orgIds":  []any{"org-123"},
		"configuration": map[string]any{
			"storagePolicyHref": "https://example.com/storage-policy",
			"flag":              true,
		},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("unexpected request body: %#v", body)
	}
}

func writeCMPConfigResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cmpConfigResponseFixture()); err != nil {
		t.Fatal(err)
	}
}

func assertCMPConfigProperties(t *testing.T, properties property.Map) {
	t.Helper()
	assertCMPConfigInputs(t, properties)
	if got := properties.Get("configId").AsString(); got != "config-123" {
		t.Fatalf("expected config ID metadata, got %q", got)
	}
	if got := properties.Get("customerId").AsString(); got != "customer-123" {
		t.Fatalf("expected customer ID metadata, got %q", got)
	}
	if got := int(properties.Get("publishedRevision").AsNumber()); got != 7 {
		t.Fatalf("expected published revision metadata, got %d", got)
	}
}

func assertCMPConfigInputs(t *testing.T, properties property.Map) {
	t.Helper()
	if got := properties.Get("name").AsString(); got != "cookie-consent" {
		t.Fatalf("expected name, got %q", got)
	}
	if got := properties.Get("domains").AsArray().Get(0).AsString(); got != "example.com" {
		t.Fatalf("expected domain, got %q", got)
	}
	if got := properties.Get("mode").AsString(); got != "debug" {
		t.Fatalf("expected mode, got %q", got)
	}
	if got := properties.Get("orgIds").AsArray().Get(0).AsString(); got != "org-123" {
		t.Fatalf("expected organization ID, got %q", got)
	}
	configuration := properties.Get("configuration").AsMap()
	if got := configuration.Get("storagePolicyHref").AsString(); got != "https://example.com/storage-policy" {
		t.Fatalf("expected configuration storage policy URL, got %q", got)
	}
}

func configInputProperties() property.Map {
	return property.NewMap(map[string]property.Value{
		"name":    property.New("cookie-consent"),
		"domains": property.New([]property.Value{property.New("example.com")}),
		"mode":    property.New("debug"),
		"orgIds":  property.New([]property.Value{property.New("org-123")}),
		"configuration": property.New(property.NewMap(map[string]property.Value{
			"storagePolicyHref": property.New("https://example.com/storage-policy"),
			"flag":              property.New(true),
		})),
	})
}

func configStateProperties() property.Map {
	return configInputProperties().Set("configId", property.New("config-123"))
}

func emptyConfigInputProperties() property.Map {
	return property.NewMap(map[string]property.Value{
		"name":          {},
		"domains":       {},
		"mode":          {},
		"orgIds":        {},
		"configuration": {},
	})
}

func cmpConfigResponseFixture() cmpConfigResponse {
	return cmpConfigResponse{
		Name:                "cookie-consent",
		Domains:             []string{"example.com"},
		Mode:                "debug",
		OrgIDs:              []string{"org-123"},
		Configuration:       map[string]any{"storagePolicyHref": "https://example.com/storage-policy", "flag": true},
		ConfigID:            "config-123",
		CustomerID:          "customer-123",
		Created:             100,
		Updated:             200,
		PublishStatus:       "published",
		LastPublished:       300,
		PublishedRevision:   7,
		TattleRecordStopped: true,
	}
}

func validConfigCheckInputValues() map[string]property.Value {
	return map[string]property.Value{
		"name": property.New("cookie-consent"),
		"domains": property.New([]property.Value{
			property.New("example.com"),
		}),
		"mode": property.New("debug"),
		"configuration": property.New(map[string]property.Value{
			"storagePolicyHref": property.New("https://example.com/storage-policy"),
		}),
	}
}

func deleteKey(values map[string]property.Value, key string) map[string]property.Value {
	delete(values, key)
	return values
}

func assertFailureProperty(t *testing.T, failures []p.CheckFailure, propertyKey string) {
	t.Helper()
	for _, failure := range failures {
		if failure.Property == propertyKey {
			return
		}
	}
	t.Fatalf("expected failure for %q, got %#v", propertyKey, failures)
}

func assertDiffKind(t *testing.T, resp p.DiffResponse, key string, want p.DiffKind) {
	t.Helper()
	if !resp.HasChanges {
		t.Fatalf("expected changes, got none")
	}
	diff, ok := resp.DetailedDiff[key]
	if !ok {
		t.Fatalf("expected diff for %q, got %#v", key, resp.DetailedDiff)
	}
	if diff.Kind != want {
		t.Fatalf("expected %q diff kind %v, got %v", key, want, diff.Kind)
	}
}

func baseConfigArgs() CookieConsentConfigArgs {
	return CookieConsentConfigArgs{
		Name:    "cookie-consent",
		Domains: []string{"example.com"},
		Mode:    "debug",
		OrgIDs:  []string{"org-123"},
		Configuration: map[string]any{
			"storagePolicyHref": "https://example.com/storage-policy",
			"flag":              true,
		},
	}
}

func baseConfigState() CookieConsentConfigState {
	return CookieConsentConfigState{CookieConsentConfigArgs: baseConfigArgs(), ConfigID: "config-123"}
}

// Osano reports additionalLinks: [] for a configuration without additional links, which its spec
// rejects on write. An import leaves it out, so the first change to the configuration passes Check.
func TestCookieConsentConfigArgsFromResponseOmitsEmptyAdditionalLinks(t *testing.T) {
	t.Parallel()

	resp := cmpConfigResponseFixture()
	resp.Configuration["additionalLinks"] = []any{}

	t.Run("import", func(t *testing.T) {
		args := cookieConsentConfigArgsFromResponse(resp, CookieConsentConfigArgs{})
		if _, present := args.Configuration["additionalLinks"]; present {
			t.Fatalf("expected an empty additionalLinks to be left out, got %#v", args.Configuration)
		}
		if args.Configuration["flag"] != true {
			t.Fatalf("expected the other keys to be adopted, got %#v", args.Configuration)
		}
		if _, present := resp.Configuration["additionalLinks"]; !present {
			t.Fatal("the response must not be modified")
		}
	})

	t.Run("import keeps links that are present", func(t *testing.T) {
		linked := cmpConfigResponseFixture()
		linked.Configuration["additionalLinks"] = []any{[]any{"imprint", "/imprint"}}
		args := cookieConsentConfigArgsFromResponse(linked, CookieConsentConfigArgs{})
		if !reflect.DeepEqual(args.Configuration["additionalLinks"], linked.Configuration["additionalLinks"]) {
			t.Fatalf("expected additionalLinks to be adopted, got %#v", args.Configuration)
		}
	})

	// A refresh surfaces links cleared in the dashboard as drift from the declared ones.
	t.Run("refresh of declared links", func(t *testing.T) {
		declared := baseConfigArgs()
		declared.Configuration["additionalLinks"] = []any{[]any{"imprint", "/imprint"}}
		args := cookieConsentConfigArgsFromResponse(resp, declared)
		if links, isList := args.Configuration["additionalLinks"].([]any); !isList || len(links) != 0 {
			t.Fatalf("expected refresh to adopt the empty list, got %#v", args.Configuration)
		}
	})
}
