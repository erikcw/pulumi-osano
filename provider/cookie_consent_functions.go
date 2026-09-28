package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	osanoclient "github.com/jflavan/pulumi-osano/provider/internal/osano"
)

const (
	// Page sizes are the maximums the Customer REST API documents for each list endpoint.
	cookieConsentConfigsPageSize  = 1000
	cookieConsentRulesPageSize    = 500
	cookieConsentAuditLogPageSize = 200

	// maxListPages bounds every pagination loop, so an endpoint that keeps answering with a fresh
	// cursor cannot run an operation forever or without bound on memory.
	maxListPages = 1000

	cookieConsentAuditLogPath = "/v1/cookie-consent/audit-log"
)

// GetCookieConsentConfig reads one Cookie Consent configuration and its install script.
type GetCookieConsentConfig struct{}

// GetCookieConsentConfigArgs identifies the configuration to read.
type GetCookieConsentConfigArgs struct {
	ConfigID string `pulumi:"configId"`
}

// Annotate documents the getCookieConsentConfig inputs.
func (args *GetCookieConsentConfigArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.ConfigID, "The Osano Cookie Consent config ID (UUID) to read.")
}

// GetCookieConsentConfigResult is a configuration read through the Customer REST API.
type GetCookieConsentConfigResult struct {
	ConfigID string `pulumi:"configId"`
	Exists   bool   `pulumi:"exists"`

	Name                string         `pulumi:"name"`
	Domains             []string       `pulumi:"domains"`
	Mode                string         `pulumi:"mode"`
	OrgIDs              []string       `pulumi:"orgIds"`
	Configuration       map[string]any `pulumi:"configuration"`
	CustomerID          string         `pulumi:"customerId"`
	Created             int            `pulumi:"created"`
	Updated             int            `pulumi:"updated"`
	PublishStatus       string         `pulumi:"publishStatus"`
	LastPublished       int            `pulumi:"lastPublished"`
	PublishedRevision   int            `pulumi:"publishedRevision"`
	TattleRecordStopped bool           `pulumi:"tattleRecordStopped"`
	ScriptSrc           string         `pulumi:"scriptSrc"`
	ScriptTag           string         `pulumi:"scriptTag"`
}

// Annotate documents the getCookieConsentConfig outputs.
func (r *GetCookieConsentConfigResult) Annotate(a infer.Annotator) {
	a.Describe(&r.ConfigID, "The config ID that was looked up.")
	a.Describe(&r.Exists, "Whether Osano returned the configuration. The other outputs are empty when false.")
	describeCookieConsentConfigFields(a, cookieConsentConfigFieldPointers{
		name: &r.Name, domains: &r.Domains, mode: &r.Mode, orgIDs: &r.OrgIDs,
		configuration: &r.Configuration, customerID: &r.CustomerID, created: &r.Created,
		updated: &r.Updated, publishStatus: &r.PublishStatus, lastPublished: &r.LastPublished,
		publishedRevision: &r.PublishedRevision, tattleRecordStopped: &r.TattleRecordStopped,
		scriptSrc: &r.ScriptSrc, scriptTag: &r.ScriptTag,
	})
}

// Annotate registers the getCookieConsentConfig function.
func (g *GetCookieConsentConfig) Annotate(a infer.Annotator) {
	a.SetToken("index", "getCookieConsentConfig")
	a.Describe(
		g,
		"Reads a Cookie Consent configuration, including its publish status and the CMP script to install, "+
			"without managing it. Use it to fetch the script for a configuration another stack or the Osano "+
			"dashboard owns. The script serves the most recently published revision, so check publishStatus "+
			"and lastPublished before relying on a new change.",
	)
}

// Invoke reads the configuration.
func (g *GetCookieConsentConfig) Invoke(
	ctx context.Context, req infer.FunctionRequest[GetCookieConsentConfigArgs],
) (infer.FunctionResponse[GetCookieConsentConfigResult], error) {
	configID := strings.TrimSpace(req.Input.ConfigID)
	if configID == "" {
		return infer.FunctionResponse[GetCookieConsentConfigResult]{}, errors.New("configId is required")
	}
	client, err := customerClient(ctx)
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentConfigResult]{}, err
	}

	var out cmpConfigResponse
	err = client.DoJSON(ctx, http.MethodGet, cookieConsentConfigPath(configID), nil, nil, &out)
	if osanoclient.IsHTTPStatus(err, http.StatusNotFound) {
		return infer.FunctionResponse[GetCookieConsentConfigResult]{
			Output: GetCookieConsentConfigResult{ConfigID: configID},
		}, nil
	}
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentConfigResult]{},
			fmt.Errorf("read Cookie Consent config %q: %w", configID, err)
	}

	details := cookieConsentConfigDetailsFromResponse(out)
	return infer.FunctionResponse[GetCookieConsentConfigResult]{Output: GetCookieConsentConfigResult{
		ConfigID:            configID,
		Exists:              true,
		Name:                details.Name,
		Domains:             details.Domains,
		Mode:                details.Mode,
		OrgIDs:              details.OrgIDs,
		Configuration:       details.Configuration,
		CustomerID:          details.CustomerID,
		Created:             details.Created,
		Updated:             details.Updated,
		PublishStatus:       details.PublishStatus,
		LastPublished:       details.LastPublished,
		PublishedRevision:   details.PublishedRevision,
		TattleRecordStopped: details.TattleRecordStopped,
		ScriptSrc:           details.ScriptSrc,
		ScriptTag:           details.ScriptTag,
	}}, nil
}

// CookieConsentConfigDetails is one configuration in a getCookieConsentConfigs result.
type CookieConsentConfigDetails struct {
	ConfigID            string         `pulumi:"configId"`
	Name                string         `pulumi:"name"`
	Domains             []string       `pulumi:"domains"`
	Mode                string         `pulumi:"mode"`
	OrgIDs              []string       `pulumi:"orgIds"`
	Configuration       map[string]any `pulumi:"configuration"`
	CustomerID          string         `pulumi:"customerId"`
	Created             int            `pulumi:"created"`
	Updated             int            `pulumi:"updated"`
	PublishStatus       string         `pulumi:"publishStatus"`
	LastPublished       int            `pulumi:"lastPublished"`
	PublishedRevision   int            `pulumi:"publishedRevision"`
	TattleRecordStopped bool           `pulumi:"tattleRecordStopped"`
	ScriptSrc           string         `pulumi:"scriptSrc"`
	ScriptTag           string         `pulumi:"scriptTag"`
}

// Annotate documents the configuration list item.
func (d *CookieConsentConfigDetails) Annotate(a infer.Annotator) {
	a.Describe(d, "A Cookie Consent configuration as Osano reports it, with its install script.")
	a.Describe(&d.ConfigID, "The Osano config ID (UUID).")
	describeCookieConsentConfigFields(a, cookieConsentConfigFieldPointers{
		name: &d.Name, domains: &d.Domains, mode: &d.Mode, orgIDs: &d.OrgIDs,
		configuration: &d.Configuration, customerID: &d.CustomerID, created: &d.Created,
		updated: &d.Updated, publishStatus: &d.PublishStatus, lastPublished: &d.LastPublished,
		publishedRevision: &d.PublishedRevision, tattleRecordStopped: &d.TattleRecordStopped,
		scriptSrc: &d.ScriptSrc, scriptTag: &d.ScriptTag,
	})
}

type cookieConsentConfigFieldPointers struct {
	name, mode, customerID, publishStatus, scriptSrc, scriptTag *string
	domains, orgIDs                                             *[]string
	configuration                                               *map[string]any
	created, updated, lastPublished, publishedRevision          *int
	tattleRecordStopped                                         *bool
}

func describeCookieConsentConfigFields(a infer.Annotator, f cookieConsentConfigFieldPointers) {
	a.Describe(f.name, "The configuration name.")
	a.Describe(f.domains, "Domains permitted to host the configuration.")
	a.Describe(f.mode, "Compliance mode: debug, permissive, or production.")
	a.Describe(f.orgIDs, "Organization IDs associated with the configuration.")
	a.Describe(f.configuration, "The CMP configuration object as Osano reports it, including server defaults.")
	a.Describe(f.customerID, "The Osano customer ID that owns the configuration.")
	a.Describe(f.created, "Unix timestamp when Osano created the configuration.")
	a.Describe(f.updated, "Unix timestamp when Osano last updated the configuration.")
	a.Describe(
		f.publishStatus,
		"Osano publication status: unpublished, in-progress, published, outdated (changed since the last "+
			"publish), or error.",
	)
	a.Describe(f.lastPublished, "Unix timestamp when Osano last published the configuration (0 if never).")
	a.Describe(f.publishedRevision, "Revision number most recently published by Osano.")
	a.Describe(f.tattleRecordStopped, "Whether Osano stopped recording discoveries (tattles) for the configuration.")
	a.Describe(
		f.scriptSrc,
		"The public hosted CMP JavaScript URL, https://cmp.osano.com/{customerId}/{configId}/osano.js. "+
			"It serves the most recently published revision.",
	)
	a.Describe(
		f.scriptTag,
		"The complete public CMP script tag to place first in the site head, without async or defer attributes.",
	)
}

func cookieConsentConfigDetailsFromResponse(resp cmpConfigResponse) CookieConsentConfigDetails {
	// A response without a customer ID cannot produce a script; leave the script outputs empty
	// rather than failing a read-only lookup.
	src, tag, _ := cookieConsentScript(resp.CustomerID, resp.ConfigID)
	return CookieConsentConfigDetails{
		ConfigID:            resp.ConfigID,
		Name:                resp.Name,
		Domains:             resp.Domains,
		Mode:                resp.Mode,
		OrgIDs:              resp.OrgIDs,
		Configuration:       resp.Configuration,
		CustomerID:          resp.CustomerID,
		Created:             resp.Created,
		Updated:             resp.Updated,
		PublishStatus:       resp.PublishStatus,
		LastPublished:       resp.LastPublished,
		PublishedRevision:   resp.PublishedRevision,
		TattleRecordStopped: resp.TattleRecordStopped,
		ScriptSrc:           src,
		ScriptTag:           tag,
	}
}

// GetCookieConsentConfigs lists Cookie Consent configurations with optional filters.
type GetCookieConsentConfigs struct{}

// GetCookieConsentConfigsArgs holds the list filters.
type GetCookieConsentConfigsArgs struct {
	Name                *string  `pulumi:"name,optional"`
	Domains             []string `pulumi:"domains,optional"`
	DomainsMatch        *string  `pulumi:"domainsMatch,optional"`
	OrgIDs              []string `pulumi:"orgIds,optional"`
	OrgIDsMatch         *string  `pulumi:"orgIdsMatch,optional"`
	Mode                *string  `pulumi:"mode,optional"`
	PublishStatus       *string  `pulumi:"publishStatus,optional"`
	TattleRecordStopped *bool    `pulumi:"tattleRecordStopped,optional"`
	SortBy              *string  `pulumi:"sortBy,optional"`
	MaxResults          *int     `pulumi:"maxResults,optional"`
}

// Annotate documents the getCookieConsentConfigs inputs.
func (args *GetCookieConsentConfigsArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.Name, "Filter by configuration name (case insensitive, partial matches allowed).")
	a.Describe(&args.Domains, "Filter by domains. Use Punycode for non-ASCII domains.")
	a.Describe(
		&args.DomainsMatch,
		"How domains filters: any (default) matches configs with any listed domain, all requires every "+
			"listed domain, and not excludes configs with any listed domain.",
	)
	a.Describe(&args.OrgIDs, "Filter by organization IDs (UUIDs).")
	a.Describe(&args.OrgIDsMatch, "How orgIds filters: any (default), all, or not, as for domainsMatch.")
	a.Describe(&args.Mode, "Filter by compliance mode: debug, permissive, or production.")
	a.Describe(
		&args.PublishStatus,
		"Filter by publish status: unpublished, in-progress, published, outdated, or error.",
	)
	a.Describe(&args.TattleRecordStopped, "Filter by whether discovery recording is stopped.")
	a.Describe(
		&args.SortBy,
		"Sort field: name, created (default), updated, or lastPublished. Results are in descending order.",
	)
	a.Describe(&args.MaxResults, "Stop after this many configurations. Unset or 0 returns every matching configuration.")
}

// GetCookieConsentConfigsResult lists the matching configurations.
type GetCookieConsentConfigsResult struct {
	Configs []CookieConsentConfigDetails `pulumi:"configs"`
}

// Annotate documents the getCookieConsentConfigs outputs.
func (r *GetCookieConsentConfigsResult) Annotate(a infer.Annotator) {
	a.Describe(&r.Configs, "The matching configurations, each with its install script.")
}

// Annotate registers the getCookieConsentConfigs function.
func (g *GetCookieConsentConfigs) Annotate(a infer.Annotator) {
	a.SetToken("index", "getCookieConsentConfigs")
	a.Describe(
		g,
		"Lists Cookie Consent configurations, optionally filtered by name, domains, organization, mode, "+
			"or publish status, and returns each one's install script. Follows pagination until maxResults "+
			"or the last page.",
	)
}

// Invoke lists the configurations.
func (g *GetCookieConsentConfigs) Invoke(
	ctx context.Context, req infer.FunctionRequest[GetCookieConsentConfigsArgs],
) (infer.FunctionResponse[GetCookieConsentConfigsResult], error) {
	query, maxResults, err := cookieConsentConfigsQuery(req.Input)
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentConfigsResult]{}, err
	}
	client, err := customerClient(ctx)
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentConfigsResult]{}, err
	}

	configs := []CookieConsentConfigDetails{}
	err = paginateCustomerList(ctx, client, cookieConsentConfigsPath, query, func(page []cmpConfigResponse) bool {
		for idx := range page {
			configs = append(configs, cookieConsentConfigDetailsFromResponse(page[idx]))
			if maxResults > 0 && len(configs) >= maxResults {
				return false
			}
		}
		return true
	})
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentConfigsResult]{},
			fmt.Errorf("list Cookie Consent configs: %w", err)
	}
	return infer.FunctionResponse[GetCookieConsentConfigsResult]{
		Output: GetCookieConsentConfigsResult{Configs: configs},
	}, nil
}

func cookieConsentConfigsQuery(args GetCookieConsentConfigsArgs) (url.Values, int, error) {
	query := url.Values{}
	pageSize := cookieConsentConfigsPageSize
	maxResults := 0
	if args.MaxResults != nil {
		if *args.MaxResults < 0 {
			return nil, 0, errors.New("maxResults must not be negative")
		}
		maxResults = *args.MaxResults
		if maxResults > 0 && maxResults < pageSize {
			pageSize = maxResults
		}
	}
	query.Set("limit", strconv.Itoa(pageSize))

	if args.Name != nil && strings.TrimSpace(*args.Name) != "" {
		query.Set("name", strings.TrimSpace(*args.Name))
	}
	domains, err := listFilter("domains", args.Domains, args.DomainsMatch)
	if err != nil {
		return nil, 0, err
	}
	if domains != "" {
		query.Set("domains", domains)
	}
	orgIDs, err := listFilter("orgIds", args.OrgIDs, args.OrgIDsMatch)
	if err != nil {
		return nil, 0, err
	}
	if orgIDs != "" {
		query.Set("orgIds", orgIDs)
	}
	for _, filter := range []struct {
		key, name string
		value     *string
		allowed   []string
	}{
		{"mode", "mode", args.Mode, cookieConsentModes},
		{"status", "publishStatus", args.PublishStatus, cookieConsentPublishStatuses},
		{"sortBy", "sortBy", args.SortBy, []string{"name", "created", "updated", "lastPublished"}},
	} {
		if filter.value == nil || *filter.value == "" {
			continue
		}
		if err := oneOf(filter.name, *filter.value, filter.allowed); err != nil {
			return nil, 0, err
		}
		query.Set(filter.key, *filter.value)
	}
	if args.TattleRecordStopped != nil {
		query.Set("tattleRecordStopped", strconv.FormatBool(*args.TattleRecordStopped))
	}
	return query, maxResults, nil
}

// listFilter encodes a comma-separated list filter with its optional any/all/not prefix.
func listFilter(name string, values []string, match *string) (string, error) {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, ",") {
			return "", fmt.Errorf("%s entries must not contain commas: %q", name, value)
		}
		cleaned = append(cleaned, value)
	}
	prefix := ""
	if match != nil && *match != "" {
		if err := oneOf(name+"Match", *match, []string{"any", "all", "not"}); err != nil {
			return "", err
		}
		if len(cleaned) == 0 {
			return "", fmt.Errorf("%sMatch requires at least one %s entry", name, name)
		}
		prefix = *match + ":"
	}
	if len(cleaned) == 0 {
		return "", nil
	}
	return prefix + strings.Join(cleaned, ","), nil
}

// GetCookieConsentRules lists the rules of a Cookie Consent configuration.
type GetCookieConsentRules struct{}

// GetCookieConsentRulesArgs selects the configuration and optional filters.
type GetCookieConsentRulesArgs struct {
	ConfigID       string  `pulumi:"configId"`
	StoreType      *string `pulumi:"storeType,optional"`
	Classification *string `pulumi:"classification,optional"`
	MaxResults     *int    `pulumi:"maxResults,optional"`
}

// Annotate documents the getCookieConsentRules inputs.
func (args *GetCookieConsentRulesArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.ConfigID, "The Osano Cookie Consent config ID whose rules are listed.")
	a.Describe(&args.StoreType, "Only return rules of this storage type: "+strings.Join(storeTypeValues(), ", ")+".")
	a.Describe(
		&args.Classification,
		"Only return rules with this classification: "+strings.Join(ruleClassifications, ", ")+".",
	)
	a.Describe(&args.MaxResults, "Stop after this many rules. Unset or 0 returns every matching rule.")
}

// CookieConsentRuleDetails is one rule in a getCookieConsentRules result.
type CookieConsentRuleDetails struct {
	RuleID         int     `pulumi:"ruleId"`
	ConfigID       string  `pulumi:"configId"`
	StoreType      string  `pulumi:"storeType"`
	Classification string  `pulumi:"classification"`
	Rule           string  `pulumi:"rule"`
	Disclosure     bool    `pulumi:"disclosure"`
	Title          *string `pulumi:"title,optional"`
	VendorName     *string `pulumi:"vendorName,optional"`
	VendorID       *string `pulumi:"vendorId,optional"`
	RuleType       *string `pulumi:"ruleType,optional"`
	Description    *string `pulumi:"description,optional"`
	Expiry         *string `pulumi:"expiry,optional"`
	Created        string  `pulumi:"created"`
	Updated        string  `pulumi:"updated"`
}

// Annotate documents the rule list item.
func (d *CookieConsentRuleDetails) Annotate(a infer.Annotator) {
	a.Describe(d, "A classification rule of a Cookie Consent configuration as Osano reports it.")
	a.Describe(&d.RuleID, "The server-assigned integer rule ID. Import a rule with <configId>/<ruleId>.")
	a.Describe(&d.ConfigID, "The configuration the rule belongs to.")
	a.Describe(
		&d.StoreType,
		"The storage type: cookies, scripts, iframes, or localStorage. Osano's raw type is passed through "+
			"when it is not one of these.",
	)
	a.Describe(&d.Classification, "The rule classification.")
	a.Describe(&d.Rule, "The rule pattern.")
	a.Describe(&d.Disclosure, "Whether the rule is disclosed.")
	a.Describe(&d.Title, "The disclosure title, if set.")
	a.Describe(&d.VendorName, "The vendor name, if set.")
	a.Describe(&d.VendorID, "The Osano vendor ID, if set.")
	a.Describe(&d.RuleType, "The matching mode, if set.")
	a.Describe(&d.Description, "The cookie description, if set (cookies only).")
	a.Describe(&d.Expiry, "The cookie expiry description, if set (cookies only).")
	a.Describe(&d.Created, "When the rule was created (ISO 8601).")
	a.Describe(&d.Updated, "When the rule was last updated (ISO 8601).")
}

// GetCookieConsentRulesResult lists the rules.
type GetCookieConsentRulesResult struct {
	ConfigID string                     `pulumi:"configId"`
	Rules    []CookieConsentRuleDetails `pulumi:"rules"`
}

// Annotate documents the getCookieConsentRules outputs.
func (r *GetCookieConsentRulesResult) Annotate(a infer.Annotator) {
	a.Describe(&r.ConfigID, "The config ID that was queried.")
	a.Describe(&r.Rules, "Every matching rule, across all pages.")
}

// Annotate registers the getCookieConsentRules function.
func (g *GetCookieConsentRules) Annotate(a infer.Annotator) {
	a.SetToken("index", "getCookieConsentRules")
	a.Describe(
		g,
		"Lists the classification rules of a Cookie Consent configuration, optionally filtered by storage "+
			"type and classification. Useful for auditing rules managed outside Pulumi or finding rule IDs to import.",
	)
}

// Invoke lists the rules.
func (g *GetCookieConsentRules) Invoke(
	ctx context.Context, req infer.FunctionRequest[GetCookieConsentRulesArgs],
) (infer.FunctionResponse[GetCookieConsentRulesResult], error) {
	configID := strings.TrimSpace(req.Input.ConfigID)
	if configID == "" {
		return infer.FunctionResponse[GetCookieConsentRulesResult]{}, errors.New("configId is required")
	}
	query := url.Values{"limit": []string{strconv.Itoa(cookieConsentRulesPageSize)}}
	if req.Input.StoreType != nil && *req.Input.StoreType != "" {
		apiType, err := ruleAPIType(*req.Input.StoreType)
		if err != nil {
			return infer.FunctionResponse[GetCookieConsentRulesResult]{}, err
		}
		query.Set("type", apiType)
	}
	if req.Input.Classification != nil && *req.Input.Classification != "" {
		if err := oneOf("classification", *req.Input.Classification, ruleClassifications); err != nil {
			return infer.FunctionResponse[GetCookieConsentRulesResult]{}, err
		}
		query.Set("classification", *req.Input.Classification)
	}
	maxResults := 0
	if req.Input.MaxResults != nil {
		if *req.Input.MaxResults < 0 {
			return infer.FunctionResponse[GetCookieConsentRulesResult]{}, errors.New("maxResults must not be negative")
		}
		maxResults = *req.Input.MaxResults
		if maxResults > 0 && maxResults < cookieConsentRulesPageSize {
			query.Set("limit", strconv.Itoa(maxResults))
		}
	}
	client, err := customerClient(ctx)
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentRulesResult]{}, err
	}

	rules := []CookieConsentRuleDetails{}
	err = paginateCustomerList(ctx, client, cookieConsentConfigPath(configID)+"/rules", query,
		func(page []cmpRuleResponse) bool {
			for idx := range page {
				rules = append(rules, cookieConsentRuleDetailsFromResponse(page[idx], configID))
				if maxResults > 0 && len(rules) >= maxResults {
					return false
				}
			}
			return true
		})
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentRulesResult]{},
			fmt.Errorf("list rules of Cookie Consent config %q: %w", configID, err)
	}
	return infer.FunctionResponse[GetCookieConsentRulesResult]{
		Output: GetCookieConsentRulesResult{ConfigID: configID, Rules: rules},
	}, nil
}

func cookieConsentRuleDetailsFromResponse(resp cmpRuleResponse, configID string) CookieConsentRuleDetails {
	if resp.ConfigID != "" {
		configID = resp.ConfigID
	}
	storeType, err := ruleStoreType(resp.Type)
	if err != nil {
		storeType = resp.Type
	}
	var vendorID *string
	if resp.VendorID != "" {
		vendorID = &resp.VendorID
	}
	return CookieConsentRuleDetails{
		RuleID:         resp.RuleID,
		ConfigID:       configID,
		StoreType:      storeType,
		Classification: resp.Classification,
		Rule:           resp.Rule,
		Disclosure:     resp.Disclosure,
		Title:          resp.Title,
		VendorName:     resp.VendorName,
		VendorID:       vendorID,
		RuleType:       resp.RuleType,
		Description:    resp.Description,
		Expiry:         resp.Expiry,
		Created:        resp.Created,
		Updated:        resp.Updated,
	}
}

// GetCookieConsentDiscoveries lists what osano.js or URL scans discovered for a configuration.
type GetCookieConsentDiscoveries struct{}

// GetCookieConsentDiscoveriesArgs selects the configuration and storage type.
type GetCookieConsentDiscoveriesArgs struct {
	ConfigID  string  `pulumi:"configId"`
	StoreType *string `pulumi:"storeType,optional"`
}

// Annotate documents the getCookieConsentDiscoveries inputs.
func (args *GetCookieConsentDiscoveriesArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.ConfigID, "The Osano Cookie Consent config ID whose discoveries are listed.")
	a.Describe(
		&args.StoreType,
		"The storage type to list: cookies (the default, as in the Osano API), "+
			strings.Join(storeTypeValues()[1:], ", ")+".",
	)
}

// CookieConsentDiscovery is one discovery in a getCookieConsentDiscoveries result.
type CookieConsentDiscovery struct {
	StoreKey      string  `pulumi:"storeKey"`
	StoreType     string  `pulumi:"storeType"`
	Created       string  `pulumi:"created"`
	Updated       string  `pulumi:"updated"`
	ScanOrigin    *string `pulumi:"scanOrigin,optional"`
	FirstPageSeen string  `pulumi:"firstPageSeen"`
	Confidence    *string `pulumi:"confidence,optional"`
}

// Annotate documents the discovery item.
func (d *CookieConsentDiscovery) Annotate(a infer.Annotator) {
	a.Describe(d, "A cookie, script, iframe, or localStorage key that osano.js or a URL scan discovered on the site.")
	a.Describe(&d.StoreKey, "The discovered cookie name, script or iframe URL, or localStorage key.")
	a.Describe(&d.StoreType, "The discovery's storage type as Osano reports it.")
	a.Describe(&d.Created, "When the discovery was first seen (ISO 8601).")
	a.Describe(&d.Updated, "When the discovery was last updated (ISO 8601).")
	a.Describe(&d.ScanOrigin, "\"URL Scan\" or \"osano.js\"; unset when Osano does not know the origin.")
	a.Describe(&d.FirstPageSeen, "The page URL where the item was first seen.")
	a.Describe(
		&d.Confidence,
		"Osano's AI classification confidence (Unknown, Low, Medium, or High). Only reported for cookies.",
	)
}

// GetCookieConsentDiscoveriesResult lists the discoveries.
type GetCookieConsentDiscoveriesResult struct {
	ConfigID    string                   `pulumi:"configId"`
	StoreType   string                   `pulumi:"storeType"`
	Discoveries []CookieConsentDiscovery `pulumi:"discoveries"`
}

// Annotate documents the getCookieConsentDiscoveries outputs.
func (r *GetCookieConsentDiscoveriesResult) Annotate(a infer.Annotator) {
	a.Describe(&r.ConfigID, "The config ID that was queried.")
	a.Describe(&r.StoreType, "The storage type that was listed.")
	a.Describe(&r.Discoveries, "The discoveries Osano reports for the configuration and storage type.")
}

// Annotate registers the getCookieConsentDiscoveries function.
func (g *GetCookieConsentDiscoveries) Annotate(a infer.Annotator) {
	a.SetToken("index", "getCookieConsentDiscoveries")
	a.Describe(
		g,
		"Lists the cookies, scripts, iframes, or localStorage keys that osano.js or URL scans discovered "+
			"for a configuration. Use it to review what still needs a rule before switching a configuration "+
			"to production mode, which blocks everything unclassified.",
	)
}

// Invoke lists the discoveries.
func (g *GetCookieConsentDiscoveries) Invoke(
	ctx context.Context, req infer.FunctionRequest[GetCookieConsentDiscoveriesArgs],
) (infer.FunctionResponse[GetCookieConsentDiscoveriesResult], error) {
	configID := strings.TrimSpace(req.Input.ConfigID)
	if configID == "" {
		return infer.FunctionResponse[GetCookieConsentDiscoveriesResult]{}, errors.New("configId is required")
	}
	storeType := "cookies"
	if req.Input.StoreType != nil && *req.Input.StoreType != "" {
		storeType = *req.Input.StoreType
	}
	apiType, err := ruleAPIType(storeType)
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentDiscoveriesResult]{}, err
	}
	client, err := customerClient(ctx)
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentDiscoveriesResult]{}, err
	}

	var out struct {
		Items []struct {
			StoreKey      string  `json:"storeKey"`
			StoreType     string  `json:"storeType"`
			Created       string  `json:"created"`
			Updated       string  `json:"updated"`
			ScanOrigin    *string `json:"scanOrigin"`
			FirstPageSeen string  `json:"firstPageSeen"`
			Confidence    *string `json:"confidence"`
		} `json:"items"`
	}
	if err := client.DoJSON(
		ctx, http.MethodGet, cookieConsentConfigPath(configID)+"/discoveries",
		url.Values{"type": []string{apiType}}, nil, &out,
	); err != nil {
		return infer.FunctionResponse[GetCookieConsentDiscoveriesResult]{},
			fmt.Errorf("list discoveries of Cookie Consent config %q: %w", configID, err)
	}

	discoveries := make([]CookieConsentDiscovery, 0, len(out.Items))
	for _, item := range out.Items {
		discoveries = append(discoveries, CookieConsentDiscovery{
			StoreKey:      item.StoreKey,
			StoreType:     item.StoreType,
			Created:       item.Created,
			Updated:       item.Updated,
			ScanOrigin:    item.ScanOrigin,
			FirstPageSeen: item.FirstPageSeen,
			Confidence:    item.Confidence,
		})
	}
	return infer.FunctionResponse[GetCookieConsentDiscoveriesResult]{
		Output: GetCookieConsentDiscoveriesResult{ConfigID: configID, StoreType: storeType, Discoveries: discoveries},
	}, nil
}

// GetCookieConsentAuditLog queries Cookie Consent audit events.
type GetCookieConsentAuditLog struct{}

// GetCookieConsentAuditLogArgs holds the audit log filters.
type GetCookieConsentAuditLogArgs struct {
	ConfigIDs  []string `pulumi:"configIds,optional"`
	EventTypes []string `pulumi:"eventTypes,optional"`
	EventIDs   []string `pulumi:"eventIds,optional"`
	ChangeType *string  `pulumi:"changeType,optional"`
	Actor      *string  `pulumi:"actor,optional"`
	StartDate  *string  `pulumi:"startDate,optional"`
	EndDate    *string  `pulumi:"endDate,optional"`
	MaxResults *int     `pulumi:"maxResults,optional"`
}

// Annotate documents the getCookieConsentAuditLog inputs.
func (args *GetCookieConsentAuditLogArgs) Annotate(a infer.Annotator) {
	a.Describe(&args.ConfigIDs, "Only return events for these configuration IDs.")
	a.Describe(
		&args.EventTypes,
		"Only return these event types, for example cmp.configPublished, cmp.configUpdated, cmp.ruleCreated, "+
			"or cmp.ruleUpdated.",
	)
	a.Describe(&args.EventIDs, "Only return these audit event IDs, such as the changeIds of a cmp.configPublished event.")
	a.Describe(
		&args.ChangeType,
		"Only return change events of this type: text_customization, style, iab, setting, or rule.",
	)
	a.Describe(&args.Actor, "Only return events performed by this user email (case insensitive).")
	a.Describe(&args.StartDate, "Only return events at or after this UTC ISO 8601 timestamp.")
	a.Describe(&args.EndDate, "Only return events strictly before this UTC ISO 8601 timestamp.")
	a.Describe(
		&args.MaxResults,
		"Stop after this many events (most recent first). Defaults to 200; set 0 to return every matching event.",
	)
}

// CookieConsentAuditEvent is one event in a getCookieConsentAuditLog result.
type CookieConsentAuditEvent struct {
	ID        string                       `pulumi:"id"`
	Module    string                       `pulumi:"module"`
	EventType string                       `pulumi:"eventType"`
	Actor     *string                      `pulumi:"actor,optional"`
	Timestamp string                       `pulumi:"timestamp"`
	Metadata  map[string]any               `pulumi:"metadata,optional"`
	Resources []CookieConsentAuditResource `pulumi:"resources"`
}

// Annotate documents the audit event.
func (e *CookieConsentAuditEvent) Annotate(a infer.Annotator) {
	a.Describe(e, "One Cookie Consent audit log event: a configuration, rule, or publication change and who made it.")
	a.Describe(&e.ID, "The audit event ID.")
	a.Describe(&e.Module, "The Osano module, currently always CMP.")
	a.Describe(&e.EventType, "The machine-readable event type, such as cmp.configPublished.")
	a.Describe(&e.Actor, "The email of the user who performed the action, when known.")
	a.Describe(&e.Timestamp, "When the event occurred (UTC ISO 8601).")
	a.Describe(&e.Metadata, "Event-specific details, such as changed fields and before/after values.")
	a.Describe(&e.Resources, "The resources the event acted on.")
}

// CookieConsentAuditResource is a resource an audit event acted on.
type CookieConsentAuditResource struct {
	ResourceID   string  `pulumi:"resourceId"`
	ResourceType string  `pulumi:"resourceType"`
	ResourceName *string `pulumi:"resourceName,optional"`
	IsPrimary    bool    `pulumi:"isPrimary"`
}

// Annotate documents the audit event resource.
func (r *CookieConsentAuditResource) Annotate(a infer.Annotator) {
	a.Describe(r, "A resource an audit log event acted on.")
	a.Describe(&r.ResourceID, "The resource ID; for CMP events, the config ID.")
	a.Describe(&r.ResourceType, "The resource type, such as CMP.")
	a.Describe(&r.ResourceName, "The resource name, when known.")
	a.Describe(&r.IsPrimary, "Whether this is the event's primary resource.")
}

// GetCookieConsentAuditLogResult lists the events.
type GetCookieConsentAuditLogResult struct {
	Events []CookieConsentAuditEvent `pulumi:"events"`
}

// Annotate documents the getCookieConsentAuditLog outputs.
func (r *GetCookieConsentAuditLogResult) Annotate(a infer.Annotator) {
	a.Describe(&r.Events, "The matching events, most recent first.")
}

// Annotate registers the getCookieConsentAuditLog function.
func (g *GetCookieConsentAuditLog) Annotate(a infer.Annotator) {
	a.SetToken("index", "getCookieConsentAuditLog")
	a.Describe(
		g,
		"Queries the Cookie Consent audit log: configuration and rule changes and publications, with who "+
			"made them and when. Use it to confirm a pipeline's publication or to detect dashboard edits.",
	)
}

// Invoke queries the audit log.
func (g *GetCookieConsentAuditLog) Invoke(
	ctx context.Context, req infer.FunctionRequest[GetCookieConsentAuditLogArgs],
) (infer.FunctionResponse[GetCookieConsentAuditLogResult], error) {
	query, maxResults, err := cookieConsentAuditLogQuery(req.Input)
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentAuditLogResult]{}, err
	}
	client, err := customerClient(ctx)
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentAuditLogResult]{}, err
	}

	type auditEventResponse struct {
		ID        string         `json:"id"`
		Module    string         `json:"module"`
		EventType string         `json:"eventType"`
		Actor     *string        `json:"actor"`
		Timestamp string         `json:"timestamp"`
		Metadata  map[string]any `json:"metadata"`
		Resources []struct {
			ResourceID   string  `json:"resourceId"`
			ResourceType string  `json:"resourceType"`
			ResourceName *string `json:"resourceName"`
			IsPrimary    bool    `json:"isPrimary"`
		} `json:"resources"`
	}
	events := []CookieConsentAuditEvent{}
	// The audit log's next token carries the original filters and must be sent on its own.
	err = paginateCustomerListWith(ctx, client, cookieConsentAuditLogPath, query, true,
		func(page []auditEventResponse) bool {
			for _, item := range page {
				resources := make([]CookieConsentAuditResource, 0, len(item.Resources))
				for _, res := range item.Resources {
					resources = append(resources, CookieConsentAuditResource{
						ResourceID:   res.ResourceID,
						ResourceType: res.ResourceType,
						ResourceName: res.ResourceName,
						IsPrimary:    res.IsPrimary,
					})
				}
				events = append(events, CookieConsentAuditEvent{
					ID:        item.ID,
					Module:    item.Module,
					EventType: item.EventType,
					Actor:     item.Actor,
					Timestamp: item.Timestamp,
					Metadata:  item.Metadata,
					Resources: resources,
				})
				if maxResults > 0 && len(events) >= maxResults {
					return false
				}
			}
			return true
		})
	if err != nil {
		return infer.FunctionResponse[GetCookieConsentAuditLogResult]{},
			fmt.Errorf("query Cookie Consent audit log: %w", err)
	}
	return infer.FunctionResponse[GetCookieConsentAuditLogResult]{
		Output: GetCookieConsentAuditLogResult{Events: events},
	}, nil
}

func cookieConsentAuditLogQuery(args GetCookieConsentAuditLogArgs) (url.Values, int, error) {
	maxResults := cookieConsentAuditLogPageSize
	if args.MaxResults != nil {
		if *args.MaxResults < 0 {
			return nil, 0, errors.New("maxResults must not be negative")
		}
		maxResults = *args.MaxResults
	}
	pageSize := cookieConsentAuditLogPageSize
	if maxResults > 0 && maxResults < pageSize {
		pageSize = maxResults
	}
	query := url.Values{"limit": []string{strconv.Itoa(pageSize)}}
	for key, values := range map[string][]string{
		"configIds": args.ConfigIDs, "eventTypes": args.EventTypes, "ids": args.EventIDs,
	} {
		if joined, err := listFilter(key, values, nil); err != nil {
			return nil, 0, err
		} else if joined != "" {
			query.Set(key, joined)
		}
	}
	if args.ChangeType != nil && *args.ChangeType != "" {
		if err := oneOf("changeType", *args.ChangeType,
			[]string{"text_customization", "style", "iab", "setting", "rule"}); err != nil {
			return nil, 0, err
		}
		query.Set("changeType", *args.ChangeType)
	}
	for key, value := range map[string]*string{
		"actor": args.Actor, "startDate": args.StartDate, "endDate": args.EndDate,
	} {
		if value != nil && strings.TrimSpace(*value) != "" {
			query.Set(key, strings.TrimSpace(*value))
		}
	}
	return query, maxResults, nil
}

// paginateCustomerList GETs pth page by page, passing each page's items to handle until it returns
// false or a page has no next token. Follow-up requests keep the original query and add next.
func paginateCustomerList[T any](
	ctx context.Context, client jsonClient, pth string, query url.Values, handle func([]T) bool,
) error {
	return paginateCustomerListWith(ctx, client, pth, query, false, handle)
}

// paginateCustomerListWith is paginateCustomerList for endpoints whose next token already encodes
// the filters (nextOnly), where follow-up requests send only next.
func paginateCustomerListWith[T any](
	ctx context.Context, client jsonClient, pth string, query url.Values, nextOnly bool, handle func([]T) bool,
) error {
	seen := map[string]bool{}
	current := query
	for pages := 1; ; pages++ {
		var page struct {
			Items []T    `json:"items"`
			Next  string `json:"next"`
		}
		if err := client.DoJSON(ctx, http.MethodGet, pth, current, nil, &page); err != nil {
			return err
		}
		if !handle(page.Items) || page.Next == "" {
			return nil
		}
		if seen[page.Next] {
			return fmt.Errorf("pagination returned repeated cursor %q", page.Next)
		}
		if pages >= maxListPages {
			return fmt.Errorf("pagination exceeded %d pages", maxListPages)
		}
		seen[page.Next] = true
		if nextOnly {
			current = url.Values{"next": []string{page.Next}}
			continue
		}
		current = url.Values{}
		for key, values := range query {
			current[key] = append([]string(nil), values...)
		}
		current.Set("next", page.Next)
	}
}

var (
	cookieConsentModes           = []string{"debug", "permissive", "production"}
	cookieConsentPublishStatuses = []string{"unpublished", "in-progress", "published", "outdated", "error"}
)

func oneOf(name, value string, allowed []string) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of: %s; got %q", name, strings.Join(allowed, ", "), value)
}
