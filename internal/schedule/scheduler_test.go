package schedule

import (
	"testing"
	"time"
)

func TestCronFieldMatches(t *testing.T) {
	cases := []struct {
		field string
		value int
		max   int
		want  bool
	}{
		{"*", 5, 59, true},
		{"5", 5, 59, true},
		{"5", 6, 59, false},
		{"1-5", 3, 59, true},
		{"1-5", 6, 59, false},
		{"*/15", 0, 59, true},
		{"*/15", 15, 59, true},
		{"*/15", 7, 59, false},
		{"1,3,5", 3, 59, true},
		{"1,3,5", 4, 59, false},
	}
	for _, tc := range cases {
		got := cronFieldMatches(tc.field, tc.value, tc.max)
		if got != tc.want {
			t.Errorf("cronFieldMatches(%q, %d, %d) = %v, want %v", tc.field, tc.value, tc.max, got, tc.want)
		}
	}
}

func TestMatchesCron(t *testing.T) {
	// "At minute 30 past hour 14" → 30 14 * * *
	ts := time.Date(2026, 3, 20, 14, 30, 0, 0, time.UTC)
	if !matchesCron("30 14 * * *", ts) {
		t.Error("should match 30 14 * * * at 14:30")
	}
	if matchesCron("0 14 * * *", ts) {
		t.Error("should not match 0 14 * * * at 14:30")
	}
}

func TestEvaluateCron_DoubleFirPrevention(t *testing.T) {
	s := NewDurableScheduler()
	now := time.Date(2026, 3, 20, 14, 30, 0, 0, time.UTC)
	lastRun := time.Date(2026, 3, 20, 14, 30, 5, 0, time.UTC) // same minute

	if s.EvaluateCron("30 14 * * *", &lastRun, now) {
		t.Error("should not fire twice in the same minute")
	}
}

func TestEvaluateInterval(t *testing.T) {
	s := NewDurableScheduler()
	now := time.Now()

	// Never run → should fire.
	if !s.EvaluateInterval(nil, 60000, now) {
		t.Error("nil last run should fire")
	}

	// Last run 30s ago, interval 60s → should not fire.
	lastRun := now.Add(-30 * time.Second)
	if s.EvaluateInterval(&lastRun, 60000, now) {
		t.Error("should not fire before interval")
	}

	// Last run 90s ago, interval 60s → should fire.
	lastRun = now.Add(-90 * time.Second)
	if !s.EvaluateInterval(&lastRun, 60000, now) {
		t.Error("should fire after interval")
	}
}

func TestEvaluateAt(t *testing.T) {
	s := NewDurableScheduler()
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

	if s.EvaluateAt(future, time.Now()) {
		t.Error("future time should not be due")
	}
	if !s.EvaluateAt(past, time.Now()) {
		t.Error("past time should be due")
	}
}

func TestIsValidCronExpression(t *testing.T) {
	valid := []string{
		"* * * * *",
		"0 */2 * * *",
		"30 14 1 * 1-5",
		"TZ=America/New_York 0 9 * * *",
	}
	for _, expr := range valid {
		if !IsValidCronExpression(expr) {
			t.Errorf("expected valid: %q", expr)
		}
	}

	invalid := []string{
		"",
		"* * *",       // too few fields
		"* * * * * *", // too many fields
		"abc * * * *",
	}
	for _, expr := range invalid {
		if IsValidCronExpression(expr) {
			t.Errorf("expected invalid: %q", expr)
		}
	}
}

func TestCalculateNextRun_Interval(t *testing.T) {
	s := NewDurableScheduler()
	now := time.Now()
	job := &CronJob{Kind: ScheduleInterval, IntervalMs: 60000}
	next := s.CalculateNextRun(job, now)
	if next == nil {
		t.Fatal("expected next run time")
	}
	diff := next.Sub(now)
	if diff < 59*time.Second || diff > 61*time.Second {
		t.Errorf("expected ~60s, got %v", diff)
	}
}
