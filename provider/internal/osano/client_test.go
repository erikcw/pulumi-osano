package osano

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recorder captures what a test server received, safely across the handler goroutine.
type recorder struct {
	mu       sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	Method  string
	Path    string
	RawPath string
	Query   string
	Header  http.Header
	Body    string
}

func (r *recorder) record(req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, recordedRequest{
		Method:  req.Method,
		Path:    req.URL.Path,
		RawPath: req.URL.EscapedPath(),
		Query:   req.URL.RawQuery,
		Header:  req.Header.Clone(),
		Body:    string(body),
	})
}

func (r *recorder) all() []recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedRequest(nil), r.requests...)
}

func (r *recorder) last(t *testing.T) recordedRequest {
	t.Helper()
	requests := r.all()
	if len(requests) == 0 {
		t.Fatal("no request was recorded")
	}
	return requests[len(requests)-1]
}

func newTestClient(t *testing.T, serverURL string, opts ...ClientOption) *Client {
	t.Helper()
	baseURL, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	return NewClient(baseURL, append([]ClientOption{WithInitialBackoff(time.Millisecond)}, opts...)...)
}

func TestNewClientDefaults(t *testing.T) {
	t.Parallel()

	baseURL, _ := url.Parse("https://api.example.com")
	client := NewClient(baseURL, WithHeader("x-api-key", "test-key"))

	if client.maxRetries != defaultMaxRetries {
		t.Fatalf("expected maxRetries %d, got %d", defaultMaxRetries, client.maxRetries)
	}
	if client.initialBackoff != defaultInitialBackoff {
		t.Fatalf("expected initialBackoff %v, got %v", defaultInitialBackoff, client.initialBackoff)
	}
	if got := client.headers.Get("x-api-key"); got != "test-key" {
		t.Fatalf("expected the API key header to be set, got %q", got)
	}
	if client.http.CheckRedirect == nil {
		t.Fatal("expected the default HTTP client to refuse redirects")
	}
}

func TestNewClientOptions(t *testing.T) {
	t.Parallel()

	baseURL, _ := url.Parse("https://api.example.com")
	httpClient := &http.Client{Timeout: 17 * time.Second}
	client := NewClient(
		baseURL,
		WithMaxRetries(5),
		WithInitialBackoff(2*time.Second),
		WithHTTPClient(httpClient),
		WithHeader("x-empty", ""),
	)

	if client.maxRetries != 5 {
		t.Fatalf("expected maxRetries 5, got %d", client.maxRetries)
	}
	if client.initialBackoff != 2*time.Second {
		t.Fatalf("expected initialBackoff 2s, got %v", client.initialBackoff)
	}
	if client.http != httpClient {
		t.Fatal("expected NewClient to retain the provided HTTP client")
	}
	if _, set := client.headers["X-Empty"]; set {
		t.Fatal("an empty header value must not be sent")
	}
}

func TestShouldRetry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status int
		want   bool
	}{
		{200, false},
		{201, false},
		{204, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{422, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			t.Parallel()

			if got := shouldRetry(tc.status); got != tc.want {
				t.Fatalf("shouldRetry(%d) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestRetryAfterDelay(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		value     string
		wantDelay time.Duration
		wantOK    bool
	}{
		{"empty", "", 0, false},
		{"seconds", "5", 5 * time.Second, true},
		{"zero seconds", "0", 0, true},
		{"negative seconds", "-1", 0, false},
		{"with spaces", " 5 ", 5 * time.Second, true},
		{"invalid string", "abc", 0, false},
		{"seconds above the cap", "3600", maxRetryAfterDelay, true},
		{"seconds that would overflow a Duration", "9223372037", maxRetryAfterDelay, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			delay, ok := retryAfterDelay(tc.value)
			if ok != tc.wantOK {
				t.Fatalf("retryAfterDelay(%q) ok = %v, want %v", tc.value, ok, tc.wantOK)
			}
			if delay != tc.wantDelay {
				t.Fatalf("retryAfterDelay(%q) delay = %v, want %v", tc.value, delay, tc.wantDelay)
			}
		})
	}
}

func TestRetryAfterDelayHTTPDate(t *testing.T) {
	t.Parallel()

	delay, ok := retryAfterDelay("Wed, 21 Oct 2015 07:28:00 GMT")
	if !ok {
		t.Fatal("expected HTTP-date Retry-After to be accepted")
	}
	if delay != 0 {
		t.Fatalf("expected a past HTTP-date Retry-After delay of 0, got %s", delay)
	}

	future := time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat)
	delay, ok = retryAfterDelay(future)
	if !ok || delay != maxRetryAfterDelay {
		t.Fatalf("expected a far-future HTTP-date to be capped at %s, got %s (ok=%v)", maxRetryAfterDelay, delay, ok)
	}
}

func TestRetryDelay(t *testing.T) {
	t.Parallel()

	baseURL, _ := url.Parse("https://api.example.com")
	client := NewClient(baseURL, WithInitialBackoff(100*time.Millisecond))

	t.Run("exponential backoff", func(t *testing.T) {
		for attempt, want := range []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond} {
			if got := client.retryDelay(nil, attempt); got != want {
				t.Fatalf("attempt %d: expected %s, got %s", attempt, want, got)
			}
		}
	})

	t.Run("retry-after header overrides backoff", func(t *testing.T) {
		resp := &http.Response{Header: http.Header{}}
		resp.Header.Set("Retry-After", "3")

		if delay := client.retryDelay(resp, 0); delay != 3*time.Second {
			t.Fatalf("expected 3s from Retry-After, got %v", delay)
		}
	})

	t.Run("negative attempt clamped to zero", func(t *testing.T) {
		if delay := client.retryDelay(nil, -1); delay != 100*time.Millisecond {
			t.Fatalf("expected 100ms for negative attempt, got %v", delay)
		}
	})

	t.Run("backoff never exceeds the cap or overflows", func(t *testing.T) {
		for _, attempt := range []int{20, 34, 63, 1000} {
			if delay := client.retryDelay(nil, attempt); delay != maxRetryAfterDelay {
				t.Fatalf("attempt %d: expected the %s cap, got %s", attempt, maxRetryAfterDelay, delay)
			}
		}
	})
}

func TestRetryDelayCapsRetryAfter(t *testing.T) {
	t.Parallel()

	baseURL, _ := url.Parse("https://api.example.com")
	client := NewClient(baseURL)
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "3600")

	if delay := client.retryDelay(resp, 0); delay != maxRetryAfterDelay {
		t.Fatalf("expected Retry-After to be capped at %s, got %s", maxRetryAfterDelay, delay)
	}
}

func TestSleepWithContext(t *testing.T) {
	t.Parallel()

	t.Run("normal sleep", func(t *testing.T) {
		if err := sleepWithContext(context.Background(), time.Millisecond); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("zero duration", func(t *testing.T) {
		if err := sleepWithContext(context.Background(), 0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("negative duration", func(t *testing.T) {
		if err := sleepWithContext(context.Background(), -time.Second); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := sleepWithContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})
}

func TestDoJSONGetSuccess(t *testing.T) {
	t.Parallel()

	var rec recorder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "123"})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, WithHeader("x-api-key", "test-key"), WithUserAgent("pulumi-osano/1.2.3"))

	var out map[string]string
	if err := client.DoJSON(context.Background(), http.MethodGet, "/test", nil, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["id"] != "123" {
		t.Fatalf("expected id=123, got %v", out)
	}
	got := rec.last(t)
	if got.Method != http.MethodGet || got.Path != "/test" {
		t.Fatalf("unexpected request %s %s", got.Method, got.Path)
	}
	for name, want := range map[string]string{
		"x-api-key": "test-key", "Accept": "application/json", "User-Agent": "pulumi-osano/1.2.3", "Content-Type": "",
	} {
		if value := got.Header.Get(name); value != want {
			t.Fatalf("expected header %s=%q, got %q", name, want, value)
		}
	}
}

func TestDoJSONPostSuccess(t *testing.T) {
	t.Parallel()

	var rec recorder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "456"})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)

	var out map[string]string
	if err := client.DoJSON(
		context.Background(), http.MethodPost, "/items", nil, map[string]string{"name": "test"}, &out,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["id"] != "456" {
		t.Fatalf("expected id=456, got %v", out)
	}
	got := rec.last(t)
	if got.Method != http.MethodPost || got.Header.Get("Content-Type") != "application/json" ||
		got.Body != `{"name":"test"}` {
		t.Fatalf("unexpected POST %#v", got)
	}
}

func TestDoReturnsAllowedStatusesAndPerRequestHeaders(t *testing.T) {
	t.Parallel()

	var rec recorder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"missing"}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, WithHeader("x-api-key", "test-key"))
	extra := http.Header{}
	extra.Set("x-country-code-override", "US")
	resp, err := client.Do(context.Background(), http.MethodGet, "/things/1", nil, nil,
		WithHeaders(extra), AllowStatus(http.StatusNotFound))
	if err != nil {
		t.Fatalf("an allowed 404 must not be an error, got %v", err)
	}
	if resp.StatusCode != http.StatusNotFound || string(resp.Body) != `{"message":"missing"}` {
		t.Fatalf("unexpected response %#v", resp)
	}
	got := rec.last(t)
	if got.Header.Get("x-api-key") != "test-key" || got.Header.Get("x-country-code-override") != "US" {
		t.Fatalf("expected both the default and the per-request header, got %v", got.Header)
	}

	_, err = client.Do(context.Background(), http.MethodGet, "/things/1", nil, nil)
	if !IsHTTPStatus(err, http.StatusNotFound) {
		t.Fatalf("a 404 that is not allowed must be an HTTPError, got %v", err)
	}
}

func TestDoJSONErrorNonRetryable(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, WithMaxRetries(3))

	err := client.DoJSON(context.Background(), http.MethodGet, "/missing", nil, nil, nil)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound || httpErr.Body != `{"error":"not found"}` {
		t.Fatalf("expected a 404 HTTPError with the body, got %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("a 404 must not be retried, got %d attempts", got)
	}
}

func TestDoJSONRetrySuccess(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := attempts.Add(1)
		if count <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, WithMaxRetries(3))

	var out map[string]string
	if err := client.DoJSON(context.Background(), http.MethodGet, "/test", nil, nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
	if out["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", out)
	}
}

func TestDoJSONRetryExhausted(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, WithMaxRetries(2))

	err := client.DoJSON(context.Background(), http.MethodGet, "/test", nil, nil, nil)
	if !IsHTTPStatus(err, http.StatusInternalServerError) {
		t.Fatalf("expected a 500 HTTPError after retries, got %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestDoJSONPreservesEscapedPathSegments(t *testing.T) {
	t.Parallel()

	var rec recorder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL+"/root")

	pth := "/v1/configs/" + url.PathEscape("a/b c")
	if err := client.DoJSON(context.Background(), http.MethodGet, pth, nil, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "/root/v1/configs/a%2Fb%20c"; rec.last(t).RawPath != want {
		t.Fatalf("expected request path %q, got %q", want, rec.last(t).RawPath)
	}
}

// A path segment of "." or ".." would be collapsed by path cleaning and address a different endpoint.
func TestDoRejectsDotSegments(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("no request must be sent, got %s %s", r.Method, r.URL)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	for _, pth := range []string{"/v1/configs/..", "/v1/configs/./publish", "/v1/configs/../rules"} {
		_, err := client.Do(context.Background(), http.MethodGet, pth, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "is not an allowed path segment") {
			t.Fatalf("%s: expected a dot-segment error, got %v", pth, err)
		}
	}
}

func TestDoJSONPostRetryPolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		status       int
		writeRetries bool
		wantAttempts int
	}{
		{"502 is not replayed", http.StatusBadGateway, false, 1},
		{"500 is not replayed", http.StatusInternalServerError, false, 1},
		{"503 is not replayed", http.StatusServiceUnavailable, false, 1},
		{"504 is not replayed", http.StatusGatewayTimeout, false, 1},
		{"429 is retried", http.StatusTooManyRequests, false, 3},
		{"502 is retried when write retries are allowed", http.StatusBadGateway, true, 3},
		{"503 is retried when write retries are allowed", http.StatusServiceUnavailable, true, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var rec recorder
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.record(r)
				w.WriteHeader(tc.status)
			}))
			defer server.Close()

			client := newTestClient(t, server.URL, WithMaxRetries(2))
			ctx := context.Background()
			if tc.writeRetries {
				ctx = WithWriteRetries(ctx)
			}

			err := client.DoJSON(ctx, http.MethodPost, "/items", nil, map[string]string{"name": "x"}, nil)
			if !IsHTTPStatus(err, tc.status) {
				t.Fatalf("expected final HTTP %d error, got %v", tc.status, err)
			}
			requests := rec.all()
			if len(requests) != tc.wantAttempts {
				t.Fatalf("expected %d attempts, got %d", tc.wantAttempts, len(requests))
			}
			// Every replay must carry the full body, not the consumed reader of the first attempt.
			for i, request := range requests {
				if request.Body != `{"name":"x"}` {
					t.Fatalf("attempt %d sent body %q", i+1, request.Body)
				}
			}
		})
	}
}

func TestDoRetriesPatchAndDeleteOnServerErrors(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			client := newTestClient(t, server.URL)
			if err := client.DoJSON(context.Background(), method, "/items/1", nil, nil, nil); err != nil {
				t.Fatalf("expected %s to recover, got %v", method, err)
			}
			if got := attempts.Load(); got != 2 {
				t.Fatalf("expected 2 attempts, got %d", got)
			}
		})
	}
}

func TestDoJSONRetriesTransportErrorsForGetOnly(t *testing.T) {
	t.Parallel()

	newFlakyServer := func(attempts *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if attempts.Add(1) == 1 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))
	}

	t.Run("GET retries after a dropped connection", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		server := newFlakyServer(&attempts)
		defer server.Close()
		client := newTestClient(t, server.URL)
		if err := client.DoJSON(context.Background(), http.MethodGet, "/poll", nil, nil, nil); err != nil {
			t.Fatalf("expected GET to recover, got %v", err)
		}
		if got := attempts.Load(); got != 2 {
			t.Fatalf("expected 2 attempts, got %d", got)
		}
	})

	t.Run("POST does not replay after a dropped connection", func(t *testing.T) {
		t.Parallel()
		var attempts atomic.Int32
		server := newFlakyServer(&attempts)
		defer server.Close()
		client := newTestClient(t, server.URL)
		err := client.DoJSON(context.Background(), http.MethodPost, "/create", nil, map[string]string{"a": "b"}, nil)
		if err == nil {
			t.Fatal("expected POST transport error")
		}
		if got := attempts.Load(); got != 1 {
			t.Fatalf("expected a single POST attempt, got %d", got)
		}
	})
}

// A request that gets no response must not print its URL: the path and query can hold a session
// ID or an audit-log actor email.
func TestTransportErrorsRedactThePathAndQuery(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	baseURL := server.URL
	server.Close()

	client := newTestClient(t, baseURL, WithMaxRetries(0))
	query := url.Values{"actor": []string{"jane@example.com"}}
	_, err := client.Do(context.Background(), http.MethodGet, "/v2/sessions/SECRET-SESSION", query, nil)
	if err == nil {
		t.Fatal("expected the request to fail")
	}
	message := err.Error()
	for _, leaked := range []string{"SECRET-SESSION", "jane", "/v2/"} {
		if strings.Contains(message, leaked) {
			t.Fatalf("error reveals the request path or query: %v", err)
		}
	}
	host := strings.TrimPrefix(baseURL, "http://")
	if !strings.HasPrefix(message, "Osano API request failed: GET http://"+host+": ") {
		t.Fatalf("unexpected transport error format: %v", err)
	}
}

// Following a redirect would forward the API key header and replay the body to the redirect target.
func TestDoDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		leaked.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/elsewhere", http.StatusFound)
	}))
	defer origin.Close()

	client := newTestClient(t, origin.URL, WithHeader("x-api-key", "test-key"))
	err := client.DoJSON(context.Background(), http.MethodPost, "/publish", nil, map[string]string{"secret": "x"}, nil)
	if !IsHTTPStatus(err, http.StatusFound) {
		t.Fatalf("expected the 302 to surface as an HTTPError, got %v", err)
	}
	if got := leaked.Load(); got != 0 {
		t.Fatalf("the redirect target received %d request(s)", got)
	}
}

func TestDoRejectsOversizedBodies(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"padding":"`))
		_, _ = io.CopyN(w, strings.NewReader(strings.Repeat("x", 1<<16)), 1<<16)
		for written := 1 << 16; written <= maxResponseBytes; written += 1 << 16 {
			_, _ = w.Write([]byte(strings.Repeat("x", 1<<16)))
		}
		_, _ = w.Write([]byte(`"}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	err := client.DoJSON(context.Background(), http.MethodGet, "/huge", nil, nil, &map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("expected an oversized-body error, got %v", err)
	}
}

func TestHTTPErrorTruncatesLongBodies(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("y", maxErrorBodyBytes*2)
	err := &HTTPError{StatusCode: http.StatusBadGateway, Body: long}
	message := err.Error()
	if len(message) > maxErrorBodyBytes+64 || !strings.HasSuffix(message, "... (truncated)") {
		t.Fatalf("expected a truncated body, got %d bytes ending %q", len(message), message[len(message)-20:])
	}
	short := &HTTPError{StatusCode: http.StatusBadRequest, Body: `{"message":"bad"}`}
	if short.Error() != `osano api error: status=400 body={"message":"bad"}` {
		t.Fatalf("unexpected short error %q", short.Error())
	}
}

// Osano documents URL-encoded spaces (%20) for query filters such as the config name search, while
// url.Values encodes spaces as "+". A literal "+" must still arrive as a plus sign.
func TestDoJSONEncodesQuerySpacesAsPercent20(t *testing.T) {
	t.Parallel()

	var rec recorder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	query := url.Values{"name": []string{"marketing site+eu"}}
	if err := client.DoJSON(context.Background(), http.MethodGet, "/v1/configs", query, nil, nil); err != nil {
		t.Fatal(err)
	}
	got := rec.last(t)
	if got.Query != "name=marketing%20site%2Beu" {
		t.Fatalf("unexpected raw query %q", got.Query)
	}
	if decoded, err := url.ParseQuery(got.Query); err != nil || decoded.Get("name") != "marketing site+eu" {
		t.Fatalf("expected the decoded name to round-trip, got %v (%v)", decoded, err)
	}
}

func TestResponseDecode(t *testing.T) {
	t.Parallel()

	var out map[string]string
	if err := (Response{Body: []byte(" \n")}).Decode(&out); err != nil || out != nil {
		t.Fatalf("an empty body must leave out untouched, got %v (%v)", out, err)
	}
	if err := (Response{Body: []byte(`{"a":"b"}`)}).Decode(&out); err != nil || out["a"] != "b" {
		t.Fatalf("expected the body to decode, got %v (%v)", out, err)
	}
	err := (Response{Body: []byte(`{not json`)}).Decode(&out)
	if err == nil || !strings.Contains(err.Error(), "unmarshal response") {
		t.Fatalf("expected an unmarshal error, got %v", err)
	}
}
