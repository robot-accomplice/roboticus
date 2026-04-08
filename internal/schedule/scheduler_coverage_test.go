package schedule

import (
	"testing"
	"time"
)

func TestDurableScheduler_CalculateNextRun(t *testing.T) {
	s := NewDurableScheduler()
	job := &CronJob{
		Kind:       ScheduleCron,
		Expression: "0 9 * * *",
	}
	now := time.Date(2026, 3, 31, 8, 0, 0, 0, time.UTC)
	next := s.CalculateNextRun(job, now)
	if next == nil {
		t.Fatal("should return next run time")
	}
	if next.Hour() != 9 {
		t.Errorf("hour = %d, want 9", next.Hour())
	}
}

func TestDurableScheduler_CalculateNextRun_AlreadyPassed(t *testing.T) {
	s := NewDurableScheduler()
	job := &CronJob{
		Kind:       ScheduleCron,
		Expression: "0 9 * * *",
	}
	now := time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	next := s.CalculateNextRun(job, now)
	if next == nil {
		t.Fatal("should return next run time")
	}
	if next.Day() == 31 && next.Hour() <= 10 {
		t.Error("should be next day or later")
	}
}

func TestDurableScheduler_EvaluateCron(t *testing.T) {
	s := NewDurableScheduler()
	now := time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	// Never ran before — should fire.
	if !s.EvaluateCron("0 9 * * *", nil, now) {
		t.Error("should fire at 9:00 with no last run")
	}
}

func TestDurableScheduler_EvaluateInterval(t *testing.T) {
	s := NewDurableScheduler()
	now := time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC)
	lastRun := time.Date(2026, 3, 31, 9, 55, 0, 0, time.UTC)
	// 5 min interval = 300000ms.
	if !s.EvaluateInterval(&lastRun, 300000, now) {
		t.Error("should fire after 5 minutes")
	}
	recentRun := time.Date(2026, 3, 31, 9, 58, 0, 0, time.UTC)
	if s.EvaluateInterval(&recentRun, 300000, now) {
		t.Error("should not fire within interval")
	}
}

func TestDurableScheduler_EvaluateAt(t *testing.T) {
	s := NewDurableScheduler()
	target := "2026-03-31T09:00:00Z"
	now := time.Date(2026, 3, 31, 9, 0, 30, 0, time.UTC)
	if !s.EvaluateAt(target, now) {
		t.Error("should match within tolerance")
	}
}

func TestDurableScheduler_IsDue_Cron(t *testing.T) {
	s := NewDurableScheduler()
	now := time.Date(2026, 3, 31, 9, 0, 0, 0, time.UTC)
	job := &CronJob{
		Kind:       ScheduleCron,
		Expression: "0 9 * * *",
		Enabled:    true,
	}
	if !s.IsDue(job, now) {
		t.Error("cron job should be due at 9:00")
	}
}

func TestDurableScheduler_IsDue_UnknownKind(t *testing.T) {
	s := NewDurableScheduler()
	job := &CronJob{
		Kind:       "unknown",
		Expression: "* * * * *",
		Enabled:    true,
	}
	if s.IsDue(job, time.Now()) {
		t.Error("unknown kind should not be due")
	}
}

func TestMatchesCron_Wildcards(t *testing.T) {
	at := time.Date(2026, 6, 15, 14, 30, 0, 0, time.UTC)
	if !matchesCron("* * * * *", at) {
		t.Error("all wildcards should match")
	}
}

func TestMatchesCron_Step(t *testing.T) {
	at0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at7 := time.Date(2026, 1, 1, 0, 7, 0, 0, time.UTC)
	if !matchesCron("*/10 * * * *", at0) {
		t.Error("*/10 should match minute 0")
	}
	if matchesCron("*/10 * * * *", at7) {
		t.Error("*/10 should not match minute 7")
	}
}

func TestCronFieldMatches_Extended(t *testing.T) {
	tests := []struct {
		field string
		value int
		max   int
		want  bool
	}{
		{"*/15", 0, 59, true},
		{"*/15", 15, 59, true},
		{"*/15", 10, 59, false},
		{"1-5", 3, 59, true},
		{"1-5", 6, 59, false},
		{"0-23", 12, 23, true},
	}
	for _, tt := range tests {
		got := cronFieldMatches(tt.field, tt.value, tt.max)
		if got != tt.want {
			t.Errorf("cronFieldMatches(%q, %d, %d) = %v, want %v", tt.field, tt.value, tt.max, got, tt.want)
		}
	}
}

func TestIsValidCronExpression_EdgeCases(t *testing.T) {
	// These should all be valid.
	valid := []string{"0 9 * * 1-5", "0 0 1,15 * *", "0 0 * * 0"}
	for _, expr := range valid {
		if !IsValidCronExpression(expr) {
			t.Errorf("IsValidCronExpression(%q) should be true", expr)
		}
	}
	// These should be invalid.
	invalid := []string{"", "bad", "0 9"}
	for _, expr := range invalid {
		if IsValidCronExpression(expr) {
			t.Errorf("IsValidCronExpression(%q) should be false", expr)
		}
	}
}

func TestParseTZPrefix(t *testing.T) {
	loc, expr := parseTZPrefix("TZ=America/New_York 0 9 * * *")
	if loc == nil {
		t.Fatal("should parse timezone")
	}
	if loc.String() != "America/New_York" {
		t.Errorf("timezone = %s", loc.String())
	}
	if expr != "0 9 * * *" {
		t.Errorf("expr = %s", expr)
	}
}

func TestParseTZPrefix_CronTZ(t *testing.T) {
	// Rust supports CRON_TZ= prefix in addition to TZ= (parity test).
	loc, expr := parseTZPrefix("CRON_TZ=Europe/London 30 14 * * 1-5")
	if loc == nil {
		t.Fatal("should parse CRON_TZ prefix")
	}
	if loc.String() != "Europe/London" {
		t.Errorf("timezone = %s, want Europe/London", loc.String())
	}
	if expr != "30 14 * * 1-5" {
		t.Errorf("expr = %s", expr)
	}
}

func TestParseTZPrefix_NoTZ(t *testing.T) {
	loc, expr := parseTZPrefix("0 9 * * *")
	if loc != nil {
		t.Error("should return nil for no TZ prefix")
	}
	if expr != "0 9 * * *" {
		t.Errorf("expr = %s", expr)
	}
}
