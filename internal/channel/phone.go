package channel

import "regexp"

// e164Re validates E.164 phone numbers: + followed by 1-15 digits.
var e164Re = regexp.MustCompile(`^\+[1-9]\d{1,14}$`)

// ValidateE164 checks if a phone number is in valid E.164 format.
func ValidateE164(number string) bool {
	return e164Re.MatchString(number)
}
