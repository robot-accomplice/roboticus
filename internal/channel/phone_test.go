package channel

import "testing"

func TestValidateE164(t *testing.T) {
	valid := []string{
		"+14155552671",
		"+447911123456",
		"+491711234567",
		"+8613800138000",
		"+12",
	}
	for _, num := range valid {
		if !ValidateE164(num) {
			t.Errorf("expected valid E.164: %s", num)
		}
	}

	invalid := []string{
		"14155552671",       // no +
		"+0123456789",       // starts with 0
		"+",                 // no digits
		"",                  // empty
		"+1234567890123456", // too long (16 digits)
		"hello",
		"+1 234", // spaces
	}
	for _, num := range invalid {
		if ValidateE164(num) {
			t.Errorf("expected invalid E.164: %s", num)
		}
	}
}
