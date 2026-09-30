package provider

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"
)

// This file validates the CookieConsentConfig configuration object against Osano's Customer REST API
// spec (the CmpConfig.configuration schema at https://developers.osano.com/customer-rest-api).
//
// Only what the spec defines precisely fails a check: value types, ranges, enums, and documented
// combinations. Keys the spec does not list only warn, because Osano may add configuration keys
// before this provider learns about them.

// cookieConsentBooleanKeys are the configuration keys whose values are booleans.
var cookieConsentBooleanKeys = []string{
	"allowTimeout", "amazonConsent", "ccpaRelaxed", "codeSplitting", "crossDomain", "deleteStorageOnOptout",
	"dntSupport", "enableDoNotSell", "enableDoNotSellDefault", "externalStylesheet", "forcedClassifyEnabled",
	"forceManagePreferences", "googleConsent", "gpcSupport", "managePreferencesEnabled", "microsoftConsent",
	"policyLinkInDrawer", "showConsentUuid", "showOptOutWidget", "showWidget",
}

// cookieConsentOtherKeys are the remaining configuration keys the spec defines.
var cookieConsentOtherKeys = []string{
	"additionalLinks", "doNotSellCategories", "iframeBlocking", "localStorageBlocking", "palette",
	"policyLinkText", "storagePolicyHref", "tattleSampling", "timeoutSeconds", "translations", "variantMapping",
}

var cookieConsentPaletteKeys = []string{
	"buttonAcceptBackgroundColor", "buttonAcceptBackgroundColorHover", "buttonAcceptBorderColor",
	"buttonAcceptForegroundColor", "buttonBackgroundColor", "buttonBackgroundColorHover", "buttonBorderColor",
	"buttonCloseColor", "buttonDenyBackgroundColor", "buttonDenyBackgroundColorHover", "buttonDenyBorderColor",
	"buttonDenyForegroundColor", "buttonRejectAllBackgroundColor", "buttonRejectAllBackgroundColorHover",
	"buttonRejectAllBorderColor", "buttonRejectAllForegroundColor", "buttonForegroundColor",
	"buttonManageBackgroundColor", "buttonManageBorderColor", "buttonManageForegroundColor",
	"buttonManageBackgroundColorHover", "dialogBackgroundColor", "dialogForegroundColor",
	"dialogGpcBackgroundColor", "dialogGpcBorderColor", "dialogGpcForegroundColor", "dialogGpcColor",
	"dialogType", "displayPosition", "gpcBackgroundColor", "gpcBorderColor", "gpcForegroundColor", "gpcColor",
	"gpcBackgroundColorHover", "infoDialogBackgroundColor", "infoDialogForegroundColor", "infoDialogOverlayColor",
	"infoDialogButtonBackgroundColor", "infoDialogButtonBackgroundColorHover", "infoDialogButtonBorderColor",
	"infoDialogButtonCloseColor", "infoDialogButtonForegroundColor", "infoDialogGpcBackgroundColor",
	"infoDialogGpcBorderColor", "infoDialogGpcForegroundColor", "infoDialogGpcColor", "infoDialogLinkColor",
	"infoDialogPosition", "infoDialogToggleOffTrackColor", "infoDialogToggleOffThumbColor",
	"infoDialogToggleOnTrackColor", "infoDialogToggleOnThumbColor", "linkColor", "optOutWidgetPosition", "theme",
	"toggleOffThumbColor", "toggleOffTrackColor", "toggleOnTrackColor", "toggleOnThumbColor",
	"toggleButtonOffColor", "toggleButtonOnColor", "toggleOffBackgroundColor", "toggleOnBackgroundColor",
	"widgetPosition", "focusOutlineColor",
}

// cookieConsentDeprecatedPaletteKeys maps each deprecated palette key to its replacement.
var cookieConsentDeprecatedPaletteKeys = map[string]string{
	"toggleButtonOffColor":     "toggleOffThumbColor",
	"toggleButtonOnColor":      "toggleOnThumbColor",
	"toggleOffBackgroundColor": "toggleOffTrackColor",
	"toggleOnBackgroundColor":  "toggleOnTrackColor",
}

var cookieConsentPaletteEnums = map[string][]string{
	"dialogType":           {"bar", "box"},
	"infoDialogPosition":   {"right", "left"},
	"optOutWidgetPosition": {"right", "left"},
	"widgetPosition":       {"right", "left"},
	"theme":                {"classic", "modern"},
}

var (
	cookieConsentBlockingModes     = []string{"", "debug", "permissive", "production"}
	cookieConsentDoNotSell         = []string{"MARKETING", "ANALYTICS", "PERSONALIZATION"}
	cookieConsentAdditionalLinkIDs = []string{
		"cookiePolicy", "doNotSellOrShare", "imprint", "googlePrivacyPolicy", "privacyPolicy", "privacyStatement",
		"securityPolicy", "storagePolicy", "subjectRightsRequest", "termsAndConditions", "termsOfService",
		"termsOfUse",
	}
	// The spec spells one value "storagePoloicy"; storagePolicy is accepted too so that a typo fix on
	// either side does not produce a warning.
	cookieConsentPolicyLinkTexts = []string{
		"cookieNotice", "cookiePolicy", "storagePolicy", "storagePoloicy", "privacyNotice", "privacyPolicy",
	}
	usJurisdictionPattern = regexp.MustCompile(`^us(-[a-z]{2})?$`)
)

// validateCookieConsentConfiguration checks a fully known configuration object. mode is the
// configuration's compliance mode, or "" when it is unknown; creating reports a new configuration.
func validateCookieConsentConfiguration(
	configuration map[string]any, mode string, creating bool,
) (failures []p.CheckFailure, warnings []string) {
	fail := func(key, format string, args ...any) {
		failures = append(failures, p.CheckFailure{
			Property: "configuration." + key,
			Reason:   "configuration." + key + " " + fmt.Sprintf(format, args...),
		})
	}

	if href, ok := configuration["storagePolicyHref"]; !ok {
		fail("storagePolicyHref", "is required")
	} else if s, isString := href.(string); !isString || strings.TrimSpace(s) == "" {
		fail("storagePolicyHref", "must be a non-empty string (the privacy policy URL)")
	}

	// declared returns a key's value when the program sets it to something other than null. A
	// declared null asks Osano to remove the value and restore its default, which every optional
	// key accepts, so nulls are not type-checked.
	declared := func(key string) (any, bool) {
		value, ok := configuration[key]
		return value, ok && value != nil
	}

	for _, key := range cookieConsentBooleanKeys {
		if value, ok := declared(key); ok {
			if _, isBool := value.(bool); !isBool {
				fail(key, "must be a boolean")
			}
		}
	}

	if value, ok := declared("tattleSampling"); ok {
		if n, isNumber := value.(float64); !isNumber || n < 0 || n > 1 {
			fail("tattleSampling", "must be a number from 0 to 1")
		}
	}
	if value, ok := declared("timeoutSeconds"); ok {
		if n, isNumber := value.(float64); !isNumber || n < 0 || n != math.Trunc(n) {
			fail("timeoutSeconds", "must be a whole number of seconds")
		}
	}
	for _, key := range []string{"iframeBlocking", "localStorageBlocking"} {
		if value, ok := declared(key); ok {
			if s, isString := value.(string); !isString || !slices.Contains(cookieConsentBlockingModes, s) {
				fail(key, `must be one of "", debug, permissive, or production`)
			}
		}
	}

	categories, hasCategories := declared("doNotSellCategories")
	if hasCategories {
		for _, message := range validateStringList(categories, cookieConsentDoNotSell) {
			fail("doNotSellCategories", "%s", message)
		}
	}
	if enabled, _ := configuration["enableDoNotSell"].(bool); enabled {
		if list, _ := categories.([]any); len(list) == 0 {
			fail("doNotSellCategories", "must list at least one category when enableDoNotSell is true")
		}
	}

	policyLinkText, _ := configuration["policyLinkText"].(string)
	if value, ok := declared("policyLinkText"); ok {
		if _, isString := value.(string); !isString {
			fail("policyLinkText", "must be a string")
		} else if !slices.Contains(cookieConsentPolicyLinkTexts, policyLinkText) {
			warnings = append(warnings, fmt.Sprintf(
				"configuration.policyLinkText %q is not one of the values Osano documents (cookieNotice, "+
					"cookiePolicy, storagePolicy, privacyNotice, privacyPolicy); Osano may reject it",
				policyLinkText,
			))
		}
	}
	if value, ok := declared("additionalLinks"); ok {
		for _, message := range validateAdditionalLinks(value, policyLinkText) {
			fail("additionalLinks", "%s", message)
		}
	}

	if value, ok := declared("variantMapping"); ok {
		mapping, usable, messages := validateVariantMapping(value)
		for _, message := range messages {
			fail("variantMapping", "%s", message)
		}
		if _, hasCCPA := configuration["ccpaRelaxed"]; hasCCPA && usable && mapping != nil {
			warnings = append(warnings,
				"configuration.ccpaRelaxed is ignored while variantMapping has entries: Osano overwrites it to "+
					"mirror the mapping, so a declared value that disagrees shows as drift on refresh. Remove "+
					"ccpaRelaxed and express the US banner formats in variantMapping.")
		}
	}

	if value, ok := declared("palette"); ok {
		paletteFailures, paletteWarnings := validatePalette(value)
		for _, message := range paletteFailures {
			fail("palette", "%s", message)
		}
		warnings = append(warnings, paletteWarnings...)
	}
	if value, ok := declared("translations"); ok {
		if _, isMap := value.(map[string]any); !isMap {
			fail("translations", "must be an object")
		}
	}

	// Osano enables Google Consent Mode by default for new configurations. An existing configuration
	// that leaves the key undeclared may already have it off, so only warn when it is declared on or
	// when the configuration is being created.
	if mode == "debug" {
		if enabled, set := configuration["googleConsent"].(bool); (set && enabled) || (!set && creating) {
			warnings = append(warnings,
				"configuration.googleConsent is enabled (Osano's default) while mode is debug: Osano then signals "+
					"denied Google Consent Mode consent for every visitor. Set googleConsent to false until the "+
					"configuration moves to permissive or production mode.")
		}
	}

	known := append(append([]string{}, cookieConsentBooleanKeys...), cookieConsentOtherKeys...)
	for _, key := range sortedKeys(configuration) {
		if !slices.Contains(known, key) {
			warnings = append(warnings, fmt.Sprintf(
				"configuration.%s is not in Osano's published Customer REST API spec, and Osano rejects "+
					"configuration keys it does not know; check the spelling unless Osano added it recently", key,
			))
		}
	}
	return failures, warnings
}

func validateStringList(value any, allowed []string) []string {
	list, ok := value.([]any)
	if !ok {
		return []string{"must be a list"}
	}
	var messages []string
	for idx, item := range list {
		if s, isString := item.(string); !isString || !slices.Contains(allowed, s) {
			messages = append(messages, fmt.Sprintf("[%d] must be one of %s", idx, strings.Join(allowed, ", ")))
		}
	}
	return messages
}

func validateAdditionalLinks(value any, policyLinkText string) []string {
	links, ok := value.([]any)
	if !ok || len(links) < 1 || len(links) > 2 {
		return []string{"must be a list of one or two [text, url] pairs; to show none, leave the key out"}
	}
	var messages []string
	for idx, link := range links {
		pair, isList := link.([]any)
		if !isList || len(pair) != 2 {
			messages = append(messages, fmt.Sprintf("[%d] must be a [text, url] pair", idx))
			continue
		}
		text, textOK := pair[0].(string)
		url, urlOK := pair[1].(string)
		switch {
		case !textOK || !slices.Contains(cookieConsentAdditionalLinkIDs, text):
			messages = append(messages, fmt.Sprintf(
				"[%d][0] must be one of %s", idx, strings.Join(cookieConsentAdditionalLinkIDs, ", ")))
		case text == policyLinkText:
			messages = append(messages, fmt.Sprintf("[%d][0] must differ from policyLinkText %q", idx, text))
		}
		if !urlOK || strings.TrimSpace(url) == "" {
			messages = append(messages, fmt.Sprintf("[%d][1] must be a non-empty URL", idx))
		}
	}
	return messages
}

// validateVariantMapping checks the US banner-format mapping. Osano accepts either {} (clear the
// mapping) or both byJurisdiction and behavior, and rejects anything else with 400.
func validateVariantMapping(value any) (mapping map[string]any, usable bool, messages []string) {
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil, false, []string{"must be an object"}
	}
	if len(mapping) == 0 {
		return mapping, false, nil
	}
	for key := range mapping {
		if key != "byJurisdiction" && key != "behavior" {
			messages = append(messages, fmt.Sprintf("has unknown key %q; only byJurisdiction and behavior are allowed", key))
		}
	}
	byJurisdiction, hasBy := mapping["byJurisdiction"]
	behavior, hasBehavior := mapping["behavior"]
	if !hasBy || !hasBehavior {
		return mapping, false, append(messages,
			"must set both byJurisdiction and behavior, or be {} to clear the stored mapping")
	}
	if behavior != "fallbackToOsano" {
		messages = append(messages, `behavior must be "fallbackToOsano"`)
	}
	jurisdictions, isMap := byJurisdiction.(map[string]any)
	if !isMap {
		return mapping, false, append(messages, "byJurisdiction must be an object")
	}
	for _, key := range sortedKeys(jurisdictions) {
		if !usJurisdictionPattern.MatchString(key) {
			messages = append(messages, fmt.Sprintf(
				"byJurisdiction key %q must be us or a lower-case state code such as us-ca", key))
		}
		if format := jurisdictions[key]; format != "one" && format != "three" {
			messages = append(messages, fmt.Sprintf(`byJurisdiction.%s must be "one" or "three"`, key))
		}
	}
	return mapping, len(jurisdictions) > 0, messages
}

func validatePalette(value any) (failures, warnings []string) {
	palette, ok := value.(map[string]any)
	if !ok {
		return []string{"must be an object"}, nil
	}
	for _, key := range sortedKeys(palette) {
		// Every palette property is nullable: null removes the value.
		if allowed, isEnum := cookieConsentPaletteEnums[key]; isEnum && palette[key] != nil {
			if s, isString := palette[key].(string); !isString || !slices.Contains(allowed, s) {
				failures = append(failures, fmt.Sprintf("%s must be one of %s", key, strings.Join(allowed, ", ")))
			}
		}
		if replacement, deprecated := cookieConsentDeprecatedPaletteKeys[key]; deprecated {
			warnings = append(warnings, fmt.Sprintf(
				"configuration.palette.%s is deprecated by Osano; use palette.%s instead", key, replacement))
		} else if !slices.Contains(cookieConsentPaletteKeys, key) {
			warnings = append(warnings, fmt.Sprintf(
				"configuration.palette.%s is not in Osano's published Customer REST API spec; check the spelling",
				key))
		}
	}
	if position, ok := palette["displayPosition"].(string); ok {
		barPositions := []string{"top", "bottom"}
		boxPositions := []string{"top-left", "top-right", "bottom-left", "bottom-right", "center"}
		// Without a declared dialogType, the one stored in Osano applies, so accept either set.
		allowed, context := append(append([]string{}, barPositions...), boxPositions...), ""
		switch palette["dialogType"] {
		case "bar":
			allowed, context = barPositions, " when dialogType is bar"
		case "box":
			allowed, context = boxPositions, " when dialogType is box"
		}
		if !slices.Contains(allowed, position) {
			failures = append(failures, fmt.Sprintf(
				"displayPosition must be one of %s%s", strings.Join(allowed, ", "), context))
		}
	}
	return failures, warnings
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// cookieConsentAtomicConfigurationKeys are object-valued configuration keys whose whole value is the
// setting, so a refresh adopts Osano's value instead of projecting it onto the declared keys.
var cookieConsentAtomicConfigurationKeys = []string{"variantMapping"}

// projectConfiguration projects Osano's configuration object onto the keys the program declares,
// recursing into nested objects such as palette and translations. Server-added defaults stay out of
// state at every level, so declaring part of a nested object does not make every `pulumi up
// --refresh` report drift and PATCH the configuration, which would also mark it outdated. A declared
// key that Osano omits keeps its declared value.
func projectConfiguration(server, declared map[string]any) map[string]any {
	return projectObject(server, declared, true)
}

// withRemovedKeysNulled returns the declared configuration plus an explicit null for every key the
// previous inputs declared and the new ones no longer do, recursing into nested objects the same
// way projectObject does. Osano treats null as "remove this value", so the update clears what the
// program stopped managing; the state still records the declared object without the nulls.
func withRemovedKeysNulled(declared, previous map[string]any) map[string]any {
	return nullRemovedKeys(declared, previous, true)
}

func nullRemovedKeys(declared, previous map[string]any, topLevel bool) map[string]any {
	merged := make(map[string]any, len(declared)+len(previous))
	for key, value := range declared {
		merged[key] = value
	}
	for key, previousValue := range previous {
		declaredValue, stillDeclared := declared[key]
		if !stillDeclared {
			merged[key] = nil
			continue
		}
		declaredObject, declaredIsObject := declaredValue.(map[string]any)
		previousObject, previousIsObject := previousValue.(map[string]any)
		atomic := topLevel && slices.Contains(cookieConsentAtomicConfigurationKeys, key)
		if declaredIsObject && previousIsObject && !atomic {
			merged[key] = nullRemovedKeys(declaredObject, previousObject, false)
		}
	}
	return merged
}

func projectObject(server, declared map[string]any, topLevel bool) map[string]any {
	projected := make(map[string]any, len(declared))
	for key, declaredValue := range declared {
		serverValue, ok := server[key]
		declaredObject, declaredIsObject := declaredValue.(map[string]any)
		// A declared null removes a value and Osano reports its default instead, and a declared {}
		// clears an object that Osano may report as null. Neither is drift.
		clearedObject := declaredIsObject && len(declaredObject) == 0 && serverValue == nil
		if !ok || declaredValue == nil || clearedObject {
			projected[key] = declaredValue
			continue
		}
		serverObject, serverIsObject := serverValue.(map[string]any)
		atomic := topLevel && slices.Contains(cookieConsentAtomicConfigurationKeys, key)
		if declaredIsObject && serverIsObject && !atomic {
			projected[key] = projectObject(serverObject, declaredObject, false)
			continue
		}
		projected[key] = serverValue
	}
	return projected
}
