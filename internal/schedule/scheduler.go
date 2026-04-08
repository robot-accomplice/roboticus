package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ScheduleKind describes how a job is triggered.
type ScheduleKind string

const (
	ScheduleCron     ScheduleKind = "cron"
	ScheduleInterval ScheduleKind = "interval"
	ScheduleAt       ScheduleKind = "at"
)

// CronJob represents a scheduled job definition.
type CronJob struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	AgentID      string       `json:"agent_id"`
	Kind         ScheduleKind `json:"schedule_kind"`
	Expression   string       `json:"schedule_expr"`     // cron expr or at timestamp
	IntervalMs   int64        `json:"schedule_every_ms"` // for interval kind
	PayloadJSON  string       `json:"payload_json"`
	Enabled      bool         `json:"enabled"`
	LastRunAt    *time.Time   `json:"last_run_at,omitempty"`
	NextRunAt    *time.Time   `json:"next_run_at,omitempty"`
	RetryCount   int          `json:"retry_count"`
	MaxRetries   int          `json:"max_retries"`
	RetryDelayMs int64        `json:"retry_delay_ms"`
}

// DurableScheduler evaluates cron/interval/at schedules (pure function, no DB).
type DurableScheduler struct{}

// NewDurableScheduler creates a scheduler.
func NewDurableScheduler() *DurableScheduler {
	return &DurableScheduler{}
}

// IsDue checks if a job should fire now.
func (s *DurableScheduler) IsDue(job *CronJob, now time.Time) bool {
	switch job.Kind {
	case ScheduleCron:
		return s.EvaluateCron(job.Expression, job.LastRunAt, now)
	case ScheduleInterval:
		return s.EvaluateInterval(job.LastRunAt, job.IntervalMs, now)
	case ScheduleAt:
		return s.EvaluateAt(job.Expression, now)
	default:
		return false
	}
}

// EvaluateCron checks if a 5-field cron expression matches now.
// Supports optional TZ prefix: "TZ=America/New_York * * * * *"
// Uses backward slot probing (Rust parity): scans backward up to 61s to find
// the nearest cron slot, then returns true only if now is within [slot, slot+60s).
// Prevents double-fire within the same slot.
func (s *DurableScheduler) EvaluateCron(expr string, lastRun *time.Time, now time.Time) bool {
	tz, cronExpr := parseTZPrefix(expr)
	if tz != nil {
		now = now.In(tz)
	}

	// Slot probe: scan backward from now minute-by-minute up to 61 seconds
	// to find the most recent minute that matches the cron expression.
	slot, found := findNearestCronSlot(cronExpr, now)
	if !found {
		return false
	}

	// Check if now is within [slot, slot+60s).
	if now.Before(slot) || !now.Before(slot.Add(60*time.Second)) {
		return false
	}

	// Prevent double-fire: if we ran in this same slot, skip.
	if lastRun != nil {
		last := *lastRun
		if tz != nil {
			last = last.In(tz)
		}
		if !last.Before(slot) && last.Before(slot.Add(60*time.Second)) {
			return false
		}
	}

	return true
}

// findNearestCronSlot scans backward from now up to 61 seconds to find the
// most recent minute boundary that matches the cron expression.
func findNearestCronSlot(cronExpr string, now time.Time) (time.Time, bool) {
	// Start from the current minute boundary.
	candidate := now.Truncate(time.Minute)
	earliest := now.Add(-61 * time.Second)

	for !candidate.Before(earliest) {
		if matchesCron(cronExpr, candidate) {
			return candidate, true
		}
		candidate = candidate.Add(-time.Minute)
	}
	return time.Time{}, false
}

// EvaluateInterval checks if enough time has passed since last run.
func (s *DurableScheduler) EvaluateInterval(lastRun *time.Time, intervalMs int64, now time.Time) bool {
	if intervalMs <= 0 {
		return false // zero or negative interval is invalid
	}
	if lastRun == nil {
		return true // never run, fire immediately
	}
	elapsed := now.Sub(*lastRun)
	return elapsed >= time.Duration(intervalMs)*time.Millisecond
}

// EvaluateAt checks if a one-shot timestamp has been reached.
func (s *DurableScheduler) EvaluateAt(expr string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, expr)
	if err != nil {
		return false
	}
	return !now.Before(t)
}

// CalculateNextRun computes the next fire time.
func (s *DurableScheduler) CalculateNextRun(job *CronJob, now time.Time) *time.Time {
	switch job.Kind {
	case ScheduleInterval:
		next := now.Add(time.Duration(job.IntervalMs) * time.Millisecond)
		return &next
	case ScheduleAt:
		t, err := time.Parse(time.RFC3339, job.Expression)
		if err != nil {
			return nil
		}
		return &t
	case ScheduleCron:
		// Scan forward minute-by-minute (max 7 days for complex expressions).
		tz, cronExpr := parseTZPrefix(job.Expression)
		candidate := now.Truncate(time.Minute).Add(time.Minute)
		limit := now.Add(7 * 24 * time.Hour)
		for candidate.Before(limit) {
			check := candidate
			if tz != nil {
				check = candidate.In(tz)
			}
			if matchesCron(cronExpr, check) {
				return &candidate
			}
			candidate = candidate.Add(time.Minute)
		}
		return nil
	default:
		return nil
	}
}

// parseTZPrefix extracts an optional TZ= or CRON_TZ= prefix from a cron expression.
// Supports both forms for Rust parity (Rust accepts both).
// Also supports fixed-offset timezones: UTC+05:30, UTC-5, UTC+0530, etc.
func parseTZPrefix(expr string) (*time.Location, string) {
	// Try both TZ= and CRON_TZ= prefixes.
	for _, prefix := range []string{"CRON_TZ=", "TZ="} {
		if strings.HasPrefix(expr, prefix) {
			parts := strings.SplitN(expr, " ", 2)
			if len(parts) == 2 {
				tzName := strings.TrimPrefix(parts[0], prefix)
				loc, err := loadLocationWithFixedOffset(tzName)
				if err == nil {
					return loc, parts[1]
				}
			}
		}
	}
	return nil, expr
}

// loadLocationWithFixedOffset tries time.LoadLocation first, then parses
// fixed-offset formats: UTC±HH:MM, UTC±HHMM, UTC±H, UTC±HH.
func loadLocationWithFixedOffset(name string) (*time.Location, error) {
	// Try standard IANA name first.
	loc, err := time.LoadLocation(name)
	if err == nil {
		return loc, nil
	}

	// Parse fixed-offset: must start with "UTC" followed by + or -.
	if !strings.HasPrefix(name, "UTC") || len(name) < 4 {
		return nil, fmt.Errorf("unknown timezone: %s", name)
	}

	sign := 1
	rest := name[3:]
	switch rest[0] {
	case '+':
		rest = rest[1:]
	case '-':
		sign = -1
		rest = rest[1:]
	default:
		return nil, fmt.Errorf("unknown timezone: %s", name)
	}

	var hours, minutes int
	switch {
	case strings.Contains(rest, ":"):
		// UTC±HH:MM
		parts := strings.SplitN(rest, ":", 2)
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("invalid fixed offset: %s", name)
		}
		hours, minutes = h, m
	case len(rest) == 4:
		// UTC±HHMM
		h, err1 := strconv.Atoi(rest[:2])
		m, err2 := strconv.Atoi(rest[2:])
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("invalid fixed offset: %s", name)
		}
		hours, minutes = h, m
	case len(rest) <= 2:
		// UTC±H or UTC±HH
		h, err1 := strconv.Atoi(rest)
		if err1 != nil {
			return nil, fmt.Errorf("invalid fixed offset: %s", name)
		}
		hours = h
	default:
		return nil, fmt.Errorf("invalid fixed offset: %s", name)
	}

	offsetSecs := sign * (hours*3600 + minutes*60)
	return time.FixedZone(name, offsetSecs), nil
}

// matchesCron checks if a 5-field cron (min hour dom month dow) matches the given time.
func matchesCron(expr string, t time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}

	checks := []struct {
		field string
		value int
		max   int
	}{
		{fields[0], t.Minute(), 59},
		{fields[1], t.Hour(), 23},
		{fields[2], t.Day(), 31},
		{fields[3], int(t.Month()), 12},
		{fields[4], int(t.Weekday()), 6}, // 0=Sunday
	}

	for _, c := range checks {
		if !cronFieldMatches(c.field, c.value, c.max) {
			return false
		}
	}
	return true
}

// cronFieldMatches checks a single cron field against a value.
// Supports: *, N, N-M, */N, N,M,...
func cronFieldMatches(field string, value, max int) bool {
	if field == "*" {
		return true
	}

	// Handle comma-separated values.
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)

		// */N step.
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
			if err != nil || step == 0 {
				continue
			}
			if value%step == 0 {
				return true
			}
			continue
		}

		// N-M range.
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			lo, err1 := strconv.Atoi(rangeParts[0])
			hi, err2 := strconv.Atoi(rangeParts[1])
			if err1 != nil || err2 != nil {
				continue
			}
			if value >= lo && value <= hi {
				return true
			}
			continue
		}

		// Single value.
		n, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		if n == value {
			return true
		}
	}

	return false
}

// IsValidCronExpression validates a 5-field cron expression with optional TZ prefix.
func IsValidCronExpression(expr string) bool {
	_, cronExpr := parseTZPrefix(expr)
	fields := strings.Fields(cronExpr)
	if len(fields) != 5 {
		return false
	}
	// Basic validation — check each field is parseable.
	for _, f := range fields {
		if f == "*" {
			continue
		}
		for _, part := range strings.Split(f, ",") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "*/") {
				if _, err := strconv.Atoi(strings.TrimPrefix(part, "*/")); err != nil {
					return false
				}
				continue
			}
			if strings.Contains(part, "-") {
				rp := strings.SplitN(part, "-", 2)
				if _, err := strconv.Atoi(rp[0]); err != nil {
					return false
				}
				if _, err := strconv.Atoi(rp[1]); err != nil {
					return false
				}
				continue
			}
			if _, err := strconv.Atoi(part); err != nil {
				return false
			}
		}
	}
	return true
}

// CronRunStatus tracks execution outcome.
type CronRunStatus string

const (
	CronRunSuccess CronRunStatus = "success"
	CronRunFailed  CronRunStatus = "failed"
)

// CronRun records a single execution of a cron job.
type CronRun struct {
	JobID      string        `json:"job_id"`
	Status     CronRunStatus `json:"status"`
	DurationMs int64         `json:"duration_ms"`
	ErrorMsg   string        `json:"error_msg,omitempty"`
	Timestamp  time.Time     `json:"timestamp"`
}

// --- Lease-based distributed locking ---

// CronLease represents a lock on a cron job for a specific instance.
type CronLease struct {
	JobID      string    `json:"job_id"`
	InstanceID string    `json:"instance_id"`
	AcquiredAt time.Time `json:"acquired_at"`
}

// LeaseError indicates a lease acquisition failure.
type LeaseError struct {
	JobID  string
	Holder string
}

func (e *LeaseError) Error() string {
	return fmt.Sprintf("lease held by %s for job %s", e.Holder, e.JobID)
}
