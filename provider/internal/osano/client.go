// Package osano is the HTTP transport the provider uses for both Osano APIs: the Customer REST API
// and the Unified Consent Core API. It sends JSON, retries what is safe to retry, and keeps
// credentials and personal data out of error messages.
package osano

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxRetries     = 3
	defaultInitialBackoff = time.Second
	maxRetryAfterDelay    = time.Minute
	// maxBackoffShift caps the exponential backoff so a large retry count cannot overflow a Duration.
	maxBackoffShift = 16
	// maxResponseBytes bounds how much of a response the client reads into memory. Osano's largest
	// documented responses (1000 configurations per page) stay well below it.
	maxResponseBytes = 8 << 20
)

// Client is a JSON HTTP client for one Osano API base URL.
type Client struct {
	baseURL   *url.URL
	headers   http.Header
	userAgent string

	http *http.Client

	maxRetries     int
	initialBackoff time.Duration
}

// ClientOption customizes a Client.
type ClientOption func(*Client)

type writeRetriesKey struct{}

// WithWriteRetries marks ctx so POST requests made with it are retried on any retryable status.
// Use it only when repeating the POST cannot create a duplicate, for example when a replay is
// answered with 409 Conflict.
func WithWriteRetries(ctx context.Context) context.Context {
	return context.WithValue(ctx, writeRetriesKey{}, true)
}

func writeRetriesAllowed(ctx context.Context) bool {
	allowed, _ := ctx.Value(writeRetriesKey{}).(bool)
	return allowed
}

// WithMaxRetries overrides the retry count for retryable responses.
func WithMaxRetries(maxRetries int) ClientOption {
	return func(c *Client) {
		c.maxRetries = maxRetries
	}
}

// WithInitialBackoff overrides the initial exponential backoff delay.
func WithInitialBackoff(backoff time.Duration) ClientOption {
	return func(c *Client) {
		c.initialBackoff = backoff
	}
}

// WithUserAgent sends userAgent as the User-Agent header of every request when it is non-empty.
func WithUserAgent(userAgent string) ClientOption {
	return func(c *Client) {
		c.userAgent = userAgent
	}
}

// WithHeader sends a header, typically an API key, with every request. An empty value is ignored.
func WithHeader(name, value string) ClientOption {
	return func(c *Client) {
		if value != "" {
			c.headers.Set(name, value)
		}
	}
}

// WithHTTPClient uses httpClient for requests when it is non-nil.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		if httpClient != nil {
			c.http = httpClient
		}
	}
}

// NewHTTPClient returns an http.Client with a per-request timeout that never follows redirects.
// Osano documents no redirects, and following one would forward the API key header and replay
// request bodies to whatever host the redirect names.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// NewClient builds a client for baseURL. Requests are resolved under the base URL's path, so a
// prefix such as a proxy mount is kept.
func NewClient(baseURL *url.URL, opts ...ClientOption) *Client {
	client := &Client{
		baseURL:        baseURL,
		headers:        http.Header{},
		http:           NewHTTPClient(30 * time.Second),
		maxRetries:     defaultMaxRetries,
		initialBackoff: defaultInitialBackoff,
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

// Response is an accepted API response: one with a 2xx status or a status the request allowed.
type Response struct {
	StatusCode int
	Body       []byte
}

// Decode unmarshals the JSON body into out. An empty body leaves out untouched.
func (r Response) Decode(out any) error {
	if out == nil || len(bytes.TrimSpace(r.Body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(r.Body, out); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	return nil
}

// RequestOption customizes one request.
type RequestOption func(*requestOptions)

type requestOptions struct {
	headers       http.Header
	allowStatuses []int
}

// WithHeaders adds headers to one request, for example a second API key or geolocation overrides.
func WithHeaders(headers http.Header) RequestOption {
	return func(o *requestOptions) {
		if o.headers == nil {
			o.headers = http.Header{}
		}
		for name, values := range headers {
			for _, value := range values {
				o.headers.Add(name, value)
			}
		}
	}
}

// AllowStatus accepts the given non-2xx statuses as responses instead of errors, so a caller can
// treat, for example, a documented 404 as "not found".
func AllowStatus(statuses ...int) RequestOption {
	return func(o *requestOptions) {
		o.allowStatuses = append(o.allowStatuses, statuses...)
	}
}

// DoJSON sends a JSON request and decodes the JSON response into out when it is non-nil.
func (c *Client) DoJSON(ctx context.Context, method, pth string, query url.Values, in, out any) error {
	resp, err := c.Do(ctx, method, pth, query, in)
	if err != nil {
		return err
	}
	return resp.Decode(out)
}

// Do sends a JSON request, retrying retryable failures, and returns the accepted response.
//
// Retries: a GET is repeated after a transport failure and after 429 or 5xx responses. Every other
// method is repeated after 429 or 5xx, except a POST, which is repeated only after 429 (a 5xx may
// have been processed, and Osano documents no idempotency keys) unless ctx carries
// WithWriteRetries. Retry-After is honored, capped at one minute; otherwise the delay doubles from
// the initial backoff.
//
// pth arrives with its segments already escaped. A response body larger than 8 MiB is an error.
// Errors never contain the request path or query, which can hold identifiers such as a session ID.
func (c *Client) Do(
	ctx context.Context, method, pth string, query url.Values, in any, opts ...RequestOption,
) (Response, error) {
	var options requestOptions
	for _, opt := range opts {
		opt(&options)
	}

	requestURL, err := c.requestURL(pth, query)
	if err != nil {
		return Response{}, err
	}

	var bodyBytes []byte
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return Response{}, fmt.Errorf("marshal request: %w", err)
		}
		bodyBytes = encoded
	}

	maxRetries := max(c.maxRetries, 0)
	for attempt := 0; ; attempt++ {
		req, err := c.newRequest(ctx, method, requestURL, bodyBytes, options.headers)
		if err != nil {
			return Response{}, err
		}

		resp, err := c.http.Do(req)
		if err != nil {
			// Reads are safe to repeat after a transport failure, such as a reset during a long publish poll.
			if method == http.MethodGet && ctx.Err() == nil && attempt < maxRetries {
				if sleepErr := sleepWithContext(ctx, c.retryDelay(nil, attempt)); sleepErr != nil {
					return Response{}, sleepErr
				}
				continue
			}
			return Response{}, transportError(method, requestURL, err)
		}

		payload, err := readBody(resp)
		if err != nil {
			return Response{}, err
		}

		if statusAccepted(resp.StatusCode, options.allowStatuses) {
			return Response{StatusCode: resp.StatusCode, Body: payload}, nil
		}

		if shouldRetryRequest(ctx, method, resp.StatusCode) && attempt < maxRetries {
			if err := sleepWithContext(ctx, c.retryDelay(resp, attempt)); err != nil {
				return Response{}, err
			}
			continue
		}

		return Response{}, &HTTPError{StatusCode: resp.StatusCode, Body: string(payload)}
	}
}

// requestURL joins pth under the base URL's path and encodes the query with %20 for spaces: Osano
// documents URL-encoded spaces for filters such as the configuration name search, while url.Values
// encodes them as "+". A literal "+" is escaped as %2B, so any "+" left is a space.
func (c *Client) requestURL(pth string, query url.Values) (*url.URL, error) {
	for _, segment := range strings.Split(strings.Trim(pth, "/"), "/") {
		if segment == "." || segment == ".." {
			return nil, fmt.Errorf("invalid request path %q: %q is not an allowed path segment", pth, segment)
		}
	}
	requestURL := *c.baseURL
	escapedPath := path.Join("/", c.baseURL.EscapedPath(), pth)
	unescapedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		return nil, fmt.Errorf("invalid request path %q: %w", pth, err)
	}
	requestURL.Path = unescapedPath
	requestURL.RawPath = escapedPath
	requestURL.RawQuery = strings.ReplaceAll(query.Encode(), "+", "%20")
	return &requestURL, nil
}

func (c *Client) newRequest(
	ctx context.Context, method string, requestURL *url.URL, body []byte, extra http.Header,
) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	for _, headers := range []http.Header{c.headers, extra} {
		for name, values := range headers {
			for _, value := range values {
				req.Header.Add(name, value)
			}
		}
	}
	return req, nil
}

func readBody(resp *http.Response) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	closeErr := resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close response: %w", closeErr)
	}
	if len(payload) > maxResponseBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxResponseBytes)
	}
	return payload, nil
}

// transportError reports a request that got no response. *url.Error prints the full URL, whose
// path and query can hold identifiers such as a session ID or an audit-log actor email, so only
// the method and host are reported.
func transportError(method string, target *url.URL, err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	return fmt.Errorf("Osano API request failed: %s %s://%s: %w", method, target.Scheme, target.Host, err)
}

func statusAccepted(status int, allowed []int) bool {
	if status >= 200 && status < 300 {
		return true
	}
	for _, candidate := range allowed {
		if status == candidate {
			return true
		}
	}
	return false
}

func shouldRetry(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= 500
}

// shouldRetryRequest avoids replaying a POST after a 5xx response, where the server may already
// have processed the request. 429 means it was not.
func shouldRetryRequest(ctx context.Context, method string, statusCode int) bool {
	if !shouldRetry(statusCode) {
		return false
	}
	if method != http.MethodPost || writeRetriesAllowed(ctx) {
		return true
	}
	return statusCode == http.StatusTooManyRequests
}

func (c *Client) retryDelay(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		if delay, ok := retryAfterDelay(resp.Header.Get("Retry-After")); ok {
			return min(delay, maxRetryAfterDelay)
		}
	}

	backoff := c.initialBackoff
	if backoff <= 0 {
		backoff = defaultInitialBackoff
	}
	shift := min(max(attempt, 0), maxBackoffShift)
	return min(backoff*time.Duration(1<<shift), maxRetryAfterDelay)
}

func retryAfterDelay(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, false
		}
		if seconds > int(maxRetryAfterDelay/time.Second) {
			return maxRetryAfterDelay, true
		}
		return time.Duration(seconds) * time.Second, true
	}

	if t, err := http.ParseTime(value); err == nil {
		delay := time.Until(t)
		if delay < 0 {
			return 0, true
		}
		return min(delay, maxRetryAfterDelay), true
	}

	return 0, false
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
