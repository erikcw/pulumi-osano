package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	osanoclient "github.com/jflavan/pulumi-osano/provider/internal/osano"
)

type headerKind int

const (
	// headerUnifiedConsent authenticates with the Unified Consent API key.
	headerUnifiedConsent headerKind = iota
	// headerSubject authenticates subject-verification routes with every configured key. Osano's guide
	// requires the Osano key for routes that create or verify subjects, while its OpenAPI spec lists
	// the Unified Consent key for these routes, so either key alone is enough.
	headerSubject
)

// Unified Consent reference types accepted by the ref query parameter.
const (
	referenceTypeSubject = "subject"
	referenceTypeSession = "session"
	// referenceTypeAnonymous was never an Osano reference type: anonymous IDs are subject references.
	// It is accepted as a deprecated alias of subject so existing programs keep working.
	referenceTypeAnonymous = "anonymous"
)

// normalizeReferenceType maps a user-supplied reference type to the ref value Osano accepts.
func normalizeReferenceType(referenceType string) (string, error) {
	switch strings.TrimSpace(referenceType) {
	case "", referenceTypeSubject, referenceTypeAnonymous:
		return referenceTypeSubject, nil
	case referenceTypeSession:
		return referenceTypeSession, nil
	default:
		return "", fmt.Errorf(
			"referenceType must be subject or session (anonymous is a deprecated alias of subject); got %q",
			referenceType,
		)
	}
}

// geoOverride carries the optional country and region Osano uses instead of resolving the caller's
// IP address, which in a pipeline is the CI runner's address rather than the subject's.
type geoOverride struct {
	CountryCode string
	RegionCode  string
}

var (
	countryCodePattern = regexp.MustCompile(`^[A-Za-z]{2}$`)
	regionCodePattern  = regexp.MustCompile(`^[A-Za-z]{2}-[A-Za-z0-9]{1,3}$`)
)

// validateGeoOverride checks the ISO 3166 formats of the override headers. Osano answers a
// malformed code with 400, which a unified consent lookup would read as "no consent".
func validateGeoOverride(country, region *string) error {
	if country != nil {
		if code := strings.TrimSpace(*country); code != "" && !countryCodePattern.MatchString(code) {
			return fmt.Errorf("countryCodeOverride must be an ISO 3166-1 alpha-2 code such as US; got %q", *country)
		}
	}
	if region != nil {
		if code := strings.TrimSpace(*region); code != "" && !regionCodePattern.MatchString(code) {
			return fmt.Errorf("regionCodeOverride must be an ISO 3166-2 code such as US-CA; got %q", *region)
		}
	}
	return nil
}

// headers returns the override headers. ISO 3166 codes are upper case, so a lower-case code is
// normalized rather than sent as Osano would reject it.
func (g geoOverride) headers() http.Header {
	headers := http.Header{}
	if code := strings.ToUpper(strings.TrimSpace(g.CountryCode)); code != "" {
		headers.Set("x-country-code-override", code)
	}
	if code := strings.ToUpper(strings.TrimSpace(g.RegionCode)); code != "" {
		headers.Set("x-region-code-override", code)
	}
	return headers
}

// apiClient calls the Unified Consent Core API through the shared transport.
type apiClient struct {
	settings *settings
	client   *osanoclient.Client
	// err is a configuration error found while resolving the settings; it is reported by every request.
	err error
}

func newAPIClient(ctx context.Context) *apiClient {
	resolved, err := infer.GetConfig[Config](ctx).resolved()
	if err != nil {
		return &apiClient{err: err}
	}
	return &apiClient{settings: resolved, client: resolved.unifiedConsent}
}

func (c *apiClient) CreateConsent(
	ctx context.Context, payload consentRequestPayload, geo geoOverride,
) (map[string]any, error) {
	body, _, err := c.doJSONWithHeaders(
		ctx, http.MethodPost, "/v2/consents", nil, payload, geo.headers(), headerUnifiedConsent,
	)
	if err != nil {
		return nil, err
	}

	data := map[string]any{}
	if err := decodeOptionalJSON(body, &data, "consent"); err != nil {
		return nil, err
	}
	return data, nil
}

// CreateGPCConsent submits a Global Privacy Control consent. Osano derives the actions from the
// configuration's privacy protocols and the subject's jurisdiction, and returns them.
func (c *apiClient) CreateGPCConsent(
	ctx context.Context, payload gpcConsentRequestPayload, geo geoOverride,
) ([]ConsentAction, error) {
	body, _, err := c.doJSONWithHeaders(
		ctx, http.MethodPost, "/v2/consents/gpc", nil, payload, geo.headers(), headerUnifiedConsent,
	)
	if err != nil {
		return nil, err
	}

	var data struct {
		GPCActions []ConsentAction `json:"gpcActions"`
	}
	if err := decodeOptionalJSON(body, &data, "GPC consent"); err != nil {
		return nil, err
	}
	return data.GPCActions, nil
}

// FetchUnifiedConsent reads the merged consent of a subject. Osano documents 400 as its answer when
// the subject has no consent, so 400 reports found=false.
func (c *apiClient) FetchUnifiedConsent(
	ctx context.Context,
	subjectRef, referenceType string,
	geo geoOverride,
) (*unifiedConsentPayload, bool, error) {
	ref, err := normalizeReferenceType(referenceType)
	if err != nil {
		return nil, false, err
	}

	query := url.Values{}
	query.Set("ref", ref)

	path := "/v2/consents/unified/" + url.PathEscape(subjectRef)
	body, status, err := c.doJSONWithHeaders(
		ctx, http.MethodGet, path, query, nil, geo.headers(), headerUnifiedConsent, http.StatusBadRequest,
	)
	if err != nil {
		return nil, false, err
	}
	if status == http.StatusBadRequest {
		return nil, false, nil
	}

	var data unifiedConsentPayload
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, false, fmt.Errorf("failed to decode unified consent payload: %w", err)
	}
	return &data, true, nil
}

// FetchSubject resolves a subject reference. Only 404 means the subject is unknown; any other
// error, including a 400 for a malformed request or key, is reported.
func (c *apiClient) FetchSubject(ctx context.Context, subjectRef, referenceType string) (*subjectPayload, bool, error) {
	ref, err := normalizeReferenceType(referenceType)
	if err != nil {
		return nil, false, err
	}

	query := url.Values{}
	query.Set("ref", ref)

	path := "/v2/subjects/" + url.PathEscape(subjectRef)
	body, status, err := c.doJSON(ctx, http.MethodGet, path, query, nil, headerUnifiedConsent, http.StatusNotFound)
	if err != nil {
		return nil, false, err
	}
	if status == http.StatusNotFound {
		return nil, false, nil
	}

	var data subjectPayload
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, false, fmt.Errorf("failed to decode subject payload: %w", err)
	}
	return &data, true, nil
}

func (c *apiClient) FetchConfig(ctx context.Context) (map[string]any, error) {
	body, _, err := c.doJSON(ctx, http.MethodGet, "/v2/config", nil, nil, headerUnifiedConsent)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to decode config payload: %w", err)
	}
	return data, nil
}

func (c *apiClient) FetchCollections(
	ctx context.Context,
	jurisdiction, collectionType string,
) (*collectionsPayload, error) {
	query := url.Values{}
	if trimmed := strings.TrimSpace(jurisdiction); trimmed != "" {
		query.Set("jurisdiction", trimmed)
	}
	if trimmed := strings.TrimSpace(collectionType); trimmed != "" {
		query.Set("type", trimmed)
	}

	body, _, err := c.doJSON(ctx, http.MethodGet, "/v2/collections", query, nil, headerUnifiedConsent)
	if err != nil {
		return nil, err
	}

	var data collectionsPayload
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to decode collections payload: %w", err)
	}
	return &data, nil
}

func (c *apiClient) FetchCollection(
	ctx context.Context,
	collectionID string,
) (collection map[string]any, found bool, err error) {
	return c.fetchOptionalObject(ctx, "/v2/collections/"+url.PathEscape(collectionID), "collection")
}

func (c *apiClient) CheckConsent(ctx context.Context, subjectID string, geo geoOverride) (bool, error) {
	path := "/v2/consents/check/" + url.PathEscape(subjectID)
	body, _, err := c.doJSONWithHeaders(ctx, http.MethodGet, path, nil, nil, geo.headers(), headerUnifiedConsent)
	if err != nil {
		return false, err
	}

	var resp struct {
		Exists bool `json:"exists"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return false, fmt.Errorf("failed to decode consent check payload: %w", err)
	}
	return resp.Exists, nil
}

// FetchConsentProfile reads the consent profile of a hashed subject ID. Osano documents 400 as its
// answer when there is no consent, so 400 and 404 report found=false.
func (c *apiClient) FetchConsentProfile(
	ctx context.Context,
	hashedSubjectID, configID string,
	geo geoOverride,
) (profile map[string]any, found bool, err error) {
	query := url.Values{}
	query.Set("configId", configID)
	path := "/v2/consent-profiles/" + url.PathEscape(hashedSubjectID)
	body, status, err := c.doJSONWithHeaders(
		ctx, http.MethodGet, path, query, nil, geo.headers(), headerUnifiedConsent,
		http.StatusBadRequest, http.StatusNotFound,
	)
	if err != nil {
		return nil, false, err
	}
	if status == http.StatusBadRequest || status == http.StatusNotFound {
		return nil, false, nil
	}

	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, false, fmt.Errorf("failed to decode consent profile payload: %w", err)
	}
	return profile, true, nil
}

// SendVerificationCode sends a one-time code and returns the decoded response body. Osano does not
// document the response; for SMS it may carry the session that verification requires.
func (c *apiClient) SendVerificationCode(ctx context.Context, req sendCodeRequest) (map[string]any, error) {
	endpoint := "/v2/subjects/send-code"
	if req.Channel == "sms" {
		endpoint = "/v2/subjects/send-code/sms"
	}

	body, _, err := c.doJSON(ctx, http.MethodPost, endpoint, nil, req.Payload(), headerSubject)
	if err != nil {
		return nil, withheldSubjectBody(err)
	}

	var data map[string]any
	if strings.TrimSpace(string(body)) != "" {
		// The response is undocumented, so a body that is not a JSON object is ignored.
		if err := json.Unmarshal(body, &data); err != nil {
			return nil, nil
		}
	}
	return data, nil
}

func (c *apiClient) VerifySubjectCode(ctx context.Context, req verifyRequest) (map[string]any, error) {
	var endpoint string
	switch req.Channel {
	case "email":
		endpoint = "/v2/subjects/profile/verify"
	case "sms":
		endpoint = "/v2/subjects/profile/verify/sms"
	default:
		return nil, fmt.Errorf("unsupported verification channel: %s", req.Channel)
	}

	body, _, err := c.doJSON(ctx, http.MethodPost, endpoint, nil, req.Payload(), headerSubject)
	if err != nil {
		return nil, withheldSubjectBody(err)
	}

	var data map[string]any
	if err := decodeOptionalJSON(body, &data, "verification"); err != nil {
		return nil, err
	}
	return data, nil
}

// withheldSubjectBody drops the body of an error response to a subject-verification route. The
// request carried the subject's email address or phone number, which a validation error may echo.
func withheldSubjectBody(err error) error {
	var httpErr *osanoclient.HTTPError
	if errors.As(err, &httpErr) {
		return &osanoclient.HTTPError{
			StatusCode: httpErr.StatusCode,
			Body:       "(body withheld: the request carried the subject's contact details)",
		}
	}
	return err
}

// FetchSubjectProfile reads the profile (email and subject ID) of a subject.
func (c *apiClient) FetchSubjectProfile(
	ctx context.Context, subjectID string,
) (profile map[string]any, found bool, err error) {
	return c.fetchOptionalObject(ctx, "/v2/subjects/"+url.PathEscape(subjectID)+"/profile", "subject profile")
}

// FetchSession reads the subject and profile associated with a session ID.
func (c *apiClient) FetchSession(
	ctx context.Context, sessionID string,
) (session map[string]any, found bool, err error) {
	return c.fetchOptionalObject(ctx, "/v2/sessions/"+url.PathEscape(sessionID), "session")
}

// fetchOptionalObject GETs a JSON object with the Unified Consent key, reporting 404 as not found.
func (c *apiClient) fetchOptionalObject(
	ctx context.Context, path, what string,
) (object map[string]any, found bool, err error) {
	body, status, err := c.doJSON(ctx, http.MethodGet, path, nil, nil, headerUnifiedConsent, http.StatusNotFound)
	if err != nil {
		return nil, false, err
	}
	if status == http.StatusNotFound {
		return nil, false, nil
	}

	var data map[string]any
	if err := decodeOptionalJSON(body, &data, what); err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// decodeOptionalJSON decodes body into out unless it is empty.
func decodeOptionalJSON(body []byte, out any, what string) error {
	if strings.TrimSpace(string(body)) == "" {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode %s payload: %w", what, err)
	}
	return nil
}

type sendCodeRequest struct {
	HashedSubjectID string
	Channel         string
	Contact         string
}

// Payload builds the send-code body. Osano documents only email or phone; hashedSubjectId is sent
// only when the caller supplies it.
func (r sendCodeRequest) Payload() map[string]string {
	body := map[string]string{}
	if r.HashedSubjectID != "" {
		body["hashedSubjectId"] = r.HashedSubjectID
	}
	switch r.Channel {
	case "email":
		body["email"] = r.Contact
	case "sms":
		body["phone"] = r.Contact
	}
	return body
}

type verifyRequest struct {
	HashedSubjectID string
	Channel         string
	Contact         string
	Code            string
	Session         string
}

// Payload builds the verify body. SMS verification also requires the session of the SMS challenge.
func (r verifyRequest) Payload() map[string]string {
	body := map[string]string{
		"code": r.Code,
	}
	if r.HashedSubjectID != "" {
		body["hashedSubjectId"] = r.HashedSubjectID
	}
	if r.Session != "" && r.Channel == "sms" {
		body["session"] = r.Session
	}
	switch r.Channel {
	case "email":
		body["email"] = r.Contact
	case "sms":
		body["phone"] = r.Contact
	}
	return body
}

func (c *apiClient) doJSON(
	ctx context.Context,
	method, path string,
	query url.Values,
	payload any,
	key headerKind,
	allowedStatus ...int,
) (respBody []byte, status int, err error) {
	return c.doJSONWithHeaders(ctx, method, path, query, payload, nil, key, allowedStatus...)
}

// doJSONWithHeaders sends one request with the API key headers of kind key plus extra headers, such
// as geolocation overrides. A 2xx response, or one whose status is in allowedStatus, is returned;
// any other status is an error.
func (c *apiClient) doJSONWithHeaders(
	ctx context.Context,
	method, path string,
	query url.Values,
	payload any,
	headers http.Header,
	key headerKind,
	allowedStatus ...int,
) (respBody []byte, status int, err error) {
	if c.err != nil {
		return nil, 0, c.err
	}
	auth, err := c.authHeaders(key)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.client.Do(ctx, method, path, query, payload,
		osanoclient.WithHeaders(auth), osanoclient.WithHeaders(headers), osanoclient.AllowStatus(allowedStatus...))
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, resp.StatusCode, nil
}

// authHeaders returns the API key headers for a request kind, or an error naming the missing key.
func (c *apiClient) authHeaders(key headerKind) (http.Header, error) {
	headers := http.Header{}
	switch key {
	case headerUnifiedConsent:
		if c.settings.unifiedConsentAPIKey == "" {
			return nil, errors.New(
				"Unified Consent API key not configured; set osano:unifiedConsentApiKey or OSANO_UC_API_KEY",
			)
		}
		headers.Set("x-uc-api-key", c.settings.unifiedConsentAPIKey)
	case headerSubject:
		if c.settings.osanoAPIKey == "" && c.settings.unifiedConsentAPIKey == "" {
			return nil, errors.New(
				"no Osano API key configured; set osano:osanoApiKey or OSANO_API_KEY " +
					"(or osano:unifiedConsentApiKey or OSANO_UC_API_KEY)",
			)
		}
		if c.settings.osanoAPIKey != "" {
			headers.Set("x-osano-api-key", c.settings.osanoAPIKey)
		}
		if c.settings.unifiedConsentAPIKey != "" {
			headers.Set("x-uc-api-key", c.settings.unifiedConsentAPIKey)
		}
	}
	return headers, nil
}

type subjectPayload struct {
	ID          string `json:"id"`
	VerifiedID  string `json:"verifiedId"`
	AnonymousID string `json:"anonymousId"`
}

type collectionsPayload struct {
	Jurisdictions []string       `json:"jurisdictions"`
	Collection    map[string]any `json:"collection"`
}
