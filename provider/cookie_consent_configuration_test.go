package provider

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

func TestValidateCookieConsentConfiguration(t *testing.T) {
	t.Parallel()

	base := func() map[string]any {
		return map[string]any{"storagePolicyHref": "https://example.com/privacy"}
	}
	with := func(key string, value any) map[string]any {
		configuration := base()
		configuration[key] = value
		return configuration
	}

	valid := map[string]map[string]any{
		"minimal": base(),
		"documented values": {
			"storagePolicyHref":    "https://example.com/privacy",
			"googleConsent":        false,
			"tattleSampling":       0.5,
			"timeoutSeconds":       10.0,
			"iframeBlocking":       "",
			"localStorageBlocking": "permissive",
			"enableDoNotSell":      true,
			"doNotSellCategories":  []any{"MARKETING", "ANALYTICS"},
			"policyLinkText":       "cookiePolicy",
			"additionalLinks":      []any{[]any{"subjectRightsRequest", "https://example.com/dsar"}},
			"palette":              map[string]any{"dialogType": "box", "displayPosition": "bottom-left", "theme": "modern"},
			"translations":         map[string]any{"buttons": map[string]any{"accept": map[string]any{"en": "OK"}}},
			"variantMapping": map[string]any{
				"byJurisdiction": map[string]any{"us-ca": "three", "us": "one"},
				"behavior":       "fallbackToOsano",
			},
		},
		"cleared variant mapping":  with("variantMapping", map[string]any{}),
		"cleared additional links": with("additionalLinks", nil),
	}
	for name, configuration := range valid {
		t.Run("accepts "+name, func(t *testing.T) {
			t.Parallel()
			failures, _ := validateCookieConsentConfiguration(configuration, "production", true)
			if len(failures) != 0 {
				t.Fatalf("unexpected failures: %#v", failures)
			}
		})
	}

	invalid := []struct {
		name          string
		configuration map[string]any
		property      string
	}{
		{"missing storagePolicyHref", map[string]any{"showWidget": true}, "configuration.storagePolicyHref"},
		{"empty storagePolicyHref", map[string]any{"storagePolicyHref": " "}, "configuration.storagePolicyHref"},
		{"non-boolean flag", with("showWidget", "yes"), "configuration.showWidget"},
		{"tattleSampling above 1", with("tattleSampling", 1.5), "configuration.tattleSampling"},
		{"fractional timeout", with("timeoutSeconds", 2.5), "configuration.timeoutSeconds"},
		{"unknown blocking mode", with("iframeBlocking", "strict"), "configuration.iframeBlocking"},
		{
			"unknown do-not-sell category",
			with("doNotSellCategories", []any{"ESSENTIAL"}),
			"configuration.doNotSellCategories",
		},
		{"do-not-sell without categories", with("enableDoNotSell", true), "configuration.doNotSellCategories"},
		// Osano's spec requires at least one link on write; import leaves out the [] it reports.
		{"empty additional links", with("additionalLinks", []any{}), "configuration.additionalLinks"},
		{"too many additional links", with("additionalLinks", []any{
			[]any{"imprint", "/a"}, []any{"termsOfUse", "/b"}, []any{"securityPolicy", "/c"},
		}), "configuration.additionalLinks"},
		{"unknown additional link text", with("additionalLinks", []any{[]any{"home", "/"}}), "configuration.additionalLinks"},
		{"non-list additional links", with("additionalLinks", "/imprint"), "configuration.additionalLinks"},
		{"additional link repeating policyLinkText", map[string]any{
			"storagePolicyHref": "/privacy", "policyLinkText": "privacyPolicy",
			"additionalLinks": []any{[]any{"privacyPolicy", "/privacy"}},
		}, "configuration.additionalLinks"},
		{"variant mapping without behavior", with("variantMapping", map[string]any{
			"byJurisdiction": map[string]any{"us-ca": "three"},
		}), "configuration.variantMapping"},
		{
			"retired flat variant mapping",
			with("variantMapping", map[string]any{"us-ca": "three"}),
			"configuration.variantMapping",
		},
		{"unknown banner format", with("variantMapping", map[string]any{
			"byJurisdiction": map[string]any{"us-ca": "two"}, "behavior": "fallbackToOsano",
		}), "configuration.variantMapping"},
		{"upper-case jurisdiction", with("variantMapping", map[string]any{
			"byJurisdiction": map[string]any{"US-CA": "three"}, "behavior": "fallbackToOsano",
		}), "configuration.variantMapping"},
		{"unknown dialog type", with("palette", map[string]any{"dialogType": "modal"}), "configuration.palette"},
		{"bar display position on a box", with("palette", map[string]any{
			"dialogType": "box", "displayPosition": "top",
		}), "configuration.palette"},
		{"non-object translations", with("translations", "fr"), "configuration.translations"},
	}
	for _, tc := range invalid {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			t.Parallel()
			failures, _ := validateCookieConsentConfiguration(tc.configuration, "production", true)
			assertFailureProperty(t, failures, tc.property)
		})
	}

	warnings := []struct {
		name          string
		configuration map[string]any
		mode          string
		want          string
	}{
		{"unknown key", with("showWidgets", true), "production", "configuration.showWidgets is not in Osano's"},
		{
			name:          "deprecated palette key",
			configuration: with("palette", map[string]any{"toggleButtonOnColor": "#fff"}),
			mode:          "production",
			want:          "use palette.toggleOnThumbColor",
		},
		{
			name:          "unknown palette key",
			configuration: with("palette", map[string]any{"buttonColour": "#fff"}),
			mode:          "production",
			want:          "configuration.palette.buttonColour",
		},
		{"undocumented policy link text", with("policyLinkText", "legal"), "production", "policyLinkText \"legal\""},
		{
			name: "ccpaRelaxed beside a variant mapping",
			configuration: map[string]any{
				"storagePolicyHref": "/privacy", "ccpaRelaxed": true,
				"variantMapping": map[string]any{
					"byJurisdiction": map[string]any{"us": "one"}, "behavior": "fallbackToOsano",
				},
			},
			mode: "production",
			want: "ccpaRelaxed is ignored",
		},
		{"Google Consent Mode in debug mode", base(), "debug", "googleConsent is enabled"},
	}
	for _, tc := range warnings {
		t.Run("warns about "+tc.name, func(t *testing.T) {
			t.Parallel()
			failures, got := validateCookieConsentConfiguration(tc.configuration, tc.mode, true)
			if len(failures) != 0 {
				t.Fatalf("warnings must not fail the check: %#v", failures)
			}
			for _, warning := range got {
				if strings.Contains(warning, tc.want) {
					return
				}
			}
			t.Fatalf("expected a warning containing %q, got %#v", tc.want, got)
		})
	}

	t.Run("debug mode with Google Consent Mode off does not warn", func(t *testing.T) {
		t.Parallel()
		_, got := validateCookieConsentConfiguration(with("googleConsent", false), "debug", true)
		if len(got) != 0 {
			t.Fatalf("unexpected warnings: %#v", got)
		}
	})
}

func TestProjectConfigurationRecursesIntoNestedObjects(t *testing.T) {
	t.Parallel()

	server := map[string]any{
		"storagePolicyHref": "https://example.com/changed",
		"showWidget":        true,
		"palette": map[string]any{
			"buttonBackgroundColor": "#000000",
			"linkColor":             "#37CD8F",
			"dialogType":            "bar",
		},
		"translations": map[string]any{
			"buttons": map[string]any{"accept": map[string]any{"en": "Accept", "fr": "Accepter"}},
		},
		"variantMapping": map[string]any{
			"byJurisdiction": map[string]any{"us-ca": "three", "us-tx": "three"},
			"behavior":       "fallbackToOsano",
		},
	}
	declared := map[string]any{
		"storagePolicyHref": "https://example.com/privacy",
		"palette":           map[string]any{"buttonBackgroundColor": "#111111", "theme": "modern"},
		"translations":      map[string]any{"buttons": map[string]any{"accept": map[string]any{"en": "OK"}}},
		"variantMapping": map[string]any{
			"byJurisdiction": map[string]any{"us-ca": "three"},
			"behavior":       "fallbackToOsano",
		},
	}
	want := map[string]any{
		// Declared scalars adopt the server value so drift is visible.
		"storagePolicyHref": "https://example.com/changed",
		// Nested objects keep only declared keys; a declared key Osano omits keeps its declared value.
		"palette":      map[string]any{"buttonBackgroundColor": "#000000", "theme": "modern"},
		"translations": map[string]any{"buttons": map[string]any{"accept": map[string]any{"en": "Accept"}}},
		// The variant mapping is one setting, so jurisdictions added in Osano show as drift.
		"variantMapping": server["variantMapping"],
	}
	if got := projectConfiguration(server, declared); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected projection:\n got: %#v\nwant: %#v", got, want)
	}
}

// A program that declares part of the palette must converge on `pulumi up --refresh`: Osano returns
// the whole palette with its defaults, and adopting it would diff and PATCH on every run.
func TestCookieConsentConfigRefreshConvergesWithPartialPalette(t *testing.T) {
	inputs := configInputProperties().Set("configuration", property.New(map[string]property.Value{
		"storagePolicyHref": property.New("https://example.com/storage-policy"),
		"palette": property.New(map[string]property.Value{
			"buttonBackgroundColor": property.New("#111111"),
		}),
	}))
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fixture := cmpConfigResponseFixture()
		fixture.Configuration = map[string]any{
			"storagePolicyHref": "https://example.com/storage-policy",
			"showWidget":        true,
			"palette": map[string]any{
				"buttonBackgroundColor": "#111111",
				"buttonForegroundColor": "#FFFFFF",
				"dialogType":            "bar",
			},
		}
		writeJSON(t, w, fixture)
	}))
	defer api.Close()

	server := newCMPProviderServer(t, api.URL)
	state := inputs.Set("configId", property.New("config-123"))
	read, err := server.Read(p.ReadRequest{
		ID: "config-123", Urn: cmpURN("CookieConsentConfig", "palette"), Properties: state, Inputs: inputs,
	})
	if err != nil {
		t.Fatal(err)
	}
	diff, err := server.Diff(p.DiffRequest{
		ID: "config-123", Urn: cmpURN("CookieConsentConfig", "palette"), State: read.Properties, Inputs: inputs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if diff.HasChanges {
		t.Fatalf("expected refresh to converge, got diff %#v\nrefreshed configuration: %#v",
			diff.DetailedDiff, read.Properties.Get("configuration"))
	}
}
