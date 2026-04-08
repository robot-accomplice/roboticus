package browser

import (
	"fmt"
	"strings"
)

// blockedSchemes lists URL schemes that must not be navigated to.
var blockedSchemes = []string{
	"file",
	"javascript",
	"data",
	"ftp",
	"chrome",
	"chrome-extension",
	"about",
	"blob",
}

// isSchemeAllowed returns true if the URL does not use a blocked scheme.
func isSchemeAllowed(url string) bool {
	lower := strings.ToLower(strings.TrimSpace(url))
	for _, scheme := range blockedSchemes {
		prefix := scheme + ":"
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	return true
}

// Selector and expression length limits.
const (
	MaxSelectorLength   = 500
	MaxExpressionLength = 100_000
)

// validateSelector checks a CSS selector for safety.
// It rejects selectors that are too long or contain curly braces (potential injection).
func validateSelector(sel string) error {
	if len(sel) > MaxSelectorLength {
		return fmt.Errorf("browser: selector exceeds maximum length of %d characters", MaxSelectorLength)
	}
	if strings.ContainsAny(sel, "{}") {
		return fmt.Errorf("browser: selector must not contain curly braces")
	}
	return nil
}

// validateExpression checks a JavaScript expression for safety.
// It rejects expressions that exceed the maximum length.
func validateExpression(expr string) error {
	if len(expr) > MaxExpressionLength {
		return fmt.Errorf("browser: expression exceeds maximum length of %d characters", MaxExpressionLength)
	}
	return nil
}
