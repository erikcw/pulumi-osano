package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"

	osanoclient "github.com/jflavan/pulumi-osano/provider/internal/osano"
)

const cookieConsentRulesPath = "/v1/cookie-consent/rules"

// ruleStoreTypes maps each storeType the provider accepts (the key of a rule create body) to the
// singular type Osano reports for a rule and accepts as a list filter.
var ruleStoreTypes = []struct{ provider, api string }{
	{"cookies", "cookie"},
	{"scripts", "script"},
	{"iframes", "iframe"},
	{"localStorage", "localStorage"},
}

var (
	ruleClassifications = []string{"ANALYTICS", "BLACKLISTED", "ESSENTIAL", "HIDDEN", "MARKETING", "PERSONALIZATION"}
	ruleMatchTypes      = []string{
		"FILENAME", "DOMAIN", "PATH", "REGEXP", "STARTS_WITH", "ENDS_WITH", "CONTAINS", "EXACT_MATCH",
	}
)

// storeTypeValues lists the storeType values in their documented order.
func storeTypeValues() []string {
	values := make([]string, 0, len(ruleStoreTypes))
	for _, storeType := range ruleStoreTypes {
		values = append(values, storeType.provider)
	}
	return values
}

// ruleAPIType maps a provider storeType (cookies, scripts, ...) to the singular type the list
// endpoints filter on and rules report.
func ruleAPIType(storeType string) (string, error) {
	for _, candidate := range ruleStoreTypes {
		if candidate.provider == storeType {
			return candidate.api, nil
		}
	}
	return "", fmt.Errorf("storeType must be one of: %s; got %q", strings.Join(storeTypeValues(), ", "), storeType)
}

// ruleStoreType maps the type Osano reports for a rule back to the provider's storeType.
func ruleStoreType(responseType string) (string, error) {
	for _, candidate := range ruleStoreTypes {
		if candidate.api == responseType {
			return candidate.provider, nil
		}
	}
	return "", fmt.Errorf("unsupported Cookie Consent rule type %q", responseType)
}

// CookieConsentRule manages a Cookie Consent rule within an Osano CMP configuration.
type CookieConsentRule struct{}

// CookieConsentRuleArgs are the user inputs for a CMP rule.
type CookieConsentRuleArgs struct {
	ConfigID       string  `pulumi:"configId"`
	StoreType      string  `pulumi:"storeType"`
	Classification string  `pulumi:"classification"`
	Rule           string  `pulumi:"rule"`
	Disclosure     bool    `pulumi:"disclosure,optional"`
	Title          *string `pulumi:"title,optional"`
	VendorName     *string `pulumi:"vendorName,optional"`
	RuleType       *string `pulumi:"ruleType,optional"`
	Description    *string `pulumi:"description,optional"`
	Expiry         *string `pulumi:"expiry,optional"`
}

// CookieConsentRuleState extends the args with server-managed metadata.
type CookieConsentRuleState struct {
	CookieConsentRuleArgs

	RuleID  int    `pulumi:"ruleId"`
	Created string `pulumi:"created,optional"`
	Updated string `pulumi:"updated,optional"`
}

// Annotate documents the CookieConsentRule resource.
func (r *CookieConsentRule) Annotate(a infer.Annotator) {
	a.Describe(
		r,
		"Manages an Osano Cookie Consent (CMP) rule within a configuration. Import with `<configId>/<ruleId>`. "+
			"Changing configId or storeType replaces the rule, and deleting this resource deletes the rule in Osano. "+
			"An optional field the program never sets stays unmanaged: Osano's value is neither read into state "+
			"nor cleared by an update; removing a field the program did set clears it in Osano.",
	)
}

// Annotate documents the CookieConsentRule input fields.
func (args *CookieConsentRuleArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.ConfigID, "The configId of the Cookie Consent Configuration this rule belongs to.")
	a.Describe(&args.StoreType, "The storage type category: "+strings.Join(storeTypeValues(), ", ")+".")
	a.Describe(&args.Classification, "Classification: "+strings.Join(ruleClassifications, ", ")+".")
	a.Describe(&args.Rule, "The rule pattern (e.g. a cookie name pattern). Min 3, max 1000 characters.")
	a.Describe(&args.Disclosure, "Whether the rule should be disclosed. Defaults to false.")
	a.Describe(&args.Title, "Optional title for the rule, used in consent disclosure. Max 64 characters.")
	a.Describe(&args.VendorName, "Optional vendor name for the rule. Max 100 characters.")
	a.Describe(&args.RuleType, "Optional matching mode: "+strings.Join(ruleMatchTypes, ", ")+".")
	a.Describe(&args.Description, "Optional cookie description. Only supported for cookies; max 1000 characters.")
	a.Describe(&args.Expiry, "Optional cookie expiry description. Only supported for cookies; max 50 characters.")
}

// Annotate documents the CookieConsentRule state fields.
func (state *CookieConsentRuleState) Annotate(a infer.Annotator) {
	a.Describe(&state.RuleID, "The server-assigned integer rule ID.")
	a.Describe(&state.Created, "Timestamp when the rule was created.")
	a.Describe(&state.Updated, "Timestamp when the rule was last updated.")
}

type cmpRuleResponse struct {
	Classification string  `json:"classification"`
	Rule           string  `json:"rule"`
	Disclosure     bool    `json:"disclosure"`
	Title          *string `json:"title"`
	VendorName     *string `json:"vendorName"`
	RuleType       *string `json:"ruleType"`
	Description    *string `json:"description"`
	Expiry         *string `json:"expiry"`

	Type     string `json:"type"`
	RuleID   int    `json:"ruleId"`
	ConfigID string `json:"configId"`
	VendorID string `json:"vendorId"`
	Created  string `json:"created"`
	Updated  string `json:"updated"`
}

type cmpRulesListResponse struct {
	Items []cmpRuleResponse `json:"items"`
	Next  string            `json:"next"`
}

// Check validates CookieConsentRule inputs before create or update.
func (r *CookieConsentRule) Check(
	ctx context.Context, req infer.CheckRequest,
) (infer.CheckResponse[CookieConsentRuleArgs], error) {
	args, failures, err := infer.DefaultCheck[CookieConsentRuleArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[CookieConsentRuleArgs]{Inputs: args, Failures: failures}, err
	}

	propertyKnown := func(name string) bool {
		return !req.NewInputs.Get(name).HasComputed()
	}
	fail := func(property, reason string) {
		failures = append(failures, p.CheckFailure{Property: property, Reason: reason})
	}
	storeTypeKnown := propertyKnown("storeType")

	if propertyKnown("configId") && args.ConfigID == "" {
		fail("configId", "configId is required")
	}
	switch {
	case !storeTypeKnown:
	case args.StoreType == "":
		fail("storeType", "storeType is required")
	case !slices.Contains(storeTypeValues(), args.StoreType):
		fail("storeType", "storeType must be one of: "+strings.Join(storeTypeValues(), ", "))
	}
	switch {
	case !propertyKnown("classification"):
	case args.Classification == "":
		fail("classification", "classification is required")
	case !slices.Contains(ruleClassifications, args.Classification):
		fail("classification", "classification must be one of: "+strings.Join(ruleClassifications, ", "))
	}
	switch {
	case !propertyKnown("rule"):
	case args.Rule == "":
		fail("rule", "rule is required")
	case utf8.RuneCountInString(args.Rule) < 3:
		fail("rule", "rule must be at least 3 characters")
	case utf8.RuneCountInString(args.Rule) > 1000:
		fail("rule", "rule must be at most 1000 characters")
	}
	if propertyKnown("title") && args.Title != nil && utf8.RuneCountInString(*args.Title) > 64 {
		fail("title", "title must be at most 64 characters")
	}
	if propertyKnown("vendorName") && args.VendorName != nil && utf8.RuneCountInString(*args.VendorName) > 100 {
		fail("vendorName", "vendorName must be at most 100 characters")
	}
	if propertyKnown("ruleType") && args.RuleType != nil && !slices.Contains(ruleMatchTypes, *args.RuleType) {
		fail("ruleType", "ruleType must be one of: "+strings.Join(ruleMatchTypes, ", "))
	}
	if propertyKnown("description") && args.Description != nil {
		if utf8.RuneCountInString(*args.Description) > 1000 {
			fail("description", "description must be at most 1000 characters")
		}
		if storeTypeKnown && args.StoreType != "cookies" {
			fail("description", "description is only supported for cookies")
		}
	}
	if propertyKnown("expiry") && args.Expiry != nil {
		if utf8.RuneCountInString(*args.Expiry) > 50 {
			fail("expiry", "expiry must be at most 50 characters")
		}
		if storeTypeKnown && args.StoreType != "cookies" {
			fail("expiry", "expiry is only supported for cookies")
		}
	}

	return infer.CheckResponse[CookieConsentRuleArgs]{Inputs: args, Failures: failures}, nil
}

// Create provisions a CookieConsentRule via the Customer REST API.
func (r *CookieConsentRule) Create(
	ctx context.Context, req infer.CreateRequest[CookieConsentRuleArgs],
) (infer.CreateResponse[CookieConsentRuleState], error) {
	if req.DryRun {
		// The inputs are known, so a preview shows them; the server-assigned ID and timestamps stay unknown.
		return infer.CreateResponse[CookieConsentRuleState]{
			Output: CookieConsentRuleState{CookieConsentRuleArgs: req.Inputs},
		}, nil
	}

	client, err := customerClient(ctx)
	if err != nil {
		return infer.CreateResponse[CookieConsentRuleState]{}, err
	}

	body := map[string]any{
		"configIds":          []string{req.Inputs.ConfigID},
		req.Inputs.StoreType: []any{cookieConsentRulePayload(req.Inputs, CookieConsentRuleArgs{})},
	}

	var out cmpRulesListResponse
	if err := client.DoJSON(ctx, http.MethodPost, cookieConsentRulesPath, nil, body, &out); err != nil {
		return infer.CreateResponse[CookieConsentRuleState]{}, fmt.Errorf("create rule: %w", err)
	}
	if len(out.Items) == 0 {
		return infer.CreateResponse[CookieConsentRuleState]{}, errors.New("create rule: API returned empty items list")
	}

	created := out.Items[0]
	state := cookieConsentRuleState(req.Inputs, created)
	return infer.CreateResponse[CookieConsentRuleState]{
		ID:     canonicalRuleID(state.ConfigID, created.RuleID),
		Output: state,
	}, nil
}

// Read refreshes the tracked CookieConsentRule from the Customer REST API.
func (r *CookieConsentRule) Read(
	ctx context.Context, req infer.ReadRequest[CookieConsentRuleArgs, CookieConsentRuleState],
) (infer.ReadResponse[CookieConsentRuleArgs, CookieConsentRuleState], error) {
	client, err := customerClient(ctx)
	if err != nil {
		return infer.ReadResponse[CookieConsentRuleArgs, CookieConsentRuleState]{}, err
	}

	configID, ruleID, err := parseRuleResourceID(req.ID)
	if err != nil {
		return infer.ReadResponse[CookieConsentRuleArgs, CookieConsentRuleState]{}, err
	}

	// A refresh knows the store type and lists only that type; an import (empty inputs) scans every type.
	item, found, err := findCookieConsentRule(ctx, client, configID, ruleID, req.Inputs.StoreType)
	if osanoclient.IsHTTPStatus(err, http.StatusNotFound) {
		return infer.ReadResponse[CookieConsentRuleArgs, CookieConsentRuleState]{ID: ""}, nil
	}
	if err != nil {
		return infer.ReadResponse[CookieConsentRuleArgs, CookieConsentRuleState]{}, fmt.Errorf("read rule: %w", err)
	}
	if !found {
		return infer.ReadResponse[CookieConsentRuleArgs, CookieConsentRuleState]{ID: ""}, nil
	}

	args, err := cookieConsentRuleArgsFromResponse(item, configID, req.Inputs)
	if err != nil {
		return infer.ReadResponse[CookieConsentRuleArgs, CookieConsentRuleState]{}, fmt.Errorf("read rule response: %w", err)
	}
	state := cookieConsentRuleState(args, item)
	return infer.ReadResponse[CookieConsentRuleArgs, CookieConsentRuleState]{
		ID:     canonicalRuleID(state.ConfigID, state.RuleID),
		Inputs: state.CookieConsentRuleArgs,
		State:  state,
	}, nil
}

// Update applies mutable CookieConsentRule changes in place.
func (r *CookieConsentRule) Update(
	ctx context.Context, req infer.UpdateRequest[CookieConsentRuleArgs, CookieConsentRuleState],
) (infer.UpdateResponse[CookieConsentRuleState], error) {
	if req.DryRun {
		preview := req.State
		preview.CookieConsentRuleArgs = req.Inputs
		return infer.UpdateResponse[CookieConsentRuleState]{Output: preview}, nil
	}

	client, err := customerClient(ctx)
	if err != nil {
		return infer.UpdateResponse[CookieConsentRuleState]{}, err
	}

	_, ruleID, err := parseRuleResourceID(req.ID)
	if err != nil {
		return infer.UpdateResponse[CookieConsentRuleState]{}, err
	}

	var out cmpRuleResponse
	if err := client.DoJSON(
		ctx,
		http.MethodPatch,
		cookieConsentRulePath(ruleID),
		nil,
		cookieConsentRulePayload(req.Inputs, req.State.CookieConsentRuleArgs),
		&out,
	); err != nil {
		return infer.UpdateResponse[CookieConsentRuleState]{}, fmt.Errorf("update rule: %w", err)
	}
	if out.RuleID == 0 {
		// An empty PATCH response carries no rule, so keep the applied inputs and prior metadata.
		state := req.State
		state.CookieConsentRuleArgs = req.Inputs
		state.RuleID = ruleID
		return infer.UpdateResponse[CookieConsentRuleState]{Output: state}, nil
	}

	return infer.UpdateResponse[CookieConsentRuleState]{Output: cookieConsentRuleState(req.Inputs, out)}, nil
}

// WireDependencies keeps ruleId known during update previews; it only changes on replacement.
func (r *CookieConsentRule) WireDependencies(
	f infer.FieldSelector, args *CookieConsentRuleArgs, state *CookieConsentRuleState,
) {
	inputs := f.InputField(args).Computed()
	for _, output := range []any{
		&state.ConfigID, &state.StoreType, &state.Classification, &state.Rule, &state.Disclosure,
		&state.Title, &state.VendorName, &state.RuleType, &state.Description, &state.Expiry,
		&state.Created, &state.Updated,
	} {
		f.OutputField(output).DependsOn(inputs)
	}
}

// Delete removes the tracked CookieConsentRule from the Customer REST API.
func (r *CookieConsentRule) Delete(
	ctx context.Context, req infer.DeleteRequest[CookieConsentRuleState],
) (infer.DeleteResponse, error) {
	client, err := customerClient(ctx)
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	_, ruleID, err := parseRuleResourceID(req.ID)
	if err != nil {
		return infer.DeleteResponse{}, err
	}

	err = client.DoJSON(ctx, http.MethodDelete, cookieConsentRulePath(ruleID), nil, nil, nil)
	if osanoclient.IsHTTPStatus(err, http.StatusNotFound) {
		return infer.DeleteResponse{}, nil
	}
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("delete rule: %w", err)
	}

	return infer.DeleteResponse{}, nil
}

// Diff reports in-place updates versus replacements for CookieConsentRule fields.
func (r *CookieConsentRule) Diff(
	_ context.Context, req infer.DiffRequest[CookieConsentRuleArgs, CookieConsentRuleState],
) (infer.DiffResponse, error) {
	diff := map[string]p.PropertyDiff{}

	if req.Inputs.ConfigID != req.State.ConfigID {
		diff["configId"] = p.PropertyDiff{Kind: p.UpdateReplace}
	}
	if req.Inputs.StoreType != req.State.StoreType {
		diff["storeType"] = p.PropertyDiff{Kind: p.UpdateReplace}
	}
	if req.Inputs.Classification != req.State.Classification {
		diff["classification"] = p.PropertyDiff{Kind: p.Update}
	}
	if req.Inputs.Rule != req.State.Rule {
		diff["rule"] = p.PropertyDiff{Kind: p.Update}
	}
	if req.Inputs.Disclosure != req.State.Disclosure {
		diff["disclosure"] = p.PropertyDiff{Kind: p.Update}
	}
	for key, values := range map[string][2]*string{
		"title":       {req.Inputs.Title, req.State.Title},
		"vendorName":  {req.Inputs.VendorName, req.State.VendorName},
		"ruleType":    {req.Inputs.RuleType, req.State.RuleType},
		"description": {req.Inputs.Description, req.State.Description},
		"expiry":      {req.Inputs.Expiry, req.State.Expiry},
	} {
		if !ptrStringEqual(values[0], values[1]) {
			diff[key] = p.PropertyDiff{Kind: p.Update}
		}
	}

	return infer.DiffResponse{
		HasChanges:   len(diff) > 0,
		DetailedDiff: diff,
	}, nil
}

// cookieConsentRuleState combines the inputs Pulumi manages with Osano's server-side metadata.
// Inputs are stored as applied rather than as echoed, so a server default for an optional field the
// program leaves unset does not diff (and PATCH an explicit null) on every subsequent `pulumi up`.
// Reading the response type is unnecessary here, so an unexpected type never orphans a rule that
// the POST already created.
func cookieConsentRuleState(args CookieConsentRuleArgs, resp cmpRuleResponse) CookieConsentRuleState {
	return CookieConsentRuleState{
		CookieConsentRuleArgs: args,
		RuleID:                resp.RuleID,
		Created:               resp.Created,
		Updated:               resp.Updated,
	}
}

// cookieConsentRuleArgsFromResponse derives refreshed inputs from a rule read. An import (no
// declared inputs) adopts everything Osano reports, including the store type mapped from the
// response type. A refresh keeps the declared identity, adopts the required fields so drift is
// visible, and adopts optional fields only where the program declares them; unset optional fields
// stay unmanaged, as do description and expiry on rules other than cookies.
func cookieConsentRuleArgsFromResponse(
	resp cmpRuleResponse, configID string, declared CookieConsentRuleArgs,
) (CookieConsentRuleArgs, error) {
	if resp.ConfigID != "" {
		configID = resp.ConfigID
	}
	args := CookieConsentRuleArgs{
		ConfigID:       configID,
		StoreType:      declared.StoreType,
		Classification: resp.Classification,
		Rule:           resp.Rule,
		Disclosure:     resp.Disclosure,
	}
	imported := declared.Rule == ""
	if resp.Type != "" && (imported || args.StoreType == "") {
		storeType, err := ruleStoreType(resp.Type)
		if err != nil {
			return CookieConsentRuleArgs{}, err
		}
		args.StoreType = storeType
	}
	adopt := func(declaredValue, serverValue *string) *string {
		if imported || declaredValue != nil {
			return serverValue
		}
		return nil
	}
	args.Title = adopt(declared.Title, resp.Title)
	args.VendorName = adopt(declared.VendorName, resp.VendorName)
	args.RuleType = adopt(declared.RuleType, resp.RuleType)
	// description and expiry apply only to cookies: the payload never sends them for other store
	// types and Check rejects them there. Osano still reports description: "" on some script rules,
	// so adopting it would import a value the program cannot declare.
	if args.StoreType == "cookies" {
		args.Description = adopt(declared.Description, resp.Description)
		args.Expiry = adopt(declared.Expiry, resp.Expiry)
	}
	return args, nil
}

// cookieConsentRulePayload builds the create or update request body. An optional field is sent as
// JSON null only to clear a value the program previously managed (previous is the prior state's
// inputs, or zero on create). A field the program never declared is omitted, so Osano's default or
// a value set in the dashboard stays unmanaged, as Create and Read already treat it.
func cookieConsentRulePayload(args, previous CookieConsentRuleArgs) map[string]any {
	payload := map[string]any{
		"classification": args.Classification,
		"rule":           args.Rule,
		"disclosure":     args.Disclosure,
	}
	putNullable := func(key string, value, previousValue *string) {
		if value != nil {
			payload[key] = *value
		} else if previousValue != nil {
			payload[key] = nil
		}
	}
	putNullable("title", args.Title, previous.Title)
	putNullable("vendorName", args.VendorName, previous.VendorName)
	putNullable("ruleType", args.RuleType, previous.RuleType)
	if args.StoreType == "cookies" {
		putNullable("description", args.Description, previous.Description)
		putNullable("expiry", args.Expiry, previous.Expiry)
	}
	return payload
}

func cookieConsentRulePath(ruleID int) string {
	return cookieConsentRulesPath + "/" + strconv.Itoa(ruleID)
}

func canonicalRuleID(configID string, ruleID int) string {
	return fmt.Sprintf("%s/%d", configID, ruleID)
}

// parseRuleResourceID splits the composite `<configId>/<ruleId>` resource ID.
func parseRuleResourceID(id string) (configID string, ruleID int, err error) {
	separator := strings.LastIndex(id, "/")
	if separator < 0 {
		return "", 0, fmt.Errorf("invalid Cookie Consent rule ID %q: expected <configId>/<ruleId>", id)
	}
	configID, ruleIDText := id[:separator], id[separator+1:]
	if configID == "" {
		return "", 0, fmt.Errorf("invalid Cookie Consent rule ID %q: config ID is required", id)
	}
	ruleID, err = strconv.Atoi(ruleIDText)
	if err != nil || ruleIDText == "" {
		return "", 0, fmt.Errorf("invalid Cookie Consent rule ID %q: rule ID must be an integer", id)
	}
	return configID, ruleID, nil
}

// findCookieConsentRule looks a rule up in its configuration's rule list, the only lookup Osano
// offers. storeType, when known, narrows the listing to that type.
func findCookieConsentRule(
	ctx context.Context, client jsonClient, configID string, ruleID int, storeType string,
) (cmpRuleResponse, bool, error) {
	query := url.Values{"limit": []string{strconv.Itoa(cookieConsentRulesPageSize)}}
	if storeType != "" {
		apiType, err := ruleAPIType(storeType)
		if err != nil {
			return cmpRuleResponse{}, false, err
		}
		query.Set("type", apiType)
	}
	var found cmpRuleResponse
	matched := false
	err := paginateCustomerList(ctx, client, cookieConsentConfigPath(configID)+"/rules", query,
		func(page []cmpRuleResponse) bool {
			for idx := range page {
				if page[idx].RuleID == ruleID {
					found, matched = page[idx], true
					return false
				}
			}
			return true
		})
	if err != nil {
		return cmpRuleResponse{}, false, err
	}
	return found, matched, nil
}

func ptrStringEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
