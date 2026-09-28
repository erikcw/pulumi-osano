//go:build e2e && subjectverification

package e2e

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/jflavan/pulumi-osano/tests/e2e/internal/testenv"
)

// TestSendAndVerifySubjectCode runs the sendSubjectCode and verifySubjectCode functions against
// Osano. The provider sends every configured key on these routes, so the Unified Consent key alone
// is enough; OSANO_API_KEY is sent too when it is set.
func TestSendAndVerifySubjectCode(t *testing.T) {
	testenv.RequireOptIn(t, testenv.EnvRunSubjectE2E, "enable subject verification tests")
	server := providerServer(t)

	// Osano's current API identifies the subject by email or phone; a hashed subject ID is optional.
	hashedSubjectID := testenv.Optional(testenv.EnvHashedSubjectID, "")
	channel := strings.ToLower(testenv.Require(t, testenv.EnvVerificationChannel, "verification channel (email or sms)"))
	contactField, contact := resolveVerificationContact(t, channel)

	args := map[string]property.Value{contactField: property.New(contact)}
	if hashedSubjectID != "" {
		args["hashedSubjectId"] = property.New(hashedSubjectID)
	}
	t.Logf("Sending verification code via %s to %s", channel, redactContact(contact))
	sent := invoke(t, server, "sendSubjectCode", args)
	if got := stringOutput(t, sent, "channel"); got != channel {
		t.Fatalf("expected channel %q, got %q", channel, got)
	}

	// SMS verification requires the challenge session. Osano does not document where it comes from,
	// so take it from the send-code response when present, or from the environment.
	session := testenv.Optional(testenv.EnvVerificationSession, "")
	if value := sent.Get("session"); session == "" && value.IsString() {
		session = value.AsString()
	}
	if channel == "sms" && session == "" {
		t.Fatalf("SMS verification needs a session: the send-code response had none; set %s",
			testenv.EnvVerificationSession)
	}

	code := strings.TrimSpace(os.Getenv(testenv.EnvVerificationCode))
	if code == "" {
		var readErr error
		code, readErr = promptForVerificationCode()
		if readErr != nil {
			t.Fatalf("unable to capture verification code: %v", readErr)
		}
	}
	if code == "" {
		t.Fatal("verification code cannot be empty")
	}

	verifyArgs := map[string]property.Value{contactField: property.New(contact), "code": property.New(code)}
	if hashedSubjectID != "" {
		verifyArgs["hashedSubjectId"] = property.New(hashedSubjectID)
	}
	if session != "" {
		verifyArgs["session"] = property.New(session)
	}
	verified := invoke(t, server, "verifySubjectCode", verifyArgs)
	if !boolOutput(t, verified, "verified") {
		t.Fatal("verification did not report verified")
	}
	if stringOutput(t, verified, "verifiedId") == "" {
		t.Log("Osano returned no verifiedId; the profile output holds the raw response")
	}
	t.Logf("Verification succeeded; profile has %d keys", mapOutput(t, verified, "profile").Len())
}

func resolveVerificationContact(t *testing.T, channel string) (field, contact string) {
	t.Helper()
	switch channel {
	case "email":
		return "email", testenv.Require(t, testenv.EnvVerificationEmail, "email recipient for verification code")
	case "sms":
		return "phone", testenv.Require(t, testenv.EnvVerificationPhone, "phone recipient for verification code")
	default:
		t.Fatalf("unsupported verification channel %q", channel)
		return "", ""
	}
}

func promptForVerificationCode() (string, error) {
	fmt.Print("Enter the verification code you received via email/SMS: ")
	reader := bufio.NewReader(os.Stdin)
	code, err := reader.ReadString('\n')
	return strings.TrimSpace(code), err
}
