package provider

import (
	"context"
	"errors"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
)

const (
	referenceTypeDescription = "Reference type: subject (default) for a subject's verified or anonymous ID, " +
		"or session for a session ID. anonymous is accepted as a deprecated alias of subject."
	countryCodeOverrideDescription = "Optional ISO 3166-1 country code Osano uses instead of resolving the " +
		"caller's IP address, which in a pipeline is the CI runner's."
	regionCodeOverrideDescription = "Optional ISO 3166-2 region code Osano uses instead of resolving the " +
		"caller's IP address."
)

func geoOverrideFrom(country, region *string) (geoOverride, error) {
	if err := validateGeoOverride(country, region); err != nil {
		return geoOverride{}, err
	}
	var geo geoOverride
	if country != nil {
		geo.CountryCode = *country
	}
	if region != nil {
		geo.RegionCode = *region
	}
	return geo, nil
}

// GetUnifiedConsent exposes an invoke to fetch the latest unified consent summary for a subject.
type GetUnifiedConsent struct{}

// GetUnifiedConsentArgs captures the lookup parameters for the invoke.
type GetUnifiedConsentArgs struct {
	SubjectRef          string  `pulumi:"subjectRef" provider:"secret"`
	ReferenceType       string  `pulumi:"referenceType,optional"`
	CountryCodeOverride *string `pulumi:"countryCodeOverride,optional"`
	RegionCodeOverride  *string `pulumi:"regionCodeOverride,optional"`
}

// Annotate documents the getUnifiedConsent input fields.
func (args *GetUnifiedConsentArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.SubjectRef, "The subject reference to look up: an anonymous ID, verified ID, or session ID.")
	a.Describe(&args.ReferenceType, referenceTypeDescription)
	a.Describe(&args.CountryCodeOverride, countryCodeOverrideDescription)
	a.Describe(&args.RegionCodeOverride, regionCodeOverrideDescription)
}

// GetUnifiedConsentResult is returned to Pulumi programs. The subject reference and the consent
// payload (which carries the subject ID and the ipAddress and userAgent attributes Osano records)
// are personal data, so they are secrets.
type GetUnifiedConsentResult struct {
	SubjectRef     string           `pulumi:"subjectRef" provider:"secret"`
	Exists         bool             `pulumi:"exists"`
	UnifiedConsent map[string]any   `pulumi:"unifiedConsent" provider:"secret"`
	Conflicts      []map[string]any `pulumi:"conflicts" provider:"secret"`
}

// Annotate documents the getUnifiedConsent outputs.
func (r *GetUnifiedConsentResult) Annotate(a infer.Annotator) {
	a.Describe(&r.SubjectRef, "The subject reference that was looked up. Secret, because it identifies a person.")
	a.Describe(&r.Exists, "Whether Osano has any consent for the subject.")
	a.Describe(
		&r.UnifiedConsent,
		"The merged consent: subjectId, brandId, channelIds, jurisdiction, lastUpdateDate, "+
			"lastConflictDate, actions, attributes, compliance, and tags. Secret, because the attributes "+
			"hold the subject's IP address and user agent.",
	)
	a.Describe(
		&r.Conflicts,
		"Conflicting consents Osano resolved, with the resolution and the actions in conflict. Secret, "+
			"because it is the consent history of a person.",
	)
}

// Annotate registers the getUnifiedConsent invoke schema metadata.
func (g *GetUnifiedConsent) Annotate(a infer.Annotator) {
	a.SetToken("index", "getUnifiedConsent")
	a.Describe(g, "Fetches the unified consent state for a subject reference using the Unified Consent API key.")
}

// Invoke fetches unified consent state for the requested subject reference.
func (g *GetUnifiedConsent) Invoke(
	ctx context.Context,
	req infer.FunctionRequest[GetUnifiedConsentArgs],
) (infer.FunctionResponse[GetUnifiedConsentResult], error) {
	subjectRef := strings.TrimSpace(req.Input.SubjectRef)
	if subjectRef == "" {
		return infer.FunctionResponse[GetUnifiedConsentResult]{}, errors.New("subjectRef is required")
	}
	warnDeprecatedReferenceType(ctx, req.Input.ReferenceType)

	geo, err := geoOverrideFrom(req.Input.CountryCodeOverride, req.Input.RegionCodeOverride)
	if err != nil {
		return infer.FunctionResponse[GetUnifiedConsentResult]{}, err
	}
	client := newAPIClient(ctx)
	payload, found, err := client.FetchUnifiedConsent(ctx, subjectRef, req.Input.ReferenceType, geo)
	if err != nil {
		return infer.FunctionResponse[GetUnifiedConsentResult]{}, err
	}
	if !found {
		return infer.FunctionResponse[GetUnifiedConsentResult]{
			Output: GetUnifiedConsentResult{
				SubjectRef: subjectRef,
				Exists:     false,
			},
		}, nil
	}

	result := GetUnifiedConsentResult{
		SubjectRef: subjectRef,
		Exists:     true,
		Conflicts:  payload.Conflicts,
	}

	if uc := payload.UnifiedConsent; uc != nil {
		result.UnifiedConsent = map[string]any{
			"subjectId":        uc.SubjectID,
			"brandId":          uc.BrandID,
			"channelIds":       stringsToAny(uc.ChannelIDs),
			"jurisdiction":     uc.Jurisdiction,
			"lastUpdateDate":   uc.LastUpdate,
			"lastConflictDate": uc.LastConflictDate,
			"actions":          convertActionMaps(uc.Actions),
			"attributes":       uc.Attributes,
			"compliance":       uc.Compliance,
			"tags":             stringsToAny(uc.Tags),
		}
	}

	return infer.FunctionResponse[GetUnifiedConsentResult]{Output: result}, nil
}

// stringsToAny converts a string list for a map output, keeping an empty list empty rather than null.
func stringsToAny(values []string) []any {
	if values == nil {
		return nil
	}
	converted := make([]any, 0, len(values))
	for _, value := range values {
		converted = append(converted, value)
	}
	return converted
}

func convertActionMaps(actions []unifiedConsentAction) []map[string]any {
	if len(actions) == 0 {
		return nil
	}
	converted := make([]map[string]any, 0, len(actions))
	for _, action := range actions {
		converted = append(converted, map[string]any{
			"target":       action.Target,
			"vendor":       action.Vendor,
			"action":       action.Action,
			"jurisdiction": action.Jurisdiction,
		})
	}
	return converted
}

// GetSubject exposes subject lookup for unified consent references.
type GetSubject struct{}

// GetSubjectArgs captures the subject lookup parameters.
type GetSubjectArgs struct {
	SubjectRef    string `pulumi:"subjectRef" provider:"secret"`
	ReferenceType string `pulumi:"referenceType,optional"`
}

// Annotate documents the getSubject input fields.
func (args *GetSubjectArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.SubjectRef, "The subject reference to resolve: an anonymous ID, verified ID, or session ID.")
	a.Describe(&args.ReferenceType, referenceTypeDescription)
}

// GetSubjectResult provides the ID pairings for a subject reference. Every identifier is a secret,
// because each one identifies a person.
type GetSubjectResult struct {
	SubjectRef  string `pulumi:"subjectRef" provider:"secret"`
	SubjectID   string `pulumi:"subjectId" provider:"secret"`
	VerifiedID  string `pulumi:"verifiedId" provider:"secret"`
	AnonymousID string `pulumi:"anonymousId" provider:"secret"`
	Exists      bool   `pulumi:"exists"`
}

// Annotate documents the getSubject outputs.
func (r *GetSubjectResult) Annotate(a infer.Annotator) {
	a.Describe(&r.SubjectRef, "The subject reference that was resolved. Secret, because it identifies a person.")
	a.Describe(&r.SubjectID, "The subject's Osano ID. Secret, because it identifies a person.")
	a.Describe(
		&r.VerifiedID,
		"The subject's verified ID, if the subject is verified. Secret, because it identifies a person.",
	)
	a.Describe(&r.AnonymousID, "The subject's anonymous ID, if any. Secret, because it identifies a person.")
	a.Describe(&r.Exists, "Whether Osano knows the subject. The ID outputs are empty when false.")
}

// Annotate registers the getSubject invoke schema metadata.
func (g *GetSubject) Annotate(a infer.Annotator) {
	a.SetToken("index", "getSubject")
	a.Describe(g, "Fetches subject identifiers (subject, verified, anonymous IDs) for a reference or session ID.")
}

// Invoke resolves subject identifiers for the requested subject reference.
func (g *GetSubject) Invoke(
	ctx context.Context,
	req infer.FunctionRequest[GetSubjectArgs],
) (infer.FunctionResponse[GetSubjectResult], error) {
	subjectRef := strings.TrimSpace(req.Input.SubjectRef)
	if subjectRef == "" {
		return infer.FunctionResponse[GetSubjectResult]{}, errors.New("subjectRef is required")
	}
	warnDeprecatedReferenceType(ctx, req.Input.ReferenceType)

	client := newAPIClient(ctx)
	payload, found, err := client.FetchSubject(ctx, subjectRef, req.Input.ReferenceType)
	if err != nil {
		return infer.FunctionResponse[GetSubjectResult]{}, err
	}

	if !found {
		return infer.FunctionResponse[GetSubjectResult]{
			Output: GetSubjectResult{
				SubjectRef: subjectRef,
				Exists:     false,
			},
		}, nil
	}

	return infer.FunctionResponse[GetSubjectResult]{
		Output: GetSubjectResult{
			SubjectRef:  subjectRef,
			SubjectID:   payload.ID,
			VerifiedID:  payload.VerifiedID,
			AnonymousID: payload.AnonymousID,
			Exists:      true,
		},
	}, nil
}

// warnDeprecatedReferenceType tells the user that the anonymous alias is deprecated.
func warnDeprecatedReferenceType(ctx context.Context, referenceType string) {
	if strings.TrimSpace(referenceType) == referenceTypeAnonymous {
		p.GetLogger(ctx).Warningf(
			"referenceType %q is deprecated and is sent as %q: anonymous IDs are subject references. Use %q or omit it.",
			referenceTypeAnonymous, referenceTypeSubject, referenceTypeSubject,
		)
	}
}

// GetConfig returns the UC configuration associated with the Unified Consent API key.
type GetConfig struct{}

// GetConfigArgs holds the arguments for getConfig.
type GetConfigArgs struct{}

// GetConfigResult returns the Unified Consent configuration payload.
type GetConfigResult struct {
	Config map[string]any `pulumi:"config"`
}

// Annotate documents the getConfig outputs.
func (r *GetConfigResult) Annotate(a infer.Annotator) {
	a.Describe(
		&r.Config,
		"The Unified Consent configuration: configId, customerId, name, domains, privacy policy, "+
			"privacyProtocols, frameworks, styling, publication state, and text customizations.",
	)
}

// Annotate registers the getConfig invoke schema metadata.
func (g *GetConfig) Annotate(a infer.Annotator) {
	a.SetToken("index", "getConfig")
	a.Describe(g, "Retrieves the Unified Consent configuration referenced by the API key.")
}

// Invoke retrieves the Unified Consent configuration payload.
func (g *GetConfig) Invoke(
	ctx context.Context,
	_ infer.FunctionRequest[GetConfigArgs],
) (infer.FunctionResponse[GetConfigResult], error) {
	client := newAPIClient(ctx)
	config, err := client.FetchConfig(ctx)
	if err != nil {
		return infer.FunctionResponse[GetConfigResult]{}, err
	}

	return infer.FunctionResponse[GetConfigResult]{
		Output: GetConfigResult{Config: config},
	}, nil
}

// GetCollections lists privacy protocol collections for the configured UC tenant.
type GetCollections struct{}

// GetCollectionsArgs holds optional collection filters.
type GetCollectionsArgs struct {
	Jurisdiction string `pulumi:"jurisdiction,optional"`
	Type         string `pulumi:"type,optional"`
}

// Annotate documents the getCollections input fields.
func (args *GetCollectionsArgs) Annotate(a infer.Annotator) {
	a.Describe(
		&args.Jurisdiction,
		"Optional jurisdiction filter. When unset, Osano resolves the jurisdiction from the caller's IP address.",
	)
	a.Describe(&args.Type, "Optional collection type: published (default) or draft.")
}

// GetCollectionsResult returns the collection aggregate payload.
type GetCollectionsResult struct {
	Jurisdictions []string       `pulumi:"jurisdictions"`
	Collection    map[string]any `pulumi:"collection"`
}

// Annotate documents the getCollections outputs.
func (r *GetCollectionsResult) Annotate(a infer.Annotator) {
	a.Describe(&r.Jurisdictions, "Every jurisdiction the configuration defines.")
	a.Describe(&r.Collection, "The collection of privacy protocols that applies to the jurisdiction.")
}

// Annotate registers the getCollections invoke schema metadata.
func (g *GetCollections) Annotate(a infer.Annotator) {
	a.SetToken("index", "getCollections")
	a.Describe(
		g,
		"Retrieves the aggregated privacy protocol collections, optionally filtered by jurisdiction and type.",
	)
}

// Invoke lists privacy protocol collections for the configured tenant.
func (g *GetCollections) Invoke(
	ctx context.Context,
	req infer.FunctionRequest[GetCollectionsArgs],
) (infer.FunctionResponse[GetCollectionsResult], error) {
	if collectionType := strings.TrimSpace(req.Input.Type); collectionType != "" {
		if err := oneOf("type", collectionType, []string{"published", "draft"}); err != nil {
			return infer.FunctionResponse[GetCollectionsResult]{}, err
		}
	}
	client := newAPIClient(ctx)
	payload, err := client.FetchCollections(ctx, req.Input.Jurisdiction, req.Input.Type)
	if err != nil {
		return infer.FunctionResponse[GetCollectionsResult]{}, err
	}

	return infer.FunctionResponse[GetCollectionsResult]{
		Output: GetCollectionsResult{
			Jurisdictions: payload.Jurisdictions,
			Collection:    payload.Collection,
		},
	}, nil
}

// GetCollection fetches a single privacy protocol collection by ID.
type GetCollection struct{}

// GetCollectionArgs identifies the collection to fetch.
type GetCollectionArgs struct {
	CollectionID string `pulumi:"collectionId"`
}

// Annotate documents the getCollection input fields.
func (args *GetCollectionArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.CollectionID, "The privacy protocol collection ID to fetch.")
}

// GetCollectionResult returns a single collection lookup result.
type GetCollectionResult struct {
	CollectionID string         `pulumi:"collectionId"`
	Collection   map[string]any `pulumi:"collection"`
	Exists       bool           `pulumi:"exists"`
}

// Annotate documents the getCollection outputs.
func (r *GetCollectionResult) Annotate(a infer.Annotator) {
	a.Describe(&r.CollectionID, "The collection ID that was looked up.")
	a.Describe(
		&r.Collection,
		"The collection: collectionId, name, frameworks, configIds, jurisdiction, type, consents, and preferences.",
	)
	a.Describe(&r.Exists, "Whether Osano returned the collection.")
}

// Annotate registers the getCollection invoke schema metadata.
func (g *GetCollection) Annotate(a infer.Annotator) {
	a.SetToken("index", "getCollection")
	a.Describe(g, "Retrieves a specific privacy protocol collection by ID.")
}

// Invoke fetches a collection by ID.
func (g *GetCollection) Invoke(
	ctx context.Context,
	req infer.FunctionRequest[GetCollectionArgs],
) (infer.FunctionResponse[GetCollectionResult], error) {
	if strings.TrimSpace(req.Input.CollectionID) == "" {
		return infer.FunctionResponse[GetCollectionResult]{}, errors.New("collectionId is required")
	}

	client := newAPIClient(ctx)
	payload, found, err := client.FetchCollection(ctx, req.Input.CollectionID)
	if err != nil {
		return infer.FunctionResponse[GetCollectionResult]{}, err
	}

	if !found {
		return infer.FunctionResponse[GetCollectionResult]{
			Output: GetCollectionResult{
				CollectionID: req.Input.CollectionID,
				Exists:       false,
			},
		}, nil
	}

	return infer.FunctionResponse[GetCollectionResult]{
		Output: GetCollectionResult{
			CollectionID: req.Input.CollectionID,
			Collection:   payload,
			Exists:       true,
		},
	}, nil
}

// CheckConsent reports whether unified consent exists for a subject ID.
type CheckConsent struct{}

// CheckConsentArgs identifies the subject to inspect.
type CheckConsentArgs struct {
	SubjectID           string  `pulumi:"subjectId" provider:"secret"`
	CountryCodeOverride *string `pulumi:"countryCodeOverride,optional"`
	RegionCodeOverride  *string `pulumi:"regionCodeOverride,optional"`
}

// Annotate documents the checkConsent input fields.
func (args *CheckConsentArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.SubjectID, "The subject ID to check. Secret, because it identifies a person.")
	a.Describe(&args.CountryCodeOverride, countryCodeOverrideDescription)
	a.Describe(&args.RegionCodeOverride, regionCodeOverrideDescription)
}

// CheckConsentResult reports whether consent exists for a subject.
type CheckConsentResult struct {
	SubjectID string `pulumi:"subjectId" provider:"secret"`
	Exists    bool   `pulumi:"exists"`
}

// Annotate documents the checkConsent outputs.
func (r *CheckConsentResult) Annotate(a infer.Annotator) {
	a.Describe(&r.SubjectID, "The subject ID that was checked. Secret, because it identifies a person.")
	a.Describe(&r.Exists, "Whether the subject has given consent in the configuration.")
}

// Annotate registers the checkConsent invoke schema metadata.
func (c *CheckConsent) Annotate(a infer.Annotator) {
	a.SetToken("index", "checkConsent")
	a.Describe(c, "Checks whether a unified consent record exists for a given subject ID.")
}

// Invoke checks whether a consent record exists for the subject.
func (c *CheckConsent) Invoke(
	ctx context.Context,
	req infer.FunctionRequest[CheckConsentArgs],
) (infer.FunctionResponse[CheckConsentResult], error) {
	subjectID := strings.TrimSpace(req.Input.SubjectID)
	if subjectID == "" {
		return infer.FunctionResponse[CheckConsentResult]{}, errors.New("subjectId is required")
	}

	geo, err := geoOverrideFrom(req.Input.CountryCodeOverride, req.Input.RegionCodeOverride)
	if err != nil {
		return infer.FunctionResponse[CheckConsentResult]{}, err
	}
	client := newAPIClient(ctx)
	exists, err := client.CheckConsent(ctx, subjectID, geo)
	if err != nil {
		return infer.FunctionResponse[CheckConsentResult]{}, err
	}

	return infer.FunctionResponse[CheckConsentResult]{
		Output: CheckConsentResult{
			SubjectID: subjectID,
			Exists:    exists,
		},
	}, nil
}

// GetConsentProfile fetches hashed consent profile data.
type GetConsentProfile struct{}

// GetConsentProfileArgs identifies the consent profile to retrieve.
type GetConsentProfileArgs struct {
	HashedSubjectID     string  `pulumi:"hashedSubjectId"`
	ConfigID            string  `pulumi:"configId"`
	CountryCodeOverride *string `pulumi:"countryCodeOverride,optional"`
	RegionCodeOverride  *string `pulumi:"regionCodeOverride,optional"`
}

// Annotate documents the getConsentProfile input fields.
func (args *GetConsentProfileArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.HashedSubjectID, "The hashed subject identifier whose consent profile is returned.")
	a.Describe(&args.ConfigID, "The consent configuration ID the profile belongs to.")
	a.Describe(&args.CountryCodeOverride, countryCodeOverrideDescription)
	a.Describe(&args.RegionCodeOverride, regionCodeOverrideDescription)
}

// GetConsentProfileResult returns a consent profile lookup result.
type GetConsentProfileResult struct {
	HashedSubjectID string         `pulumi:"hashedSubjectId"`
	ConfigID        string         `pulumi:"configId"`
	Profile         map[string]any `pulumi:"profile" provider:"secret"`
	Exists          bool           `pulumi:"exists"`
}

// Annotate documents the getConsentProfile outputs.
func (r *GetConsentProfileResult) Annotate(a infer.Annotator) {
	a.Describe(&r.HashedSubjectID, "The hashed subject identifier that was looked up.")
	a.Describe(&r.ConfigID, "The configuration ID that was looked up.")
	a.Describe(
		&r.Profile,
		"The consent profile Osano returned, with unifiedConsent and conflicts keys. Secret, because it "+
			"holds the subject's consent history, IP address, and user agent.",
	)
	a.Describe(&r.Exists, "Whether Osano returned a consent profile.")
}

// Annotate registers the getConsentProfile invoke schema metadata.
func (g *GetConsentProfile) Annotate(a infer.Annotator) {
	a.SetToken("index", "getConsentProfile")
	a.Describe(g, "Retrieves a consent profile for a hashed subject identifier and config ID.")
}

// Invoke fetches a consent profile for the supplied hashed subject ID and config ID.
func (g *GetConsentProfile) Invoke(
	ctx context.Context,
	req infer.FunctionRequest[GetConsentProfileArgs],
) (infer.FunctionResponse[GetConsentProfileResult], error) {
	hashed := strings.TrimSpace(req.Input.HashedSubjectID)
	configID := strings.TrimSpace(req.Input.ConfigID)
	if hashed == "" {
		return infer.FunctionResponse[GetConsentProfileResult]{}, errors.New("hashedSubjectId is required")
	}
	if configID == "" {
		return infer.FunctionResponse[GetConsentProfileResult]{}, errors.New("configId is required")
	}

	geo, err := geoOverrideFrom(req.Input.CountryCodeOverride, req.Input.RegionCodeOverride)
	if err != nil {
		return infer.FunctionResponse[GetConsentProfileResult]{}, err
	}
	client := newAPIClient(ctx)
	profile, found, err := client.FetchConsentProfile(ctx, hashed, configID, geo)
	if err != nil {
		return infer.FunctionResponse[GetConsentProfileResult]{}, err
	}

	result := GetConsentProfileResult{
		HashedSubjectID: hashed,
		ConfigID:        configID,
		Exists:          found,
	}
	if found {
		result.Profile = profile
	}

	return infer.FunctionResponse[GetConsentProfileResult]{Output: result}, nil
}

// GetSubjectProfile reads the profile of a subject.
type GetSubjectProfile struct{}

// GetSubjectProfileArgs identifies the subject.
type GetSubjectProfileArgs struct {
	SubjectID string `pulumi:"subjectId" provider:"secret"`
}

// Annotate documents the getSubjectProfile inputs.
func (args *GetSubjectProfileArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.SubjectID, "The subject ID whose profile is returned. Secret, because it identifies a person.")
}

// GetSubjectProfileResult is the subject profile.
type GetSubjectProfileResult struct {
	SubjectID string         `pulumi:"subjectId" provider:"secret"`
	Exists    bool           `pulumi:"exists"`
	Email     string         `pulumi:"email" provider:"secret"`
	Profile   map[string]any `pulumi:"profile" provider:"secret"`
}

// Annotate documents the getSubjectProfile outputs.
func (r *GetSubjectProfileResult) Annotate(a infer.Annotator) {
	a.Describe(&r.SubjectID, "The subject ID that was looked up. Secret, because it identifies a person.")
	a.Describe(&r.Exists, "Whether Osano returned a profile for the subject.")
	a.Describe(&r.Email, "The subject's email address. Secret, because it is personal data.")
	a.Describe(&r.Profile, "The complete profile Osano returned. Secret, because it is personal data.")
}

// Annotate registers the getSubjectProfile function.
func (g *GetSubjectProfile) Annotate(a infer.Annotator) {
	a.SetToken("index", "getSubjectProfile")
	a.Describe(
		g,
		"Reads a subject's profile (email and subject ID) using the Unified Consent API key. The outputs "+
			"are secrets because they hold personal data.",
	)
}

// Invoke reads the subject profile.
func (g *GetSubjectProfile) Invoke(
	ctx context.Context, req infer.FunctionRequest[GetSubjectProfileArgs],
) (infer.FunctionResponse[GetSubjectProfileResult], error) {
	subjectID := strings.TrimSpace(req.Input.SubjectID)
	if subjectID == "" {
		return infer.FunctionResponse[GetSubjectProfileResult]{}, errors.New("subjectId is required")
	}
	profile, found, err := newAPIClient(ctx).FetchSubjectProfile(ctx, subjectID)
	if err != nil {
		return infer.FunctionResponse[GetSubjectProfileResult]{}, err
	}
	result := GetSubjectProfileResult{SubjectID: subjectID, Exists: found}
	if found {
		result.Profile = profile
		result.Email, _ = profile["email"].(string)
	}
	return infer.FunctionResponse[GetSubjectProfileResult]{Output: result}, nil
}

// GetSession reads the subject and profile behind a session ID.
type GetSession struct{}

// GetSessionArgs identifies the session.
type GetSessionArgs struct {
	SessionID string `pulumi:"sessionId" provider:"secret"`
}

// Annotate documents the getSession inputs.
func (args *GetSessionArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.SessionID, "The session ID to resolve.")
}

// GetSessionResult is the session's subject and profile.
type GetSessionResult struct {
	Exists     bool           `pulumi:"exists"`
	VerifiedID string         `pulumi:"verifiedId" provider:"secret"`
	Profile    map[string]any `pulumi:"profile" provider:"secret"`
}

// Annotate documents the getSession outputs.
func (r *GetSessionResult) Annotate(a infer.Annotator) {
	a.Describe(&r.Exists, "Whether Osano recognized the session.")
	a.Describe(&r.VerifiedID, "The verified ID of the session's subject. Secret, because it identifies a person.")
	a.Describe(
		&r.Profile,
		"The session's profile (email, firstName, lastName). Secret, because it is personal data.",
	)
}

// Annotate registers the getSession function.
func (g *GetSession) Annotate(a infer.Annotator) {
	a.SetToken("index", "getSession")
	a.Describe(
		g,
		"Resolves a Unified Consent session ID to its subject's verified ID and profile, using the Unified "+
			"Consent API key.",
	)
}

// Invoke reads the session.
func (g *GetSession) Invoke(
	ctx context.Context, req infer.FunctionRequest[GetSessionArgs],
) (infer.FunctionResponse[GetSessionResult], error) {
	sessionID := strings.TrimSpace(req.Input.SessionID)
	if sessionID == "" {
		return infer.FunctionResponse[GetSessionResult]{}, errors.New("sessionId is required")
	}
	session, found, err := newAPIClient(ctx).FetchSession(ctx, sessionID)
	if err != nil {
		return infer.FunctionResponse[GetSessionResult]{}, err
	}
	result := GetSessionResult{Exists: found}
	if found {
		if subject, ok := session["subject"].(map[string]any); ok {
			result.VerifiedID, _ = subject["verifiedId"].(string)
		}
		if profile, ok := session["profile"].(map[string]any); ok {
			result.Profile = profile
		}
	}
	return infer.FunctionResponse[GetSessionResult]{Output: result}, nil
}

// SendSubjectCode starts the verification flow for a subject profile.
type SendSubjectCode struct{}

// SendSubjectCodeArgs identifies the subject and delivery channel for verification. The contact
// details are personal data, so they are secrets, as the destination output already is.
type SendSubjectCodeArgs struct {
	HashedSubjectID string `pulumi:"hashedSubjectId,optional"`
	Email           string `pulumi:"email,optional" provider:"secret"`
	Phone           string `pulumi:"phone,optional" provider:"secret"`
}

// Annotate documents the sendSubjectCode input fields.
func (args *SendSubjectCodeArgs) Annotate(a infer.Annotator) {
	a.Describe(
		&args.HashedSubjectID,
		"Optional hashed subject identifier, sent only when set. Osano's current API identifies the subject "+
			"by email or phone.",
	)
	a.Describe(
		&args.Email,
		"Email address to send the code to. Set exactly one of email or phone. Secret, because it is personal data.",
	)
	a.Describe(
		&args.Phone,
		"Phone number to send the code to by SMS. Set exactly one of email or phone. Secret, because it is "+
			"personal data.",
	)
}

// SendSubjectCodeResult reports where the verification code was sent.
type SendSubjectCodeResult struct {
	HashedSubjectID string `pulumi:"hashedSubjectId"`
	Channel         string `pulumi:"channel"`
	Destination     string `pulumi:"destination" provider:"secret"`
	Session         string `pulumi:"session" provider:"secret"`
}

// Annotate documents the sendSubjectCode outputs.
func (r *SendSubjectCodeResult) Annotate(a infer.Annotator) {
	a.Describe(&r.HashedSubjectID, "The hashed subject identifier sent with the request, if any.")
	a.Describe(&r.Channel, "The delivery channel: email or sms.")
	a.Describe(
		&r.Destination,
		"The email address or phone number the code was sent to. Secret, because it is personal data.",
	)
	a.Describe(
		&r.Session,
		"The SMS challenge session, when Osano returns one; pass it to verifySubjectCode. Empty for email.",
	)
}

// Annotate registers the sendSubjectCode invoke schema metadata.
func (s *SendSubjectCode) Annotate(a infer.Annotator) {
	a.SetToken("index", "sendSubjectCode")
	a.Describe(
		s,
		"Sends a verification code to a subject's email or phone, authenticating with every configured "+
			"key (the Osano API key, the Unified Consent API key, or both). Pulumi runs invokes on every preview, update, and "+
			"refresh, so declaring this in a stack sends a new code each time; call it from automation rather "+
			"than from long-lived stack code.",
	)
}

// Invoke sends a verification code to the requested subject contact.
func (s *SendSubjectCode) Invoke(
	ctx context.Context,
	req infer.FunctionRequest[SendSubjectCodeArgs],
) (infer.FunctionResponse[SendSubjectCodeResult], error) {
	hashed := strings.TrimSpace(req.Input.HashedSubjectID)
	channel, destination, err := resolveVerificationContact(req.Input.Email, req.Input.Phone)
	if err != nil {
		return infer.FunctionResponse[SendSubjectCodeResult]{}, err
	}

	client := newAPIClient(ctx)
	response, err := client.SendVerificationCode(ctx, sendCodeRequest{
		HashedSubjectID: hashed,
		Channel:         channel,
		Contact:         destination,
	})
	if err != nil {
		return infer.FunctionResponse[SendSubjectCodeResult]{}, err
	}

	result := SendSubjectCodeResult{
		HashedSubjectID: hashed,
		Channel:         channel,
		Destination:     destination,
	}
	for _, key := range []string{"session", "sessionId"} {
		if session, ok := response[key].(string); ok && session != "" {
			result.Session = session
			break
		}
	}
	return infer.FunctionResponse[SendSubjectCodeResult]{Output: result}, nil
}

// VerifySubjectCode finalizes a subject verification challenge.
type VerifySubjectCode struct{}

// VerifySubjectCodeArgs captures the contact and code used for verification. The contact details
// are personal data, so they are secrets, as the destination output already is.
type VerifySubjectCodeArgs struct {
	HashedSubjectID string `pulumi:"hashedSubjectId,optional"`
	Code            string `pulumi:"code" provider:"secret"`
	Email           string `pulumi:"email,optional" provider:"secret"`
	Phone           string `pulumi:"phone,optional" provider:"secret"`
	Session         string `pulumi:"session,optional" provider:"secret"`
}

// Annotate documents the verifySubjectCode input fields.
func (args *VerifySubjectCodeArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.HashedSubjectID, "Optional hashed subject identifier, sent only when set.")
	a.Describe(&args.Code, "The one-time verification code the subject received (6 characters by email, 8 by SMS).")
	a.Describe(
		&args.Email,
		"Email address the code was sent to. Set exactly one of email or phone. Secret, because it is personal data.",
	)
	a.Describe(
		&args.Phone,
		"Phone number the code was sent to. Set exactly one of email or phone. Secret, because it is personal data.",
	)
	a.Describe(&args.Session, "The SMS challenge session. Required with phone; not used with email.")
}

// VerifySubjectCodeResult reports whether verification succeeded and returns the profile.
type VerifySubjectCodeResult struct {
	HashedSubjectID string         `pulumi:"hashedSubjectId"`
	Channel         string         `pulumi:"channel"`
	Destination     string         `pulumi:"destination" provider:"secret"`
	Verified        bool           `pulumi:"verified"`
	VerifiedID      string         `pulumi:"verifiedId" provider:"secret"`
	Profile         map[string]any `pulumi:"profile" provider:"secret"`
}

// Annotate documents the verifySubjectCode outputs.
func (r *VerifySubjectCodeResult) Annotate(a infer.Annotator) {
	a.Describe(&r.HashedSubjectID, "The hashed subject identifier sent with the request, if any.")
	a.Describe(&r.Channel, "The verification channel: email or sms.")
	a.Describe(&r.Destination, "The email address or phone number that was verified. Secret, because it is personal data.")
	a.Describe(
		&r.Verified,
		"True when Osano accepted the code. A rejected code fails the invoke, as does a response that "+
			"reports verified: false.",
	)
	a.Describe(&r.VerifiedID, "The subject's verified ID returned by Osano. Secret, because it identifies a person.")
	a.Describe(&r.Profile, "The complete response Osano returned. Secret, because it can hold personal data.")
}

// Annotate registers the verifySubjectCode invoke schema metadata.
func (v *VerifySubjectCode) Annotate(a infer.Annotator) {
	a.SetToken("index", "verifySubjectCode")
	a.Describe(
		v,
		"Verifies a subject profile using the code sent via email or SMS. Pulumi runs invokes on every "+
			"preview, update, and refresh, and one-time codes cannot be reused, so call this from automation "+
			"rather than from long-lived stack code.",
	)
}

// Invoke verifies the subject profile with the supplied code and contact.
func (v *VerifySubjectCode) Invoke(
	ctx context.Context,
	req infer.FunctionRequest[VerifySubjectCodeArgs],
) (infer.FunctionResponse[VerifySubjectCodeResult], error) {
	hashed := strings.TrimSpace(req.Input.HashedSubjectID)
	code := strings.TrimSpace(req.Input.Code)
	if code == "" {
		return infer.FunctionResponse[VerifySubjectCodeResult]{}, errors.New("code is required")
	}
	channel, destination, err := resolveVerificationContact(req.Input.Email, req.Input.Phone)
	if err != nil {
		return infer.FunctionResponse[VerifySubjectCodeResult]{}, err
	}
	session := strings.TrimSpace(req.Input.Session)
	if channel == "sms" && session == "" {
		return infer.FunctionResponse[VerifySubjectCodeResult]{}, errors.New(
			"session is required to verify an SMS code; pass the session from the SMS challenge",
		)
	}

	client := newAPIClient(ctx)
	profile, err := client.VerifySubjectCode(ctx, verifyRequest{
		HashedSubjectID: hashed,
		Channel:         channel,
		Contact:         destination,
		Code:            code,
		Session:         session,
	})
	if err != nil {
		return infer.FunctionResponse[VerifySubjectCodeResult]{}, err
	}

	// Osano documents only {"verifiedId": ...} on success; a body that explicitly reports
	// verified: false must not count as verified even when the status is 2xx.
	if verified, reported := profile["verified"].(bool); reported && !verified {
		return infer.FunctionResponse[VerifySubjectCodeResult]{}, errors.New(
			"Osano did not verify the code: the response reports verified: false",
		)
	}
	result := VerifySubjectCodeResult{
		HashedSubjectID: hashed,
		Channel:         channel,
		Destination:     destination,
		Verified:        true,
	}
	if len(profile) > 0 {
		result.Profile = profile
		result.VerifiedID, _ = profile["verifiedId"].(string)
	}

	return infer.FunctionResponse[VerifySubjectCodeResult]{Output: result}, nil
}

func resolveVerificationContact(email, phone string) (channel, destination string, err error) {
	email = strings.TrimSpace(email)
	phone = strings.TrimSpace(phone)
	if email == "" && phone == "" {
		return "", "", errors.New("either email or phone is required")
	}
	if email != "" && phone != "" {
		return "", "", errors.New("only one of email or phone can be provided")
	}
	if email != "" {
		return "email", email, nil
	}
	return "sms", phone, nil
}
