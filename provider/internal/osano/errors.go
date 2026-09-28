package osano

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxErrorBodyBytes is how much of an error response body an HTTPError reports. Osano's error
// bodies are short JSON documents; anything longer is usually an HTML page from a proxy.
const maxErrorBodyBytes = 2048

// HTTPError reports a non-success Osano API response.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("osano api error: status=%d", e.StatusCode)
	}
	return fmt.Sprintf("osano api error: status=%d body=%s", e.StatusCode, truncate(body, maxErrorBodyBytes))
}

// truncate shortens s to at most limit bytes without splitting a UTF-8 sequence.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "... (truncated)"
}

// IsHTTPStatus reports whether err wraps an HTTPError with statusCode.
func IsHTTPStatus(err error, statusCode int) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == statusCode
}
