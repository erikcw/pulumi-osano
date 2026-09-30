package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func TestCookieConsentRuleCheck(t *testing.T) {
	t.Parallel()

	resource := &CookieConsentRule{}
	ctx := context.Background()

	t.Run("length limits count characters, not bytes", func(t *testing.T) {
		values := validRuleCheckInputValues()
		values["title"] = property.New(strings.Repeat("標", 64))
		values["vendorName"] = property.New(strings.Repeat("é", 100))
		resp, err := resource.Check(ctx, infer.CheckRequest{NewInputs: property.NewMap(values)})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Failures) != 0 {
			t.Fatalf("expected multibyte values at the character limit to pass, got %#v", resp.Failures)
		}
	})

	t.Run("valid inputs", func(t *testing.T) {
		inputs := property.NewMap(validRuleCheckInputValues())
		resp, err := resource.Check(ctx, infer.CheckRequest{NewInputs: inputs})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Failures) != 0 {
			t.Fatalf("expected no failures, got: %#v", resp.Failures)
		}
	})

	for _, ruleType := range []string{
		"FILENAME", "DOMAIN", "PATH", "REGEXP", "STARTS_WITH", "ENDS_WITH", "CONTAINS", "EXACT_MATCH",
	} {
		t.Run("valid ruleType "+ruleType, func(t *testing.T) {
			values := validRuleCheckInputValues()
			values["ruleType"] = property.New(ruleType)
			resp, err := resource.Check(ctx, infer.CheckRequest{NewInputs: property.NewMap(values)})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(resp.Failures) != 0 {
				t.Fatalf("expected no failures, got: %#v", resp.Failures)
			}
		})
	}

	for _, propertyName := range []string{
		"configId", "storeType", "classification", "rule",
		"title", "vendorName", "ruleType", "description", "expiry",
	} {
		t.Run("computed "+propertyName+" defers validation", func(t *testing.T) {
			values := validRuleCheckInputValues()
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
			name:       "missing configId",
			inputs:     property.NewMap(deleteKey(validRuleCheckInputValues(), "configId")),
			failureKey: "configId",
		},
		{
			name:       "missing storeType",
			inputs:     property.NewMap(deleteKey(validRuleCheckInputValues(), "storeType")),
			failureKey: "storeType",
		},
		{
			name:       "missing classification",
			inputs:     property.NewMap(deleteKey(validRuleCheckInputValues(), "classification")),
			failureKey: "classification",
		},
		{
			name:       "missing rule",
			inputs:     property.NewMap(deleteKey(validRuleCheckInputValues(), "rule")),
			failureKey: "rule",
		},
		{
			name: "invalid classification",
			inputs: property.NewMap(func() map[string]property.Value {
				values := validRuleCheckInputValues()
				values["classification"] = property.New("INVALID")
				return values
			}()),
			failureKey: "classification",
		},
		{
			name: "invalid storeType",
			inputs: property.NewMap(func() map[string]property.Value {
				values := validRuleCheckInputValues()
				values["storeType"] = property.New("invalid")
				return values
			}()),
			failureKey: "storeType",
		},
		{
			name: "rule too short",
			inputs: property.NewMap(func() map[string]property.Value {
				values := validRuleCheckInputValues()
				values["rule"] = property.New("ab")
				return values
			}()),
			failureKey: "rule",
		},
		{
			name: "invalid ruleType",
			inputs: property.NewMap(func() map[string]property.Value {
				values := validRuleCheckInputValues()
				values["ruleType"] = property.New("INVALID")
				return values
			}()),
			failureKey: "ruleType",
		},
		{
			name: "title too long",
			inputs: property.NewMap(func() map[string]property.Value {
				values := validRuleCheckInputValues()
				values["title"] = property.New(strings.Repeat("t", 65))
				return values
			}()),
			failureKey: "title",
		},
		{
			name: "vendorName too long",
			inputs: property.NewMap(func() map[string]property.Value {
				values := validRuleCheckInputValues()
				values["vendorName"] = property.New(strings.Repeat("v", 101))
				return values
			}()),
			failureKey: "vendorName",
		},
		{
			name: "description too long",
			inputs: property.NewMap(func() map[string]property.Value {
				values := validRuleCheckInputValues()
				values["description"] = property.New(strings.Repeat("d", 1001))
				return values
			}()),
			failureKey: "description",
		},
		{
			name: "expiry too long",
			inputs: property.NewMap(func() map[string]property.Value {
				values := validRuleCheckInputValues()
				values["expiry"] = property.New(strings.Repeat("e", 51))
				return values
			}()),
			failureKey: "expiry",
		},
		{
			name: "description rejected for scripts",
			inputs: property.NewMap(func() map[string]property.Value {
				values := validRuleCheckInputValues()
				values["storeType"] = property.New("scripts")
				return values
			}()),
			failureKey: "description",
		},
		{
			name: "expiry rejected for scripts",
			inputs: property.NewMap(func() map[string]property.Value {
				values := validRuleCheckInputValues()
				values["storeType"] = property.New("scripts")
				return values
			}()),
			failureKey: "expiry",
		},
		// The payload never sends description for other store types, so a declared "" would sit in
		// state while refresh reads Osano's null, and every `pulumi up` would plan an update.
		{
			name: "empty description rejected for scripts",
			inputs: property.NewMap(func() map[string]property.Value {
				values := deleteKey(validRuleCheckInputValues(), "expiry")
				values["storeType"] = property.New("scripts")
				values["description"] = property.New("")
				return values
			}()),
			failureKey: "description",
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

func TestCookieConsentRuleProviderCheckAcceptsComputedConfigID(t *testing.T) {
	server := newCMPProviderServer(t, "https://customer.example.test")
	values := validRuleCheckInputValues()
	values["configId"] = property.New(property.Computed)

	resp, err := server.Check(p.CheckRequest{
		Urn:    cmpURN("CookieConsentRule", "computed-config"),
		Inputs: property.NewMap(values),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Failures) != 0 {
		t.Fatalf("computed configId must be accepted by provider Check: %#v", resp.Failures)
	}
}

func TestCookieConsentRuleDiff(t *testing.T) {
	t.Parallel()

	resource := &CookieConsentRule{}
	ctx := context.Background()

	t.Run("no changes", func(t *testing.T) {
		state := baseRuleState()
		inputs := baseRuleArgs()
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentRuleArgs, CookieConsentRuleState]{
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

	t.Run("changed classification", func(t *testing.T) {
		state := baseRuleState()
		inputs := baseRuleArgs()
		inputs.Classification = "MARKETING"
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentRuleArgs, CookieConsentRuleState]{
			State:  state,
			Inputs: inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDiffKind(t, resp, "classification", p.Update)
	})

	t.Run("changed rule", func(t *testing.T) {
		state := baseRuleState()
		inputs := baseRuleArgs()
		inputs.Rule = "new-pattern-*"
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentRuleArgs, CookieConsentRuleState]{
			State:  state,
			Inputs: inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDiffKind(t, resp, "rule", p.Update)
	})

	t.Run("changed disclosure", func(t *testing.T) {
		state := baseRuleState()
		inputs := baseRuleArgs()
		inputs.Disclosure = false
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentRuleArgs, CookieConsentRuleState]{
			State:  state,
			Inputs: inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDiffKind(t, resp, "disclosure", p.Update)
	})

	t.Run("configId change requires replace", func(t *testing.T) {
		state := baseRuleState()
		inputs := baseRuleArgs()
		inputs.ConfigID = "new-config-id"
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentRuleArgs, CookieConsentRuleState]{
			State:  state,
			Inputs: inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDiffKind(t, resp, "configId", p.UpdateReplace)
	})

	t.Run("storeType change requires replace", func(t *testing.T) {
		state := baseRuleState()
		inputs := baseRuleArgs()
		inputs.StoreType = "iframes"
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentRuleArgs, CookieConsentRuleState]{
			State:  state,
			Inputs: inputs,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertDiffKind(t, resp, "storeType", p.UpdateReplace)
	})

	for _, tc := range []struct {
		name   string
		key    string
		mutate func(*CookieConsentRuleArgs)
	}{
		{name: "changed ruleType", key: "ruleType", mutate: func(args *CookieConsentRuleArgs) {
			value := "DOMAIN"
			args.RuleType = &value
		}},
		{name: "changed description", key: "description", mutate: func(args *CookieConsentRuleArgs) {
			value := "updated description"
			args.Description = &value
		}},
		{name: "changed expiry", key: "expiry", mutate: func(args *CookieConsentRuleArgs) {
			value := "session"
			args.Expiry = &value
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := baseRuleState()
			inputs := baseRuleArgs()
			tc.mutate(&inputs)
			resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentRuleArgs, CookieConsentRuleState]{
				State:  state,
				Inputs: inputs,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertDiffKind(t, resp, tc.key, p.Update)
		})
	}
}

func TestCookieConsentRuleLifecycle(t *testing.T) {
	t.Run("create sends all supported fields and returns a composite ID", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodPost, "/v1/cookie-consent/rules")
			assertCMPRuleCreateBody(t, r)
			writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{cmpRuleResponseFixture()}})
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Create(p.CreateRequest{
			Urn:        cmpURN("CookieConsentRule", "created"),
			Properties: ruleInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "config-abc/42" {
			t.Fatalf("expected composite rule ID, got %q", resp.ID)
		}
		assertCMPRuleProperties(t, resp.Properties)
	})

	t.Run("update clears nullable fields explicitly", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodPatch, "/v1/cookie-consent/rules/42")
			assertCMPRuleClearBody(t, r)
			fixture := cmpRuleResponseFixture()
			fixture.Title = nil
			fixture.VendorName = nil
			fixture.RuleType = nil
			fixture.Description = nil
			fixture.Expiry = nil
			writeCMPRuleResponse(t, w, fixture)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Update(p.UpdateRequest{
			ID:     "config-abc/42",
			Urn:    cmpURN("CookieConsentRule", "updated"),
			State:  ruleStateProperties(),
			Inputs: clearedRuleInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"title", "vendorName", "ruleType", "description", "expiry"} {
			if !resp.Properties.Get(key).IsNull() {
				t.Fatalf("expected %s to be null, got %#v", key, resp.Properties.Get(key))
			}
		}
	})

	t.Run("import reconstructs every input from Osano", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodGet, "/v1/cookie-consent/configs/config-abc/rules")
			if got := r.URL.Query().Get("limit"); got != "500" {
				t.Fatalf("expected limit=500, got %q", got)
			}
			writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{cmpRuleResponseFixture()}})
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-abc/42",
			Urn:        cmpURN("CookieConsentRule", "imported"),
			Properties: emptyRuleStateProperties(),
			Inputs:     emptyRuleInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "config-abc/42" {
			t.Fatalf("expected canonical rule ID, got %q", resp.ID)
		}
		assertCMPRuleProperties(t, resp.Properties)
		assertCMPRuleInputs(t, resp.Inputs)
	})

	t.Run("import keeps the parsed config ID when list items omit configId", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodGet, "/v1/cookie-consent/configs/config-abc/rules")
			fixture := cmpRuleResponseFixture()
			fixture.ConfigID = ""
			writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{fixture}})
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-abc/42",
			Urn:        cmpURN("CookieConsentRule", "imported-without-config"),
			Properties: emptyRuleStateProperties(),
			Inputs:     emptyRuleInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "config-abc/42" {
			t.Fatalf("expected canonical rule ID, got %q", resp.ID)
		}
		if got := resp.Inputs.Get("configId").AsString(); got != "config-abc" {
			t.Fatalf("expected imported configId input config-abc, got %q", got)
		}
	})

	t.Run("update with an empty response keeps applied inputs", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodPatch, "/v1/cookie-consent/rules/42")
			w.WriteHeader(http.StatusNoContent)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Update(p.UpdateRequest{
			ID:     "config-abc/42",
			Urn:    cmpURN("CookieConsentRule", "empty-update"),
			State:  ruleStateProperties(),
			Inputs: ruleInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := resp.Properties.Get("ruleId").AsNumber(); got != 42 {
			t.Fatalf("expected ruleId 42 to be retained, got %v", got)
		}
		if got := resp.Properties.Get("classification").AsString(); got != "ANALYTICS" {
			t.Fatalf("expected applied classification ANALYTICS, got %q", got)
		}
	})

	t.Run("read lists only the rule's store type", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodGet, "/v1/cookie-consent/configs/config-abc/rules")
			if got := r.URL.Query().Get("type"); got != "cookie" {
				t.Errorf("expected the refresh to filter on type=cookie, got %q", got)
			}
			writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{cmpRuleResponseFixture()}})
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-abc/42",
			Urn:        cmpURN("CookieConsentRule", "filtered"),
			Properties: ruleStateProperties(),
			Inputs:     ruleInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "config-abc/42" {
			t.Fatalf("expected the canonical rule ID, got %q", resp.ID)
		}
	})

	t.Run("read rejects an ID without a config ID", func(t *testing.T) {
		server := newCMPProviderServer(t, "http://127.0.0.1:1")
		_, err := server.Read(p.ReadRequest{
			ID:         "42",
			Urn:        cmpURN("CookieConsentRule", "bare"),
			Properties: ruleStateProperties(),
			Inputs:     ruleInputProperties(),
		})
		if err == nil || !strings.Contains(err.Error(), "expected <configId>/<ruleId>") {
			t.Fatalf("expected a composite ID error, got %v", err)
		}
	})

	// After a dashboard deletion the list endpoint answers 200 without the rule, not 404.
	t.Run("read returns empty ID when the rule is missing from the list", func(t *testing.T) {
		requestCount := 0
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			assertCMPRequest(t, r, http.MethodGet, "/v1/cookie-consent/configs/config-abc/rules")
			other := cmpRuleResponseFixture()
			other.RuleID = 7
			if requestCount == 1 {
				writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{other}, Next: "page-2"})
				return
			}
			writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{other}})
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-abc/42",
			Urn:        cmpURN("CookieConsentRule", "deleted-upstream"),
			Properties: ruleStateProperties(),
			Inputs:     ruleInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "" {
			t.Fatalf("expected the rule to be reported as deleted, got ID %q", resp.ID)
		}
		if requestCount != 2 {
			t.Fatalf("expected both pages to be searched, got %d requests", requestCount)
		}
	})

	t.Run("read returns empty ID when the rules endpoint returns 404", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodGet, "/v1/cookie-consent/configs/config-abc/rules")
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-abc/42",
			Urn:        cmpURN("CookieConsentRule", "missing"),
			Properties: ruleStateProperties(),
			Inputs:     ruleInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "" {
			t.Fatalf("expected missing resource ID, got %q", resp.ID)
		}
	})

	t.Run("delete treats 404 as success", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodDelete, "/v1/cookie-consent/rules/42")
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		if err := server.Delete(p.DeleteRequest{
			ID:         "config-abc/42",
			Urn:        cmpURN("CookieConsentRule", "missing"),
			Properties: ruleStateProperties(),
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("delete propagates non-404 errors", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		err := server.Delete(p.DeleteRequest{
			ID:         "config-abc/42",
			Urn:        cmpURN("CookieConsentRule", "bad-delete"),
			Properties: ruleStateProperties(),
		})
		if err == nil {
			t.Fatal("expected delete error")
		}
	})
}

func TestParseRuleResourceID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		id           string
		wantConfigID string
		wantRuleID   int
		wantError    bool
	}{
		{name: "composite", id: "config-abc/42", wantConfigID: "config-abc", wantRuleID: 42},
		{name: "opaque config ID", id: "customer/config/abc/42", wantConfigID: "customer/config/abc", wantRuleID: 42},
		// No released provider ever wrote a bare rule ID; the composite form is the only one accepted.
		{name: "bare rule ID", id: "42", wantError: true},
		{name: "missing rule ID", id: "config-abc/", wantError: true},
		{name: "non-numeric rule ID", id: "config-abc/not-a-number", wantError: true},
		{name: "missing config ID", id: "/42", wantError: true},
		{name: "empty", id: "", wantError: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configID, ruleID, err := parseRuleResourceID(tc.id)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if configID != tc.wantConfigID || ruleID != tc.wantRuleID {
				t.Fatalf("got (%q, %d), want (%q, %d)", configID, ruleID, tc.wantConfigID, tc.wantRuleID)
			}
		})
	}
}

func TestFindCookieConsentRule(t *testing.T) {
	t.Run("follows cursor pagination", func(t *testing.T) {
		requestCount := 0
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			assertCMPRequest(t, r, http.MethodGet, "/v1/cookie-consent/configs/config-abc/rules")
			if got := r.URL.Query().Get("limit"); got != "500" {
				t.Fatalf("expected limit=500, got %q", got)
			}
			switch requestCount {
			case 1:
				if got := r.URL.Query().Get("next"); got != "" {
					t.Fatalf("expected no initial cursor, got %q", got)
				}
				writeCMPRulesListResponse(t, w, cmpRulesListResponse{Next: "page-2"})
			case 2:
				if got := r.URL.Query().Get("next"); got != "page-2" {
					t.Fatalf("expected next=page-2, got %q", got)
				}
				writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{cmpRuleResponseFixture()}})
			default:
				t.Fatalf("unexpected request %d", requestCount)
			}
		}))
		defer api.Close()

		found, ok, err := findCookieConsentRule(t.Context(), newCMPJSONClient(t, api.URL), "config-abc", 42, "")
		if err != nil {
			t.Fatal(err)
		}
		if !ok || found.RuleID != 42 {
			t.Fatalf("expected rule 42, got %#v, found=%v", found, ok)
		}
		if requestCount != 2 {
			t.Fatalf("expected two requests, got %d", requestCount)
		}
	})

	t.Run("filters on the store type when it is known", func(t *testing.T) {
		var gotType atomic.Value
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotType.Store(r.URL.Query().Get("type"))
			writeCMPRulesListResponse(t, w, cmpRulesListResponse{Items: []cmpRuleResponse{cmpRuleResponseFixture()}})
		}))
		defer api.Close()

		client := newCMPJSONClient(t, api.URL)
		if _, _, err := findCookieConsentRule(t.Context(), client, "config-abc", 42, "scripts"); err != nil {
			t.Fatal(err)
		}
		if got, _ := gotType.Load().(string); got != "script" {
			t.Fatalf("expected type=script, got %q", got)
		}
		if _, _, err := findCookieConsentRule(t.Context(), client, "config-abc", 42, "pixels"); err == nil {
			t.Fatal("expected an unknown store type to be rejected before any request")
		}
	})

	t.Run("rejects a repeated cursor", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeCMPRulesListResponse(t, w, cmpRulesListResponse{Next: "repeat"})
		}))
		defer api.Close()

		_, _, err := findCookieConsentRule(t.Context(), newCMPJSONClient(t, api.URL), "config-abc", 42, "")
		if err == nil || !strings.Contains(err.Error(), "repeated cursor") {
			t.Fatalf("expected repeated cursor error, got %v", err)
		}
	})

	t.Run("stops after the page cap", func(t *testing.T) {
		var pages atomic.Int32
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeCMPRulesListResponse(t, w, cmpRulesListResponse{Next: fmt.Sprintf("page-%d", pages.Add(1))})
		}))
		defer api.Close()

		_, _, err := findCookieConsentRule(t.Context(), newCMPJSONClient(t, api.URL), "config-abc", 42, "")
		if err == nil || !strings.Contains(err.Error(), "exceeded 1000 pages") {
			t.Fatalf("expected a page cap error, got %v", err)
		}
		if got := pages.Load(); got != maxListPages {
			t.Fatalf("expected exactly %d pages to be fetched, got %d", maxListPages, got)
		}
	})
}

func TestPtrStringEqual(t *testing.T) {
	t.Parallel()

	a := "hello"
	b := "hello"
	c := "world"

	cases := []struct {
		name string
		a    *string
		b    *string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"a nil", nil, &a, false},
		{"b nil", &a, nil, false},
		{"equal", &a, &b, true},
		{"not equal", &a, &c, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ptrStringEqual(tc.a, tc.b); got != tc.want {
				t.Fatalf("ptrStringEqual(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// Osano reports description: "" on some script rules. Adopting it would import a value Check
// rejects, so the preview after `pulumi import` failed; it must stay unmanaged instead.
func TestCookieConsentRuleArgsFromResponseLeavesCookieOnlyFieldsUnmanaged(t *testing.T) {
	t.Parallel()

	empty := ""
	scriptRule := cmpRuleResponseFixture()
	scriptRule.Type = "script"
	scriptRule.Description = &empty
	scriptRule.Expiry = &empty

	t.Run("import of a script rule", func(t *testing.T) {
		args, err := cookieConsentRuleArgsFromResponse(scriptRule, "config-abc", CookieConsentRuleArgs{})
		if err != nil {
			t.Fatal(err)
		}
		if args.StoreType != "scripts" {
			t.Fatalf("expected storeType scripts, got %q", args.StoreType)
		}
		if args.Description != nil || args.Expiry != nil {
			t.Fatalf("expected description and expiry unset, got %v and %v", args.Description, args.Expiry)
		}
		if args.Title == nil || *args.Title != *scriptRule.Title {
			t.Fatalf("expected the other optional fields to be adopted, got title %v", args.Title)
		}
	})

	// State written by an import from an earlier provider version still holds the "".
	t.Run("refresh of a script rule imported with an empty description", func(t *testing.T) {
		declared := baseRuleArgs()
		declared.StoreType = "scripts"
		declared.Description = &empty
		declared.Expiry = &empty
		args, err := cookieConsentRuleArgsFromResponse(scriptRule, "config-abc", declared)
		if err != nil {
			t.Fatal(err)
		}
		if args.Description != nil || args.Expiry != nil {
			t.Fatalf("expected description and expiry unset, got %v and %v", args.Description, args.Expiry)
		}
	})

	t.Run("import of a cookie rule keeps an empty description", func(t *testing.T) {
		cookieRule := cmpRuleResponseFixture()
		cookieRule.Description = &empty
		args, err := cookieConsentRuleArgsFromResponse(cookieRule, "config-abc", CookieConsentRuleArgs{})
		if err != nil {
			t.Fatal(err)
		}
		if args.Description == nil || *args.Description != "" {
			t.Fatalf("expected the cookie rule's empty description to be adopted, got %v", args.Description)
		}
	})
}

func validRuleCheckInputValues() map[string]property.Value {
	return map[string]property.Value{
		"configId":       property.New("config-abc"),
		"storeType":      property.New("cookies"),
		"classification": property.New("ANALYTICS"),
		"rule":           property.New("_ga"),
		"disclosure":     property.New(true),
		"title":          property.New("Google Analytics"),
		"vendorName":     property.New("Google"),
		"ruleType":       property.New("EXACT_MATCH"),
		"description":    property.New("Measures site usage"),
		"expiry":         property.New("2 years"),
	}
}

func baseRuleArgs() CookieConsentRuleArgs {
	title := "Google Analytics"
	vendorName := "Google"
	ruleType := "EXACT_MATCH"
	description := "Measures site usage"
	expiry := "2 years"
	return CookieConsentRuleArgs{
		ConfigID:       "config-abc",
		StoreType:      "cookies",
		Classification: "ANALYTICS",
		Rule:           "_ga",
		Disclosure:     true,
		Title:          &title,
		VendorName:     &vendorName,
		RuleType:       &ruleType,
		Description:    &description,
		Expiry:         &expiry,
	}
}

func baseRuleState() CookieConsentRuleState {
	return CookieConsentRuleState{
		CookieConsentRuleArgs: baseRuleArgs(),
		RuleID:                42,
		Created:               "2026-08-13T12:00:00Z",
		Updated:               "2026-08-13T13:00:00Z",
	}
}

func assertCMPRuleCreateBody(t *testing.T, r *http.Request) {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"configIds": []any{"config-abc"},
		"cookies": []any{map[string]any{
			"classification": "ANALYTICS",
			"rule":           "_ga",
			"disclosure":     true,
			"title":          "Google Analytics",
			"vendorName":     "Google",
			"ruleType":       "EXACT_MATCH",
			"description":    "Measures site usage",
			"expiry":         "2 years",
		}},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("unexpected create body: %#v", body)
	}
}

func assertCMPRuleClearBody(t *testing.T, r *http.Request) {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"classification": "ANALYTICS",
		"rule":           "_ga",
		"disclosure":     true,
		"title":          nil,
		"vendorName":     nil,
		"ruleType":       nil,
		"description":    nil,
		"expiry":         nil,
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("unexpected update body: %#v", body)
	}
}

func assertCMPRuleProperties(t *testing.T, properties property.Map) {
	t.Helper()
	assertCMPRuleInputs(t, properties)
	if got := int(properties.Get("ruleId").AsNumber()); got != 42 {
		t.Fatalf("expected rule ID 42, got %d", got)
	}
	if got := properties.Get("created").AsString(); got != "2026-08-13T12:00:00Z" {
		t.Fatalf("unexpected created timestamp %q", got)
	}
	if got := properties.Get("updated").AsString(); got != "2026-08-13T13:00:00Z" {
		t.Fatalf("unexpected updated timestamp %q", got)
	}
}

func assertCMPRuleInputs(t *testing.T, inputs property.Map) {
	t.Helper()
	wantStrings := map[string]string{
		"configId":       "config-abc",
		"storeType":      "cookies",
		"classification": "ANALYTICS",
		"rule":           "_ga",
		"title":          "Google Analytics",
		"vendorName":     "Google",
		"ruleType":       "EXACT_MATCH",
		"description":    "Measures site usage",
		"expiry":         "2 years",
	}
	for key, want := range wantStrings {
		if got := inputs.Get(key).AsString(); got != want {
			t.Fatalf("expected %s %q, got %q", key, want, got)
		}
	}
	if got := inputs.Get("disclosure").AsBool(); !got {
		t.Fatal("expected disclosure=true")
	}
}

func ruleInputProperties() property.Map {
	return property.NewMap(validRuleCheckInputValues())
}

func clearedRuleInputProperties() property.Map {
	values := validRuleCheckInputValues()
	for _, key := range []string{"title", "vendorName", "ruleType", "description", "expiry"} {
		delete(values, key)
	}
	return property.NewMap(values)
}

func ruleStateProperties() property.Map {
	return ruleInputProperties().
		Set("ruleId", property.New(42.0)).
		Set("created", property.New("2026-08-13T12:00:00Z")).
		Set("updated", property.New("2026-08-13T13:00:00Z"))
}

func emptyRuleInputProperties() property.Map {
	return property.NewMap(map[string]property.Value{
		"configId": {}, "storeType": {}, "classification": {}, "rule": {}, "disclosure": {},
		"title": {}, "vendorName": {}, "ruleType": {}, "description": {}, "expiry": {},
	})
}

func emptyRuleStateProperties() property.Map {
	return emptyRuleInputProperties().
		Set("ruleId", property.Value{}).
		Set("created", property.Value{}).
		Set("updated", property.Value{})
}

func cmpRuleResponseFixture() cmpRuleResponse {
	args := baseRuleArgs()
	return cmpRuleResponse{
		Classification: args.Classification,
		Rule:           args.Rule,
		Disclosure:     args.Disclosure,
		Title:          args.Title,
		VendorName:     args.VendorName,
		RuleType:       args.RuleType,
		Description:    args.Description,
		Expiry:         args.Expiry,
		Type:           "cookie",
		RuleID:         42,
		ConfigID:       "config-abc",
		VendorID:       "vendor-123",
		Created:        "2026-08-13T12:00:00Z",
		Updated:        "2026-08-13T13:00:00Z",
	}
}

func writeCMPRulesListResponse(t *testing.T, w http.ResponseWriter, response cmpRulesListResponse) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Fatal(err)
	}
}

func writeCMPRuleResponse(t *testing.T, w http.ResponseWriter, response cmpRuleResponse) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Fatal(err)
	}
}
