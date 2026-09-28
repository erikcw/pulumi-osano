package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	osanoclient "github.com/jflavan/pulumi-osano/provider/internal/osano"
)

func TestCookieConsentPublicationCheck(t *testing.T) {
	t.Parallel()

	resource := &CookieConsentPublication{}
	ctx := context.Background()

	t.Run("defaults discovery preservation", func(t *testing.T) {
		resp, err := resource.Check(ctx, infer.CheckRequest{NewInputs: property.NewMap(map[string]property.Value{
			"configId":    property.New("config-id"),
			"changeToken": property.New("desired-state-v1"),
		})})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Failures) != 0 {
			t.Fatalf("expected no failures, got %#v", resp.Failures)
		}
		if resp.Inputs.KeepUnclassifiedTattles == nil || !*resp.Inputs.KeepUnclassifiedTattles {
			t.Fatalf("expected keepUnclassifiedTattles=true, got %#v", resp.Inputs.KeepUnclassifiedTattles)
		}
	})

	for _, tc := range []struct {
		name        string
		computedKey string
		values      map[string]property.Value
	}{
		{
			name:        "computed configId is valid during preview",
			computedKey: "configId",
			values: map[string]property.Value{
				"configId":    property.New(property.Computed),
				"changeToken": property.New("desired-state-v1"),
			},
		},
		{
			name:        "computed changeToken is valid during preview",
			computedKey: "changeToken",
			values: map[string]property.Value{
				"configId":    property.New("config-id"),
				"changeToken": property.New(property.Computed),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := resource.Check(ctx, infer.CheckRequest{NewInputs: property.NewMap(tc.values)})
			if err != nil {
				t.Fatal(err)
			}
			for _, failure := range resp.Failures {
				if failure.Property == tc.computedKey {
					t.Fatalf("computed %s must not fail validation: %#v", tc.computedKey, resp.Failures)
				}
			}
		})
	}

	for _, tc := range []struct {
		name       string
		values     map[string]property.Value
		failureKey string
	}{
		{
			name: "missing configId",
			values: map[string]property.Value{
				"changeToken": property.New("desired-state-v1"),
			},
			failureKey: "configId",
		},
		{
			name: "missing changeToken",
			values: map[string]property.Value{
				"configId": property.New("config-id"),
			},
			failureKey: "changeToken",
		},
		{
			name: "relative webhook URL",
			values: map[string]property.Value{
				"configId":    property.New("config-id"),
				"changeToken": property.New("desired-state-v1"),
				"webhookUrl":  property.New("/publish-complete"),
			},
			failureKey: "webhookUrl",
		},
		{
			name: "non HTTP webhook URL",
			values: map[string]property.Value{
				"configId":    property.New("config-id"),
				"changeToken": property.New("desired-state-v1"),
				"webhookUrl":  property.New("ftp://example.com/publish-complete"),
			},
			failureKey: "webhookUrl",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := resource.Check(ctx, infer.CheckRequest{NewInputs: property.NewMap(tc.values)})
			if err != nil {
				t.Fatal(err)
			}
			assertFailureProperty(t, resp.Failures, tc.failureKey)
		})
	}
}

func TestCookieConsentPublicationDiff(t *testing.T) {
	t.Parallel()

	resource := &CookieConsentPublication{}
	ctx := context.Background()

	t.Run("identical args are a no-op", func(t *testing.T) {
		args := publicationArgsFixture()
		resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentPublicationArgs, CookieConsentPublicationState]{
			Inputs: args,
			State:  CookieConsentPublicationState{CookieConsentPublicationArgs: args},
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.HasChanges {
			t.Fatalf("expected no changes, got %#v", resp.DetailedDiff)
		}
	})

	for _, tc := range []struct {
		name   string
		key    string
		kind   p.DiffKind
		mutate func(*CookieConsentPublicationArgs)
	}{
		{
			name: "configId requires replacement", key: "configId", kind: p.UpdateReplace,
			mutate: func(args *CookieConsentPublicationArgs) { args.ConfigID = "other-config" },
		},
		{
			name: "changeToken queues an update", key: "changeToken", kind: p.Update,
			mutate: func(args *CookieConsentPublicationArgs) { args.ChangeToken = "desired-state-v2" },
		},
		{
			name: "keepUnclassifiedTattles queues an update", key: "keepUnclassifiedTattles", kind: p.Update,
			mutate: func(args *CookieConsentPublicationArgs) {
				keep := false
				args.KeepUnclassifiedTattles = &keep
			},
		},
		{
			name: "description queues an update", key: "description", kind: p.Update,
			mutate: func(args *CookieConsentPublicationArgs) {
				description := "updated publication"
				args.Description = &description
			},
		},
		{
			name: "webhookUrl queues an update", key: "webhookUrl", kind: p.Update,
			mutate: func(args *CookieConsentPublicationArgs) {
				webhookURL := "https://example.com/other-hook"
				args.WebhookURL = &webhookURL
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateArgs := publicationArgsFixture()
			inputs := publicationArgsFixture()
			tc.mutate(&inputs)
			resp, err := resource.Diff(ctx, infer.DiffRequest[CookieConsentPublicationArgs, CookieConsentPublicationState]{
				Inputs: inputs,
				State:  CookieConsentPublicationState{CookieConsentPublicationArgs: stateArgs},
			})
			if err != nil {
				t.Fatal(err)
			}
			assertDiffKind(t, resp, tc.key, tc.kind)
		})
	}
}

func TestCookieConsentScript(t *testing.T) {
	t.Parallel()

	src, tag, err := cookieConsentScript("xpYRGjjVrA", "4c5ae68c-68d7-41ac-8ab3-08ff308bf254")
	if err != nil {
		t.Fatal(err)
	}
	if src != "https://cmp.osano.com/xpYRGjjVrA/4c5ae68c-68d7-41ac-8ab3-08ff308bf254/osano.js" {
		t.Fatalf("unexpected src %q", src)
	}
	if tag != `<script src="https://cmp.osano.com/xpYRGjjVrA/4c5ae68c-68d7-41ac-8ab3-08ff308bf254/osano.js"></script>` {
		t.Fatalf("unexpected tag %q", tag)
	}

	t.Run("escapes path segments", func(t *testing.T) {
		src, _, err := cookieConsentScript("customer/one", "config two")
		if err != nil {
			t.Fatal(err)
		}
		if src != "https://cmp.osano.com/customer%2Fone/config%20two/osano.js" {
			t.Fatalf("unexpected escaped src %q", src)
		}
	})

	for _, tc := range []struct {
		name       string
		customerID string
		configID   string
	}{
		{name: "missing customer ID", configID: "config-id"},
		{name: "missing config ID", customerID: "customer-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := cookieConsentScript(tc.customerID, tc.configID)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestPublishCookieConsentOutdatedBaseline(t *testing.T) {
	requests := make([]string, 0, 4)
	getCount := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		assertCMPRequest(t, r, r.Method, r.URL.Path)
		switch r.Method {
		case http.MethodGet:
			getCount++
			switch getCount {
			case 1:
				writePublicationConfigResponse(t, w, "outdated", 100, 3)
			case 2:
				writePublicationConfigResponse(t, w, "in-progress", 100, 3)
			case 3:
				writePublicationConfigResponse(t, w, "published", 200, 4)
			default:
				t.Fatalf("unexpected GET %d", getCount)
			}
		case http.MethodPost:
			assertPublicationBody(t, r, map[string]any{
				"keepUnclassifiedTattles": false,
				"description":             "release publication",
				"webhookUrl":              "https://example.com/publish-complete",
			})
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer api.Close()

	args := publicationArgsFixture()
	keep := false
	args.KeepUnclassifiedTattles = &keep
	state, err := publishCookieConsent(t.Context(), newCMPJSONClient(t, api.URL), args, zeroPublicationPollOptions())
	if err != nil {
		t.Fatal(err)
	}
	assertPublishedState(t, state, args, 200, 4)
	wantRequests := []string{
		"GET /v1/cookie-consent/configs/config-id",
		"POST /v1/cookie-consent/configs/config-id/publish",
		"GET /v1/cookie-consent/configs/config-id",
		"GET /v1/cookie-consent/configs/config-id",
	}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Fatalf("unexpected request order: %#v", requests)
	}
}

func TestPublishCookieConsentRejectsStalePublishedResponse(t *testing.T) {
	requests := make([]string, 0, 5)
	getCount := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		assertCMPRequest(t, r, r.Method, r.URL.Path)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		getCount++
		switch getCount {
		case 1, 2:
			writePublicationConfigResponse(t, w, "published", 100, 3)
		case 3:
			writePublicationConfigResponse(t, w, "in-progress", 100, 3)
		case 4:
			writePublicationConfigResponse(t, w, "published", 200, 4)
		default:
			t.Fatalf("unexpected GET %d", getCount)
		}
	}))
	defer api.Close()

	args := publicationArgsFixture()
	state, err := publishCookieConsent(t.Context(), newCMPJSONClient(t, api.URL), args, zeroPublicationPollOptions())
	if err != nil {
		t.Fatal(err)
	}
	assertPublishedState(t, state, args, 200, 4)
	wantRequests := []string{
		"GET /v1/cookie-consent/configs/config-id",
		"POST /v1/cookie-consent/configs/config-id/publish",
		"GET /v1/cookie-consent/configs/config-id",
		"GET /v1/cookie-consent/configs/config-id",
		"GET /v1/cookie-consent/configs/config-id",
	}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Fatalf("unexpected request order: %#v", requests)
	}
}

func TestPublishCookieConsentJoinsConflict(t *testing.T) {
	postCount := 0
	getCount := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertCMPRequest(t, r, r.Method, r.URL.Path)
		if r.Method == http.MethodPost {
			postCount++
			assertPublicationBody(t, r, map[string]any{
				"keepUnclassifiedTattles": true,
			})
			w.WriteHeader(http.StatusConflict)
			return
		}
		getCount++
		if getCount == 1 {
			writePublicationConfigResponse(t, w, "outdated", 100, 3)
			return
		}
		if getCount == 2 {
			writePublicationConfigResponse(t, w, "in-progress", 100, 3)
			return
		}
		writePublicationConfigResponse(t, w, "published", 200, 4)
	}))
	defer api.Close()

	args := publicationArgsFixture()
	args.Description = nil
	args.WebhookURL = nil
	_, err := publishCookieConsent(t.Context(), newCMPJSONClient(t, api.URL), args, zeroPublicationPollOptions())
	if err != nil {
		t.Fatal(err)
	}
	if postCount != 1 {
		t.Fatalf("expected one POST, got %d", postCount)
	}
}

func TestPublishCookieConsentUsesClientRetries(t *testing.T) {
	postCount := 0
	getCount := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertCMPRequest(t, r, r.Method, r.URL.Path)
		if r.Method == http.MethodPost {
			postCount++
			switch postCount {
			case 1:
				w.WriteHeader(http.StatusTooManyRequests)
			case 2:
				w.WriteHeader(http.StatusInternalServerError)
			case 3:
				w.WriteHeader(http.StatusNoContent)
			default:
				t.Fatalf("unexpected POST %d", postCount)
			}
			return
		}
		getCount++
		if getCount == 1 {
			writePublicationConfigResponse(t, w, "outdated", 100, 3)
			return
		}
		writePublicationConfigResponse(t, w, "published", 200, 4)
	}))
	defer api.Close()

	client := newCMPJSONClientWithOptions(
		t, api.URL,
		osanoclient.WithInitialBackoff(time.Nanosecond),
		osanoclient.WithMaxRetries(3),
	)
	state, err := publishCookieConsent(t.Context(), client, publicationArgsFixture(), zeroPublicationPollOptions())
	if err != nil {
		t.Fatal(err)
	}
	if postCount != 3 {
		t.Fatalf("expected three POST attempts, got %d", postCount)
	}
	if state.PublishStatus != "published" {
		t.Fatalf("expected published state, got %#v", state)
	}
}

func TestPublishCookieConsentRecoversFromStaleBaselineError(t *testing.T) {
	requests := make([]string, 0, 5)
	getCount := 0
	postCount := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		assertCMPRequest(t, r, r.Method, r.URL.Path)
		if r.Method == http.MethodPost {
			postCount++
			w.WriteHeader(http.StatusNoContent)
			return
		}

		getCount++
		switch getCount {
		case 1, 2:
			writePublicationConfigResponse(t, w, "error", 100, 3)
		case 3:
			writePublicationConfigResponse(t, w, "in-progress", 100, 3)
		case 4:
			writePublicationConfigResponse(t, w, "published", 200, 4)
		default:
			t.Fatalf("unexpected GET %d", getCount)
		}
	}))
	defer api.Close()

	args := publicationArgsFixture()
	state, err := publishCookieConsent(t.Context(), newCMPJSONClient(t, api.URL), args, zeroPublicationPollOptions())
	if err != nil {
		t.Fatal(err)
	}
	assertPublishedState(t, state, args, 200, 4)
	if postCount != 1 {
		t.Fatalf("expected one POST, got %d", postCount)
	}
	wantRequests := []string{
		"GET /v1/cookie-consent/configs/config-id",
		"POST /v1/cookie-consent/configs/config-id/publish",
		"GET /v1/cookie-consent/configs/config-id",
		"GET /v1/cookie-consent/configs/config-id",
		"GET /v1/cookie-consent/configs/config-id",
	}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Fatalf("unexpected request order: got %#v, want %#v", requests, wantRequests)
	}
}

func TestPublishCookieConsentErrors(t *testing.T) {
	t.Run("terminal error status after accepted publish", func(t *testing.T) {
		requests := make([]string, 0, 3)
		getCount := 0
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests = append(requests, r.Method+" "+r.URL.Path)
			assertCMPRequest(t, r, r.Method, r.URL.Path)
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			getCount++
			if getCount == 1 {
				writePublicationConfigResponse(t, w, "outdated", 100, 3)
				return
			}
			writePublicationConfigResponse(t, w, "error", 101, 3)
		}))
		defer api.Close()

		_, err := publishCookieConsent(
			t.Context(), newCMPJSONClient(t, api.URL), publicationArgsFixture(), zeroPublicationPollOptions(),
		)
		if err == nil || !strings.Contains(err.Error(), "status=error") ||
			!strings.Contains(err.Error(), "lastPublished=101") ||
			!strings.Contains(err.Error(), "publishedRevision=3") {
			t.Fatalf("expected polled terminal status diagnostic, got %v", err)
		}
		wantRequests := []string{
			"GET /v1/cookie-consent/configs/config-id",
			"POST /v1/cookie-consent/configs/config-id/publish",
			"GET /v1/cookie-consent/configs/config-id",
		}
		if !reflect.DeepEqual(requests, wantRequests) {
			t.Fatalf("unexpected request order: got %#v, want %#v", requests, wantRequests)
		}
	})

	// Osano can queue a publication for a long time behind its publication batches, so a config that
	// keeps reporting its previous error is waited for until the deadline, with one warning.
	t.Run("repeated stale error keeps polling until the deadline", func(t *testing.T) {
		var getCount, postCount atomic.Int32
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, r.Method, r.URL.Path)
			if r.Method == http.MethodPost {
				postCount.Add(1)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			getCount.Add(1)
			writePublicationConfigResponse(t, w, "error", 100, 3)
		}))
		defer api.Close()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var warnings []string
		polls := 0
		opts := zeroPublicationPollOptions()
		opts.Warn = func(message string) { warnings = append(warnings, message) }
		opts.Sleep = func(ctx context.Context, _ time.Duration) error {
			polls++
			if polls == 2*staleErrorWarningPolls {
				cancel() // the deadline a customTimeout would impose
			}
			return ctx.Err()
		}
		_, err := publishCookieConsent(ctx, newCMPJSONClient(t, api.URL), publicationArgsFixture(), opts)
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "wait for Cookie Consent publication") {
			t.Fatalf("expected the wait to end only with the deadline, got %v", err)
		}
		if postCount.Load() != 1 {
			t.Fatalf("expected one POST, got %d", postCount.Load())
		}
		if want := int32(1 + 2*staleErrorWarningPolls); getCount.Load() != want {
			t.Fatalf("expected the baseline plus every stale poll (%d GETs), got %d", want, getCount.Load())
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "still reports its previous publication error") {
			t.Fatalf("expected exactly one stale-error warning, got %q", warnings)
		}
	})

	t.Run("unknown terminal status", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writePublicationConfigResponse(t, w, "mystery", 100, 3)
		}))
		defer api.Close()

		_, err := publishCookieConsent(
			t.Context(), newCMPJSONClient(t, api.URL), publicationArgsFixture(), zeroPublicationPollOptions(),
		)
		if err == nil || !strings.Contains(err.Error(), "status=mystery") {
			t.Fatalf("expected unknown status diagnostic, got %v", err)
		}
	})

	t.Run("missing config", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		_, err := publishCookieConsent(
			t.Context(), newCMPJSONClient(t, api.URL), publicationArgsFixture(), zeroPublicationPollOptions(),
		)
		if err == nil || !strings.Contains(err.Error(), "config-id") {
			t.Fatalf("expected missing config diagnostic, got %v", err)
		}
	})

	t.Run("repeated stale publication honors cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		getCount := 0
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			getCount++
			writePublicationConfigResponse(t, w, "published", 100, 3)
		}))
		defer api.Close()

		sleepCount := 0
		opts := zeroPublicationPollOptions()
		opts.Sleep = func(ctx context.Context, _ time.Duration) error {
			sleepCount++
			if sleepCount == 2 {
				cancel()
				return ctx.Err()
			}
			return nil
		}
		_, err := publishCookieConsent(ctx, newCMPJSONClient(t, api.URL), publicationArgsFixture(), opts)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
		if getCount != 3 {
			t.Fatalf("expected baseline and two stale reads, got %d GETs", getCount)
		}
	})

	t.Run("missing config ID makes no request", func(t *testing.T) {
		requestCount := 0
		api := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			requestCount++
		}))
		defer api.Close()

		args := publicationArgsFixture()
		args.ConfigID = ""
		_, err := publishCookieConsent(t.Context(), newCMPJSONClient(t, api.URL), args, zeroPublicationPollOptions())
		if err == nil || !strings.Contains(err.Error(), "configId") {
			t.Fatalf("expected config ID diagnostic, got %v", err)
		}
		if requestCount != 0 {
			t.Fatalf("expected no requests, got %d", requestCount)
		}
	})

	t.Run("missing customer ID fails before publish", func(t *testing.T) {
		requestCount := 0
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requestCount++
			response := publicationConfigFixture("outdated", 100, 3)
			response.CustomerID = ""
			writeJSON(t, w, response)
		}))
		defer api.Close()

		_, err := publishCookieConsent(
			t.Context(), newCMPJSONClient(t, api.URL), publicationArgsFixture(), zeroPublicationPollOptions(),
		)
		if err == nil || !strings.Contains(err.Error(), "customerId") {
			t.Fatalf("expected customer ID diagnostic, got %v", err)
		}
		if requestCount != 1 {
			t.Fatalf("expected only the baseline GET, got %d requests", requestCount)
		}
	})
}

func TestCookieConsentPublicationLifecycle(t *testing.T) {
	t.Run("create and update preview make no HTTP calls", func(t *testing.T) {
		requestCount := 0
		api := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			requestCount++
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		createResp, err := server.Create(p.CreateRequest{
			Urn:        cmpURN("CookieConsentPublication", "preview-create"),
			Properties: publicationInputProperties(),
			DryRun:     true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if createResp.ID != "config-id" {
			t.Fatalf("expected the preview to use the config ID, got %q", createResp.ID)
		}
		_, err = server.Update(p.UpdateRequest{
			ID:     "config-id",
			Urn:    cmpURN("CookieConsentPublication", "preview-update"),
			State:  publicationStateProperties(),
			Inputs: publicationInputProperties(),
			DryRun: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if requestCount != 0 {
			t.Fatalf("preview made %d HTTP requests", requestCount)
		}
	})

	t.Run("read preserves prior inputs and only gets config", func(t *testing.T) {
		requestCount := 0
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			assertCMPRequest(t, r, http.MethodGet, "/v1/cookie-consent/configs/config-id")
			writePublicationConfigResponse(t, w, "published", 200, 4)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-id",
			Urn:        cmpURN("CookieConsentPublication", "read"),
			Properties: publicationStateProperties(),
			Inputs:     publicationInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if requestCount != 1 {
			t.Fatalf("expected one GET, got %d requests", requestCount)
		}
		if got := resp.Inputs.Get("changeToken").AsString(); got != "desired-state-v1" {
			t.Fatalf("expected preserved change token, got %q", got)
		}
		assertPublicationProperties(t, resp.Properties, "desired-state-v1", 200, 4)
	})

	t.Run("import adopts current metadata without publishing", func(t *testing.T) {
		requestCount := 0
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			assertCMPRequest(t, r, http.MethodGet, "/v1/cookie-consent/configs/config-id")
			writePublicationConfigResponse(t, w, "published", 200, 4)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-id",
			Urn:        cmpURN("CookieConsentPublication", "imported"),
			Properties: emptyPublicationStateProperties(),
			Inputs:     emptyPublicationInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if requestCount != 1 {
			t.Fatalf("expected one GET, got %d requests", requestCount)
		}
		if got := resp.ID; got != "config-id" {
			t.Fatalf("expected imported ID config-id, got %q", got)
		}
		assertPublicationProperties(t, resp.Properties, "import:200:4", 200, 4)
		if got := resp.Inputs.Get("changeToken").AsString(); got != "import:200:4" {
			t.Fatalf("expected adoption token, got %q", got)
		}
		if !resp.Inputs.Get("keepUnclassifiedTattles").AsBool() {
			t.Fatal("expected imported keepUnclassifiedTattles=true")
		}
	})

	t.Run("missing config read returns empty ID", func(t *testing.T) {
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assertCMPRequest(t, r, http.MethodGet, "/v1/cookie-consent/configs/config-id")
			w.WriteHeader(http.StatusNotFound)
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		resp, err := server.Read(p.ReadRequest{
			ID:         "config-id",
			Urn:        cmpURN("CookieConsentPublication", "missing"),
			Properties: publicationStateProperties(),
			Inputs:     publicationInputProperties(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID != "" {
			t.Fatalf("expected empty ID, got %q", resp.ID)
		}
	})

	t.Run("delete is state only", func(t *testing.T) {
		requestCount := 0
		api := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			requestCount++
		}))
		defer api.Close()

		server := newCMPProviderServer(t, api.URL)
		if err := server.Delete(p.DeleteRequest{
			ID:         "config-id",
			Urn:        cmpURN("CookieConsentPublication", "deleted"),
			Properties: publicationStateProperties(),
		}); err != nil {
			t.Fatal(err)
		}
		if requestCount != 0 {
			t.Fatalf("delete made %d HTTP requests", requestCount)
		}
	})
}

func TestCookieConsentPublicationUpdatePreviewOverlaysInputs(t *testing.T) {
	t.Parallel()

	oldArgs := publicationArgsFixture()
	state := CookieConsentPublicationState{
		CookieConsentPublicationArgs: oldArgs,
		CustomerID:                   "customer-id",
		PublishStatus:                "published",
		LastPublished:                100,
		PublishedRevision:            3,
		ScriptSrc:                    "https://cmp.osano.com/customer-id/config-id/osano.js",
		ScriptTag:                    `<script src="https://cmp.osano.com/customer-id/config-id/osano.js"></script>`,
	}
	inputs := publicationArgsFixture()
	inputs.ChangeToken = "desired-state-v2"
	keep := false
	inputs.KeepUnclassifiedTattles = &keep
	description := "previewed publication options"
	inputs.Description = &description
	inputs.WebhookURL = nil

	resp, err := (&CookieConsentPublication{}).Update(
		t.Context(),
		infer.UpdateRequest[CookieConsentPublicationArgs, CookieConsentPublicationState]{
			State:  state,
			Inputs: inputs,
			DryRun: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resp.Output.CookieConsentPublicationArgs, inputs) {
		t.Fatalf("preview inputs were not overlaid: got %#v, want %#v", resp.Output.CookieConsentPublicationArgs, inputs)
	}
	if resp.Output.CustomerID != state.CustomerID ||
		resp.Output.PublishStatus != state.PublishStatus ||
		resp.Output.LastPublished != state.LastPublished ||
		resp.Output.PublishedRevision != state.PublishedRevision ||
		resp.Output.ScriptSrc != state.ScriptSrc ||
		resp.Output.ScriptTag != state.ScriptTag {
		t.Fatalf("preview did not retain computed outputs: got %#v, want outputs from %#v", resp.Output, state)
	}
}

func TestCookieConsentPublicationTimeoutRetainsEarlierDeadline(t *testing.T) {
	engineDeadline := time.Now().Add(time.Minute)
	ctx, engineCancel := context.WithDeadline(t.Context(), engineDeadline)
	defer engineCancel()

	publicationCtx, cancel := withPublicationTimeout(ctx, 20*time.Minute)
	defer cancel()
	got, ok := publicationCtx.Deadline()
	if !ok {
		t.Fatal("expected publication context deadline")
	}
	if !got.Equal(engineDeadline) {
		t.Fatalf("expected engine deadline %s, got %s", engineDeadline, got)
	}
}

func publicationArgsFixture() CookieConsentPublicationArgs {
	keep := true
	description := "release publication"
	webhookURL := "https://example.com/publish-complete"
	return CookieConsentPublicationArgs{
		ConfigID:                "config-id",
		ChangeToken:             "desired-state-v1",
		KeepUnclassifiedTattles: &keep,
		Description:             &description,
		WebhookURL:              &webhookURL,
	}
}

func zeroPublicationPollOptions() publicationPollOptions {
	return publicationPollOptions{
		InitialInterval: 0,
		MaxInterval:     0,
		Timeout:         time.Second,
		Sleep: func(context.Context, time.Duration) error {
			return nil
		},
	}
}

func publicationConfigFixture(status string, lastPublished, publishedRevision int) cmpConfigResponse {
	return cmpConfigResponse{
		ConfigID:            "config-id",
		CustomerID:          "customer-id",
		PublishStatus:       status,
		LastPublished:       lastPublished,
		PublishedRevision:   publishedRevision,
		Name:                "cookie-consent",
		Domains:             []string{"example.com"},
		Mode:                "production",
		Configuration:       map[string]any{"storagePolicyHref": "https://example.com/cookies"},
		TattleRecordStopped: false,
	}
}

func writePublicationConfigResponse(
	t *testing.T, w http.ResponseWriter, status string, lastPublished, publishedRevision int,
) {
	t.Helper()
	writeJSON(t, w, publicationConfigFixture(status, lastPublished, publishedRevision))
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("content-type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func assertPublicationBody(t *testing.T, r *http.Request, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected publication body: got %#v, want %#v", got, want)
	}
}

func assertPublishedState(
	t *testing.T,
	state CookieConsentPublicationState,
	wantArgs CookieConsentPublicationArgs,
	wantLastPublished int,
	wantPublishedRevision int,
) {
	t.Helper()
	if !reflect.DeepEqual(state.CookieConsentPublicationArgs, wantArgs) {
		t.Fatalf("unexpected publication args: %#v", state.CookieConsentPublicationArgs)
	}
	if state.CustomerID != "customer-id" || state.PublishStatus != "published" ||
		state.LastPublished != wantLastPublished || state.PublishedRevision != wantPublishedRevision {
		t.Fatalf("unexpected publication metadata: %#v", state)
	}
	if state.ScriptSrc != "https://cmp.osano.com/customer-id/config-id/osano.js" {
		t.Fatalf("unexpected script src %q", state.ScriptSrc)
	}
	if state.ScriptTag != `<script src="https://cmp.osano.com/customer-id/config-id/osano.js"></script>` {
		t.Fatalf("unexpected script tag %q", state.ScriptTag)
	}
}

func publicationInputProperties() property.Map {
	return property.NewMap(map[string]property.Value{
		"configId":                property.New("config-id"),
		"changeToken":             property.New("desired-state-v1"),
		"keepUnclassifiedTattles": property.New(true),
		"description":             property.New("release publication"),
		"webhookUrl":              property.New("https://example.com/publish-complete"),
	})
}

func publicationStateProperties() property.Map {
	return publicationInputProperties().
		Set("customerId", property.New("customer-id")).
		Set("publishStatus", property.New("published")).
		Set("lastPublished", property.New(100.0)).
		Set("publishedRevision", property.New(3.0)).
		Set("scriptSrc", property.New("https://cmp.osano.com/customer-id/config-id/osano.js")).
		Set("scriptTag", property.New(`<script src="https://cmp.osano.com/customer-id/config-id/osano.js"></script>`))
}

func emptyPublicationInputProperties() property.Map {
	return property.NewMap(map[string]property.Value{
		"configId": {}, "changeToken": {}, "keepUnclassifiedTattles": {}, "description": {}, "webhookUrl": {},
	})
}

func emptyPublicationStateProperties() property.Map {
	return emptyPublicationInputProperties().
		Set("customerId", property.Value{}).
		Set("publishStatus", property.Value{}).
		Set("lastPublished", property.Value{}).
		Set("publishedRevision", property.Value{}).
		Set("scriptSrc", property.Value{}).
		Set("scriptTag", property.Value{})
}

func assertPublicationProperties(
	t *testing.T, properties property.Map, changeToken string, lastPublished, publishedRevision int,
) {
	t.Helper()
	wantStrings := map[string]string{
		"configId":      "config-id",
		"changeToken":   changeToken,
		"customerId":    "customer-id",
		"publishStatus": "published",
		"scriptSrc":     "https://cmp.osano.com/customer-id/config-id/osano.js",
		"scriptTag":     `<script src="https://cmp.osano.com/customer-id/config-id/osano.js"></script>`,
	}
	for key, want := range wantStrings {
		if got := properties.Get(key).AsString(); got != want {
			t.Fatalf("expected %s=%q, got %q", key, want, got)
		}
	}
	if got := int(properties.Get("lastPublished").AsNumber()); got != lastPublished {
		t.Fatalf("expected lastPublished=%d, got %d", lastPublished, got)
	}
	if got := int(properties.Get("publishedRevision").AsNumber()); got != publishedRevision {
		t.Fatalf("expected publishedRevision=%d, got %d", publishedRevision, got)
	}
}
