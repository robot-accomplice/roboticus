package core

import "net/http"

// HTTPDoer abstracts HTTP request execution. All packages that make external
// HTTP calls should depend on this interface rather than *http.Client directly.
// This enables mock injection for testing without real network calls.
//
// Satisfied by *http.Client and any test double that returns canned responses.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}
