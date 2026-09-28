//go:build e2e && consentread

package e2e

import (
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/jflavan/pulumi-osano/tests/e2e/internal/testenv"
)

func TestUnifiedConsentLookups(t *testing.T) {
	testenv.RequireOptIn(t, testenv.EnvRunConsentE2E, "enable unified consent read tests")
	server := providerServer(t)

	subjectRef := testenv.Require(t, testenv.EnvTestSubjectRef, "subject reference value")
	referenceType := testenv.Optional(testenv.EnvTestReferenceType, "subject")
	configID := testenv.Require(t, testenv.EnvTestConfigID, "configuration identifier")
	hashedSubjectID := testenv.Require(t, testenv.EnvTestHashedSubjectID, "hashed subject identifier")

	unified := invoke(t, server, "getUnifiedConsent", map[string]property.Value{
		"subjectRef": property.New(subjectRef), "referenceType": property.New(referenceType),
	})
	if !boolOutput(t, unified, "exists") {
		t.Fatalf("no unified consent returned for %s (%s)", subjectRef, referenceType)
	}
	if consent := mapOutput(t, unified, "unifiedConsent"); stringOutput(t, consent, "subjectId") == "" {
		t.Fatal("unified consent payload missing the subject ID")
	}

	subject := invoke(t, server, "getSubject", map[string]property.Value{
		"subjectRef": property.New(subjectRef), "referenceType": property.New(referenceType),
	})
	if !boolOutput(t, subject, "exists") {
		t.Fatalf("subject %s (%s) not found", subjectRef, referenceType)
	}
	subjectID := stringOutput(t, subject, "subjectId")
	if subjectID == "" {
		t.Fatal("subject response missing id")
	}

	check := invoke(t, server, "checkConsent", map[string]property.Value{"subjectId": property.New(subjectID)})
	if !boolOutput(t, check, "exists") {
		t.Fatalf("consent not reported for subject %s", subjectID)
	}

	profile := invoke(t, server, "getConsentProfile", map[string]property.Value{
		"hashedSubjectId": property.New(hashedSubjectID), "configId": property.New(configID),
	})
	if !boolOutput(t, profile, "exists") {
		t.Fatalf("consent profile not found for hashed subject %s", hashedSubjectID)
	}
	if mapOutput(t, profile, "profile").Len() == 0 {
		t.Fatal("consent profile payload empty")
	}
}

func TestConfigAndCollectionsReads(t *testing.T) {
	testenv.RequireOptIn(t, testenv.EnvRunConsentE2E, "enable unified consent read tests")
	server := providerServer(t)

	config := mapOutput(t, invoke(t, server, "getConfig", nil), "config")
	if config.Len() == 0 {
		t.Fatal("config payload was empty")
	}

	filters := map[string]property.Value{}
	if jurisdiction := testenv.Optional(testenv.EnvTestCollectionsJurisdiction, ""); jurisdiction != "" {
		filters["jurisdiction"] = property.New(jurisdiction)
	}
	if collectionType := testenv.Optional(testenv.EnvTestCollectionsType, ""); collectionType != "" {
		filters["type"] = property.New(collectionType)
	}
	collections := invoke(t, server, "getCollections", filters)
	if jurisdictions := collections.Get("jurisdictions"); !jurisdictions.IsArray() || jurisdictions.AsArray().Len() == 0 {
		t.Fatal("collections response missing jurisdictions")
	}
	if mapOutput(t, collections, "collection").Len() == 0 {
		t.Fatal("collections response missing collection detail")
	}

	collectionID := testenv.Require(t, testenv.EnvTestCollectionID, "collection identifier")
	collection := invoke(t, server, "getCollection", map[string]property.Value{"collectionId": property.New(collectionID)})
	if !boolOutput(t, collection, "exists") {
		t.Fatalf("collection %s not found", collectionID)
	}
	if mapOutput(t, collection, "collection").Len() == 0 {
		t.Fatalf("collection %s payload empty", collectionID)
	}

	t.Logf("config has %d keys; collection %s returned %d keys",
		config.Len(), collectionID, mapOutput(t, collection, "collection").Len())
}
