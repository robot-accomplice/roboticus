package api

import (
	_ "embed"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

//go:embed dashboard_spa.html
var dashboardHTML string

// DashboardHandler serves the embedded SPA with CSP nonce injection.
// Each request gets a unique nonce injected into all <script> tags
// for Content-Security-Policy compliance.
func DashboardHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nonce := generateNonce()

		// Inject nonce into script tags and API_KEY = null.
		html := strings.Replace(dashboardHTML, "var BASE = '';", "var BASE = ''; var API_KEY = null;", 1)
		html = strings.ReplaceAll(html, "<script>", `<script nonce="`+nonce+`">`)

		// Truncate at </html> to strip any trailing garbage.
		if idx := strings.Index(html, "</html>"); idx >= 0 {
			html = html[:idx+len("</html>")]
		}

		csp := "default-src 'self'; " +
			"script-src 'self' 'nonce-" + nonce + "'; " +
			"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
			"img-src 'self' data:; " +
			"font-src 'self' https://fonts.gstatic.com; " +
			"connect-src 'self' ws: wss:; " +
			"frame-ancestors 'none'"

		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Write([]byte(html))
	}
}

func generateNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "fallback-nonce"
	}
	return hex.EncodeToString(b)
}
