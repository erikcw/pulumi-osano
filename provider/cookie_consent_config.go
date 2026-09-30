package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"

	osanoclient "github.com/jflavan/pulumi-osano/provider/internal/osano"
)

// CookieConsentConfig manages a Cookie Consent configuration via the Osano Customer REST API.
type CookieConsentConfig struct{}

// CookieConsentConfigArgs captures the desired Cookie Consent configuration.
type CookieConsentConfigArgs struct {
	Name          string         `pulumi:"name"`
	Domains       []string       `pulumi:"domains"`
	Mode          string         `pulumi:"mode"`
	OrgIDs        []string       `pulumi:"orgIds,optional"`
	Configuration map[string]any `pulumi:"configuration"`
}

// CookieConsentConfigState extends the user inputs with Osano-managed metadata.
type CookieConsentConfigState struct {
	CookieConsentConfigArgs

	ConfigID            string `pulumi:"configId"`
	CustomerID          string `pulumi:"customerId,optional"`
	Created             int    `pulumi:"created,optional"`
	Updated             int    `pulumi:"updated,optional"`
	PublishStatus       string `pulumi:"publishStatus,optional"`
	LastPublished       int    `pulumi:"lastPublished,optional"`
	PublishedRevision   int    `pulumi:"publishedRevision,optional"`
	TattleRecordStopped bool   `pulumi:"tattleRecordStopped,optional"`
}

// Annotate documents the CookieConsentConfig resource.
func (r *CookieConsentConfig) Annotate(a infer.Annotator) {
	a.Describe(
		r,
		"Manages an Osano Cookie Consent (CMP) configuration. Import with the Osano config ID. "+
			"Osano has no delete endpoint for configs, so deleting this resource only removes it from "+
			"Pulumi state and retains the upstream configuration.",
	)
}

// Annotate documents the CookieConsentConfig input fields.
func (args *CookieConsentConfigArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.Name, "The name of the configuration.")
	a.Describe(&args.Domains, "Domains permitted to host the configuration.")
	a.Describe(&args.Mode, "Compliance mode: debug, permissive, or production.")
	a.Describe(&args.OrgIDs, "Optional organization IDs associated with the config.")
	a.Describe(
		&args.Configuration,
		"CMP configuration object. At minimum, must include storagePolicyHref.",
	)
}

// Annotate documents the CookieConsentConfig state fields.
func (state *CookieConsentConfigState) Annotate(a infer.Annotator) {
	a.Describe(&state.ConfigID, "The Osano configId (UUID).")
	a.Describe(&state.CustomerID, "The Osano customer ID that owns the configuration.")
	a.Describe(&state.Created, "Unix timestamp when Osano created the configuration.")
	a.Describe(&state.Updated, "Unix timestamp when Osano last updated the configuration.")
	a.Describe(&state.PublishStatus, "Current Osano publication status for the configuration.")
	a.Describe(&state.LastPublished, "Unix timestamp when Osano last published the configuration.")
	a.Describe(&state.PublishedRevision, "Revision number most recently published by Osano.")
	a.Describe(&state.TattleRecordStopped, "Whether Osano stopped recording discoveries (tattles) for the configuration.")
}

type cmpConfigResponse struct {
	Name          string         `json:"name"`
	Domains       []string       `json:"domains"`
	Mode          string         `json:"mode"`
	OrgIDs        []string       `json:"orgIds"`
	Configuration map[string]any `json:"configuration"`

	ConfigID            string `json:"configId"`
	CustomerID          string `json:"customerId"`
	Created             int    `json:"created"`
	Updated             int    `json:"updated"`
	PublishStatus       string `json:"publishStatus"`
	LastPublished       int    `json:"lastPublished"`
	PublishedRevision   int    `json:"publishedRevision"`
	TattleRecordStopped bool   `json:"tattleRecordStopped"`
}

// Check validates CookieConsentConfig inputs before create or update.
func (r *CookieConsentConfig) Check(
	ctx context.Context, req infer.CheckRequest,
) (infer.CheckResponse[CookieConsentConfigArgs], error) {
	args, failures, err := infer.DefaultCheck[CookieConsentConfigArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[CookieConsentConfigArgs]{Inputs: args, Failures: failures}, err
	}

	propertyKnown := func(name string) bool {
		return !req.NewInputs.Get(name).HasComputed()
	}

	if propertyKnown("name") && args.Name == "" {
		failures = append(failures, p.CheckFailure{Property: "name", Reason: "name is required"})
	}
	if propertyKnown("domains") && len(args.Domains) == 0 {
		failures = append(failures, p.CheckFailure{Property: "domains", Reason: "at least one domain is required"})
	}
	switch {
	case !propertyKnown("mode"):
	case args.Mode == "debug", args.Mode == "permissive", args.Mode == "production":
	default:
		failures = append(
			failures,
			p.CheckFailure{
				Property: "mode",
				Reason:   "mode must be one of: debug, permissive, production",
			},
		)
	}
	if !propertyKnown("configuration") {
		return infer.CheckResponse[CookieConsentConfigArgs]{Inputs: args, Failures: failures}, nil
	}
	if args.Configuration == nil {
		failures = append(failures, p.CheckFailure{Property: "configuration", Reason: "configuration is required"})
		return infer.CheckResponse[CookieConsentConfigArgs]{Inputs: args, Failures: failures}, nil
	}
	mode := ""
	if propertyKnown("mode") {
		mode = args.Mode
	}
	creating := req.OldInputs.Get("configuration").IsNull()
	configurationFailures, warnings := validateCookieConsentConfiguration(args.Configuration, mode, creating)
	if !creating && unchangedInputs(req, "configuration") {
		// Osano accepted this configuration when it was applied, so a stricter check in a newer
		// provider version must not block the stack; report the problem until the next change.
		for _, failure := range configurationFailures {
			warnings = append(warnings, failure.Reason+
				" (the configuration is unchanged, so this is not enforced until you next change it)")
		}
	} else {
		failures = append(failures, configurationFailures...)
	}
	logger := p.GetLogger(ctx)
	for _, warning := range warnings {
		logger.Warning(warning)
	}

	return infer.CheckResponse[CookieConsentConfigArgs]{Inputs: args, Failures: failures}, nil
}

// Create provisions a CookieConsentConfig via the Customer REST API.
func (r *CookieConsentConfig) Create(
	ctx context.Context, req infer.CreateRequest[CookieConsentConfigArgs],
) (infer.CreateResponse[CookieConsentConfigState], error) {
	if req.DryRun {
		// The inputs are known, so a preview shows them; the server-assigned metadata stays unknown.
		return infer.CreateResponse[CookieConsentConfigState]{
			Output: CookieConsentConfigState{CookieConsentConfigArgs: req.Inputs},
		}, nil
	}

	client, err := customerClient(ctx)
	if err != nil {
		return infer.CreateResponse[CookieConsentConfigState]{}, err
	}

	body := cookieConsentConfigPayload(req.Inputs, len(req.Inputs.OrgIDs) > 0)

	var out cmpConfigResponse
	requestedAt := time.Now().Unix()
	if err := client.DoJSON(ctx, http.MethodPost, cookieConsentConfigsPath, nil, body, &out); err != nil {
		// Osano cannot delete a configuration, so a create whose answer was lost must not be repeated
		// blindly: when the POST may have been processed, adopt the configuration it created.
		if created, found := adoptLostConfigCreate(ctx, client, req.Inputs, requestedAt, err); found {
			p.GetLogger(ctx).Warningf(
				"the create request for Cookie Consent config %q failed (%v), but Osano reports a matching "+
					"configuration %s created by it; adopting it instead of creating a duplicate",
				req.Inputs.Name, err, created.ConfigID,
			)
			out = created
		} else {
			return infer.CreateResponse[CookieConsentConfigState]{}, err
		}
	}
	if out.ConfigID == "" {
		return infer.CreateResponse[CookieConsentConfigState]{},
			errors.New("create Cookie Consent config: Osano's response has no configId")
	}

	state := cookieConsentConfigState(req.Inputs, out)
	return infer.CreateResponse[CookieConsentConfigState]{ID: out.ConfigID, Output: state}, nil
}

// adoptLostConfigCreate finds the configuration a failed POST may still have created. It applies
// only when the request may have been processed (a lost response or a 5xx), never to a 4xx, and
// adopts a configuration only when exactly one with the requested name and domains was created at
// or after the request.
func adoptLostConfigCreate(
	ctx context.Context, client jsonClient, args CookieConsentConfigArgs, requestedAt int64, cause error,
) (cmpConfigResponse, bool) {
	var httpErr *osanoclient.HTTPError
	if errors.As(cause, &httpErr) && httpErr.StatusCode < 500 {
		return cmpConfigResponse{}, false
	}
	query := url.Values{"name": []string{args.Name}, "limit": []string{strconv.Itoa(cookieConsentConfigsPageSize)}}
	var matches []cmpConfigResponse
	err := paginateCustomerList(ctx, client, cookieConsentConfigsPath, query, func(page []cmpConfigResponse) bool {
		for idx := range page {
			candidate := &page[idx]
			if candidate.ConfigID != "" && candidate.Name == args.Name &&
				stringSlicesEqual(candidate.Domains, args.Domains) && int64(candidate.Created) >= requestedAt {
				matches = append(matches, *candidate)
			}
		}
		return true
	})
	if err != nil || len(matches) != 1 {
		return cmpConfigResponse{}, false
	}
	return matches[0], true
}

// Read refreshes the tracked CookieConsentConfig from the Customer REST API.
func (r *CookieConsentConfig) Read(
	ctx context.Context, req infer.ReadRequest[CookieConsentConfigArgs, CookieConsentConfigState],
) (infer.ReadResponse[CookieConsentConfigArgs, CookieConsentConfigState], error) {
	client, err := customerClient(ctx)
	if err != nil {
		return infer.ReadResponse[CookieConsentConfigArgs, CookieConsentConfigState]{}, err
	}

	var out cmpConfigResponse
	err = client.DoJSON(ctx, http.MethodGet, cookieConsentConfigPath(req.ID), nil, nil, &out)
	if osanoclient.IsHTTPStatus(err, http.StatusNotFound) {
		return infer.ReadResponse[CookieConsentConfigArgs, CookieConsentConfigState]{ID: ""}, nil
	}
	if err != nil {
		return infer.ReadResponse[CookieConsentConfigArgs, CookieConsentConfigState]{},
			fmt.Errorf("read Cookie Consent config %q: %w", req.ID, err)
	}
	if out.ConfigID == "" {
		// Only a 404 means the configuration is gone. An empty ID here is a malformed answer, and
		// reporting it as deleted would make the next update create an undeletable duplicate.
		return infer.ReadResponse[CookieConsentConfigArgs, CookieConsentConfigState]{},
			fmt.Errorf("read Cookie Consent config %q: Osano's response has no configId", req.ID)
	}

	state := cookieConsentConfigState(cookieConsentConfigArgsFromResponse(out, req.Inputs), out)
	return infer.ReadResponse[CookieConsentConfigArgs, CookieConsentConfigState]{
		ID:     out.ConfigID,
		Inputs: state.CookieConsentConfigArgs,
		State:  state,
	}, nil
}

// Update applies mutable CookieConsentConfig changes in place.
func (r *CookieConsentConfig) Update(
	ctx context.Context, req infer.UpdateRequest[CookieConsentConfigArgs, CookieConsentConfigState],
) (infer.UpdateResponse[CookieConsentConfigState], error) {
	if req.DryRun {
		preview := req.State
		preview.CookieConsentConfigArgs = req.Inputs
		return infer.UpdateResponse[CookieConsentConfigState]{Output: preview}, nil
	}

	client, err := customerClient(ctx)
	if err != nil {
		return infer.UpdateResponse[CookieConsentConfigState]{}, err
	}

	// Send orgIds only to set or clear a managed value; Osano treats omitted and empty alike.
	body := cookieConsentConfigPayload(req.Inputs, len(req.Inputs.OrgIDs) > 0 || len(req.State.OrgIDs) > 0)
	// A configuration key the program stopped declaring is cleared with an explicit null. Refresh
	// compares only declared keys, so without this the old value would survive in Osano unseen.
	body["configuration"] = withRemovedKeysNulled(req.Inputs.Configuration, req.State.Configuration)

	var out cmpConfigResponse
	if err := client.DoJSON(
		ctx,
		http.MethodPatch,
		cookieConsentConfigPath(req.ID),
		nil,
		body,
		&out,
	); err != nil {
		return infer.UpdateResponse[CookieConsentConfigState]{}, err
	}
	if out.ConfigID == "" {
		// An empty PATCH response carries no config, so keep the applied inputs and prior metadata.
		state := req.State
		state.CookieConsentConfigArgs = req.Inputs
		return infer.UpdateResponse[CookieConsentConfigState]{Output: state}, nil
	}

	return infer.UpdateResponse[CookieConsentConfigState]{
		Output: cookieConsentConfigState(req.Inputs, out),
	}, nil
}

// WireDependencies keeps configId and customerId known during update previews. They never change
// in place, and marking them unknown would make every dependent rule and publication preview as a
// replacement. Server metadata still depends on every input.
func (r *CookieConsentConfig) WireDependencies(
	f infer.FieldSelector, args *CookieConsentConfigArgs, state *CookieConsentConfigState,
) {
	inputs := f.InputField(args).Computed()
	for _, output := range []any{
		&state.Name, &state.Domains, &state.Mode, &state.OrgIDs, &state.Configuration,
		&state.Created, &state.Updated, &state.PublishStatus, &state.LastPublished,
		&state.PublishedRevision, &state.TattleRecordStopped,
	} {
		f.OutputField(output).DependsOn(inputs)
	}
}

// Delete forgets the local CookieConsentConfig state without removing the upstream config.
func (r *CookieConsentConfig) Delete(
	_ context.Context, _ infer.DeleteRequest[CookieConsentConfigState],
) (infer.DeleteResponse, error) {
	// Osano does not expose a delete endpoint for CMP configs, so destroy only forgets local state.
	return infer.DeleteResponse{}, nil
}

// Diff reports in-place changes for CookieConsentConfig fields.
func (r *CookieConsentConfig) Diff(
	_ context.Context, req infer.DiffRequest[CookieConsentConfigArgs, CookieConsentConfigState],
) (infer.DiffResponse, error) {
	diff := map[string]p.PropertyDiff{}

	if req.Inputs.Name != req.State.Name {
		diff["name"] = p.PropertyDiff{Kind: p.Update}
	}
	if !stringSlicesEqual(req.Inputs.Domains, req.State.Domains) {
		diff["domains"] = p.PropertyDiff{Kind: p.Update}
	}
	if req.Inputs.Mode != req.State.Mode {
		diff["mode"] = p.PropertyDiff{Kind: p.Update}
	}
	if !stringSlicesEqual(req.Inputs.OrgIDs, req.State.OrgIDs) {
		diff["orgIds"] = p.PropertyDiff{Kind: p.Update}
	}

	if !jsonValuesEqual(req.Inputs.Configuration, req.State.Configuration) {
		diff["configuration"] = p.PropertyDiff{Kind: p.Update}
	}

	return infer.DiffResponse{HasChanges: len(diff) > 0, DetailedDiff: diff}, nil
}

const cookieConsentConfigsPath = "/v1/cookie-consent/configs"

func cookieConsentConfigPath(configID string) string {
	return cookieConsentConfigsPath + "/" + url.PathEscape(configID)
}

// stringSlicesEqual treats nil and empty as equal, since Osano may echo an unset list as [].
func stringSlicesEqual(left, right []string) bool {
	if len(left) == 0 && len(right) == 0 {
		return true
	}
	return slices.Equal(left, right)
}

func jsonValuesEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

// cookieConsentConfigPayload builds the create/update request body. Osano documents orgIds as
// optional, with omitted and empty both meaning the root organization, so it is sent only when
// includeOrgIDs is set and then always as a JSON array rather than null.
func cookieConsentConfigPayload(args CookieConsentConfigArgs, includeOrgIDs bool) map[string]any {
	body := map[string]any{
		"name":          args.Name,
		"domains":       args.Domains,
		"mode":          args.Mode,
		"configuration": args.Configuration,
	}
	if includeOrgIDs {
		orgIDs := args.OrgIDs
		if orgIDs == nil {
			orgIDs = []string{}
		}
		body["orgIds"] = orgIDs
	}
	return body
}

// cookieConsentConfigState combines the inputs Pulumi manages with Osano's server-side metadata.
// Inputs are stored as applied rather than as echoed, so a configuration object that Osano
// normalizes (for example by adding default keys) does not diff on every subsequent `pulumi up`.
func cookieConsentConfigState(args CookieConsentConfigArgs, resp cmpConfigResponse) CookieConsentConfigState {
	return CookieConsentConfigState{
		CookieConsentConfigArgs: args,
		ConfigID:                resp.ConfigID,
		CustomerID:              resp.CustomerID,
		Created:                 resp.Created,
		Updated:                 resp.Updated,
		PublishStatus:           resp.PublishStatus,
		LastPublished:           resp.LastPublished,
		PublishedRevision:       resp.PublishedRevision,
		TattleRecordStopped:     resp.TattleRecordStopped,
	}
}

// cookieConsentConfigArgsFromResponse derives refreshed inputs from a config read. An import (no
// declared inputs) adopts everything Osano reports except an empty additionalLinks. A refresh
// adopts the scalar fields so drift is visible, but projects the configuration object onto the
// keys the program declares: server-added defaults stay out of state, and a declared key Osano
// omits keeps its declared value.
func cookieConsentConfigArgsFromResponse(
	resp cmpConfigResponse, declared CookieConsentConfigArgs,
) CookieConsentConfigArgs {
	args := CookieConsentConfigArgs{
		Name:          resp.Name,
		Domains:       resp.Domains,
		Mode:          resp.Mode,
		OrgIDs:        resp.OrgIDs,
		Configuration: resp.Configuration,
	}
	if declared.Configuration != nil {
		args.Configuration = projectConfiguration(resp.Configuration, declared.Configuration)
	} else if links, isList := resp.Configuration["additionalLinks"].([]any); isList && len(links) == 0 {
		// Osano reports [] for a configuration without additional links, but its spec requires at
		// least one link on write, so adopting [] would import a value Check rejects once the
		// configuration next changes. Leaving the key out means the same thing.
		args.Configuration = maps.Clone(resp.Configuration)
		delete(args.Configuration, "additionalLinks")
	}
	return args
}
