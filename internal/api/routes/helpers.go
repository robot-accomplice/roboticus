package routes

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// parseIntParam extracts an integer query param with a fallback default.
func parseIntParam(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

// parsePeriodHours parses a period string like "24h" or "7d" into hours.
func parsePeriodHours(period string, fallback int) int {
	if period == "" {
		return fallback
	}
	period = strings.TrimSpace(period)
	if strings.HasSuffix(period, "d") {
		if d, err := strconv.Atoi(strings.TrimSuffix(period, "d")); err == nil && d > 0 {
			return d * 24
		}
	}
	if strings.HasSuffix(period, "h") {
		if h, err := strconv.Atoi(strings.TrimSuffix(period, "h")); err == nil && h > 0 {
			return h
		}
	}
	if h, err := strconv.Atoi(period); err == nil && h > 0 {
		return h
	}
	return fallback
}

// derefInt64 safely dereferences an *int64, returning 0 for nil.
func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// Base chain ID for wallet operations.
const baseChainID = 8453

// processStartTime records when the process started, used for uptime reporting.
var processStartTime = time.Now()
