package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

func TestConsentSubjectReference(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		subject ConsentSubject
		wantRef string
		wantErr string
	}{
		{name: "verified ID", subject: ConsentSubject{VerifiedID: "verified-123"}, wantRef: "verified-123"},
		{name: "anonymous ID", subject: ConsentSubject{AnonymousID: "anon-123"}, wantRef: "anon-123"},
		{
			name:    "verified ID wins over anonymous ID",
			subject: ConsentSubject{VerifiedID: "verified-123", AnonymousID: "anon-123"},
			wantRef: "verified-123",
		},
		{
			name:    "missing subject",
			subject: ConsentSubject{},
			wantErr: "either subject.verifiedId or subject.anonymousId must be set",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotRef, err := tc.subject.reference()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				if gotRef != tc.wantRef {
					t.Fatalf("expected %q, got %q", tc.wantRef, gotRef)
				}
				return
			}

			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("expected error %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestNormalizeReferenceType(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ input, want string }{
		{"", "subject"}, {"subject", "subject"}, {" session ", "session"}, {"anonymous", "subject"},
	} {
		got, err := normalizeReferenceType(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("normalizeReferenceType(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	if _, err := normalizeReferenceType("verified"); err == nil {
		t.Fatal("expected an unknown reference type to fail")
	}
}

func TestConsentStructContractUsesReplacementAndOmitsRawResponse(t *testing.T) {
	t.Parallel()

	argsType := reflect.TypeOf(ConsentArgs{})
	for _, fieldName := range []string{
		"Subject",
		"Actions",
		"Attributes",
		"Compliance",
		"Jurisdiction",
		"Origin",
		"Tags",
	} {
		field, ok := argsType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("missing ConsentArgs field %q", fieldName)
		}
		if tag := field.Tag.Get("provider"); !strings.Contains(tag, "replaceOnChanges") {
			t.Fatalf("expected provider tag replaceOnChanges for %s, got %q", fieldName, tag)
		}
	}
	// Consents are immutable, so every input, including the newer optional ones, must replace.
	for idx := range argsType.NumField() {
		field := argsType.Field(idx)
		if !strings.Contains(field.Tag.Get("provider"), "replaceOnChanges") {
			t.Fatalf("expected ConsentArgs.%s to be replaceOnChanges", field.Name)
		}
	}
	if tag, _ := argsType.FieldByName("SessionToken"); !strings.Contains(tag.Tag.Get("provider"), "secret") {
		t.Fatal("expected sessionToken to be secret")
	}

	stateType := reflect.TypeOf(ConsentState{})
	if _, ok := stateType.FieldByName("Response"); ok {
		t.Fatal("consent state should not persist raw response output")
	}
}

// TestConsentDiff covers the value-based diff: every changed input replaces the resource, and an
// absent list or map equals an empty one.
func TestConsentDiff(t *testing.T) {
	t.Parallel()

	base := func() ConsentArgs {
		return ConsentArgs{
			Subject:    ConsentSubject{VerifiedID: "user-1"},
			Actions:    []ConsentAction{{Target: "protocol-1", Vendor: "config-1", Action: "ACCEPT"}},
			Attributes: map[string]string{"count": "1"},
			Origin:     "api",
		}
	}
	diffOf := func(t *testing.T, previous, next ConsentArgs) infer.DiffResponse {
		t.Helper()
		resp, err := (&Consent{}).Diff(context.Background(), infer.DiffRequest[ConsentArgs, ConsentState]{
			State: ConsentState{ConsentArgs: previous, ConsentID: "consent-1"}, Inputs: next,
		})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	t.Run("identical inputs are a no-op", func(t *testing.T) {
		if resp := diffOf(t, base(), base()); resp.HasChanges {
			t.Fatalf("expected no changes, got %#v", resp.DetailedDiff)
		}
	})

	t.Run("absent and empty collections are equal", func(t *testing.T) {
		previous, next := base(), base()
		previous.Attributes, previous.Tags = nil, nil
		next.Attributes, next.Tags = map[string]string{}, []string{}
		if resp := diffOf(t, previous, next); resp.HasChanges {
			t.Fatalf("expected no changes, got %#v", resp.DetailedDiff)
		}
	})

	for name, mutate := range map[string]func(*ConsentArgs){
		"subject":             func(args *ConsentArgs) { args.Subject = ConsentSubject{AnonymousID: "anon-1"} },
		"actions":             func(args *ConsentArgs) { args.Actions[0].Action = "REJECT" },
		"attributes":          func(args *ConsentArgs) { args.Attributes["count"] = "2" },
		"origin":              func(args *ConsentArgs) { args.Origin = "gpc" },
		"jurisdiction":        func(args *ConsentArgs) { args.Jurisdiction = "us-ca" },
		"tags":                func(args *ConsentArgs) { args.Tags = []string{"demo"} },
		"sessionToken":        func(args *ConsentArgs) { token := "token"; args.SessionToken = &token },
		"countryCodeOverride": func(args *ConsentArgs) { code := "US"; args.CountryCodeOverride = &code },
		"regionCodeOverride":  func(args *ConsentArgs) { code := "US-CA"; args.RegionCodeOverride = &code },
		"compliance":          func(args *ConsentArgs) { gpc := 1; args.Compliance = &ConsentCompliance{GPC: &gpc} },
	} {
		t.Run(name+" replaces", func(t *testing.T) {
			next := base()
			mutate(&next)
			resp := diffOf(t, base(), next)
			if !resp.HasChanges || len(resp.DetailedDiff) != 1 || resp.DetailedDiff[name].Kind != p.UpdateReplace {
				t.Fatalf("expected only %s to replace, got %#v", name, resp.DetailedDiff)
			}
		})
	}
}
