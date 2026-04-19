package db

import (
	"fmt"
	"time"
)

const sqliteTimeFormat = "2006-01-02 15:04:05"

// FormatTime serializes timestamps in the SQLite-friendly format used by
// datetime('now') so lexical comparisons remain consistent.
func FormatTime(t time.Time) string {
	return t.UTC().Format(sqliteTimeFormat)
}

// ParseTime accepts both the SQLite text format and older RFC3339 cache rows.
func ParseTime(raw string) (time.Time, error) {
	for _, layout := range []string{
		sqliteTimeFormat,
		time.RFC3339,
		time.RFC3339Nano,
	} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp format: %q", raw)
}
