package channel

import "strings"

// CheckAllowlist returns true if the sender is on the allowlist.
// Supports exact match and phone E.164 suffix matching (e.g., "5551234"
// matches "+15551234" and "15551234").
func CheckAllowlist(senderID string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true // empty allowlist = allow all
	}
	for _, allowed := range allowlist {
		if strings.EqualFold(senderID, allowed) {
			return true
		}
		if phoneSuffixMatch(senderID, allowed) {
			return true
		}
	}
	return false
}

// phoneSuffixMatch handles E.164 phone number matching where one side may
// have a country code prefix that the other lacks.
func phoneSuffixMatch(senderID, allowed string) bool {
	s := stripPhonePrefix(senderID)
	a := stripPhonePrefix(allowed)
	if s == "" || a == "" {
		return false
	}
	// Match if one is a suffix of the other.
	if len(s) >= len(a) {
		return strings.HasSuffix(s, a)
	}
	return strings.HasSuffix(a, s)
}

func stripPhonePrefix(s string) string {
	s = strings.TrimPrefix(s, "+")
	// Must be all digits to be a phone number.
	for _, c := range s {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return s
}
