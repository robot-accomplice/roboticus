package schedule

import "testing"

func FuzzIsValidCronExpression(f *testing.F) {
	f.Add("* * * * *")
	f.Add("0 */2 * * *")
	f.Add("30 14 1 * 1-5")
	f.Add("TZ=America/New_York 0 9 * * *")
	f.Add("")
	f.Add("not a cron")
	f.Add("* * *")
	f.Add("60 25 32 13 8")

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic.
		_ = IsValidCronExpression(input)
	})
}

func FuzzMatchesCron(f *testing.F) {
	f.Add("* * * * *")
	f.Add("0 0 1 1 0")
	f.Add("*/5 * * * *")
	f.Add("1,2,3 * * * *")
	f.Add("1-5 * * * *")

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic, even with malformed input.
		_ = IsValidCronExpression(input)
	})
}
