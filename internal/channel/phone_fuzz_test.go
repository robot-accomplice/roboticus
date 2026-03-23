package channel

import "testing"

func FuzzValidateE164(f *testing.F) {
	f.Add("+14155552671")
	f.Add("+447911123456")
	f.Add("14155552671")
	f.Add("+0123456789")
	f.Add("")
	f.Add("+")
	f.Add("hello")
	f.Add("+1234567890123456789")

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic.
		_ = ValidateE164(input)
	})
}
