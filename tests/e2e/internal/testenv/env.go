package testenv

import (
	"os"
	"strings"
	"testing"
)

const (
	EnvUnifiedConsentAPIKey        = "OSANO_UC_API_KEY" //nolint:gosec // Environment variable name, not a credential.
	EnvOsanoAPIKey                 = "OSANO_API_KEY"    //nolint:gosec // Environment variable name, not a credential.
	EnvAPIBaseURL                  = "OSANO_API_BASE_URL"
	EnvRunSubjectE2E               = "OSANO_RUN_SUBJECT_E2E"
	EnvRunConsentE2E               = "OSANO_RUN_CONSENT_E2E"
	EnvRunWriteE2E                 = "OSANO_RUN_WRITE_E2E"
	EnvHashedSubjectID             = "OSANO_HASHED_SUBJECT_ID"
	EnvVerificationChannel         = "OSANO_VERIFICATION_CHANNEL"
	EnvVerificationEmail           = "OSANO_VERIFICATION_EMAIL"
	EnvVerificationPhone           = "OSANO_VERIFICATION_PHONE"
	EnvVerificationCode            = "OSANO_VERIFICATION_CODE"
	EnvVerificationSession         = "OSANO_VERIFICATION_SESSION"
	EnvTestSubjectRef              = "OSANO_TEST_SUBJECT_REF"
	EnvTestReferenceType           = "OSANO_TEST_REFERENCE_TYPE"
	EnvTestConfigID                = "OSANO_TEST_CONFIG_ID"
	EnvTestCollectionID            = "OSANO_TEST_COLLECTION_ID"
	EnvTestHashedSubjectID         = "OSANO_TEST_HASHED_SUBJECT_ID"
	EnvTestCollectionsJurisdiction = "OSANO_TEST_COLLECTIONS_JURISDICTION"
	EnvTestCollectionsType         = "OSANO_TEST_COLLECTIONS_TYPE"
	EnvWriteSubjectType            = "OSANO_WRITE_SUBJECT_TYPE"
	EnvWriteSubjectValue           = "OSANO_WRITE_SUBJECT_VALUE"
	EnvWritePrivacyProtocolID      = "OSANO_WRITE_PRIVACY_PROTOCOL_ID"
	EnvWriteAction                 = "OSANO_WRITE_ACTION"
)

func Require(t testing.TB, key, description string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("set %s (%s)", key, description)
	}
	return value
}

func Optional(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func RequireOptIn(t testing.TB, key, description string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(key)) == "" {
		t.Skipf("set %s to %s", key, description)
	}
}

func MaybeGet(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
