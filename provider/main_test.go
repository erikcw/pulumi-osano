package provider

import (
	"os"
	"testing"
	"time"
)

// TestMain shortens the retry backoff the provider's clients use, so tests that exercise retryable
// failures against mock servers do not wait for the production delays.
func TestMain(m *testing.M) {
	retryInitialBackoff = time.Millisecond
	os.Exit(m.Run())
}
