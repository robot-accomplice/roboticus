package core

import "context"

// ScheduleKind identifies the type of schedule for cron jobs.
type ScheduleKind int

const (
	// ScheduleKindCron uses a standard cron expression (e.g., "0 9 * * *").
	ScheduleKindCron ScheduleKind = iota
	// ScheduleKindEvery uses a fixed interval (e.g., every 300 seconds).
	ScheduleKindEvery
	// ScheduleKindAt fires once at a specific time.
	ScheduleKindAt
)

func (k ScheduleKind) String() string {
	switch k {
	case ScheduleKindCron:
		return "cron"
	case ScheduleKindEvery:
		return "every"
	case ScheduleKindAt:
		return "at"
	default:
		return "unknown"
	}
}

// PaymentHandler handles HTTP 402 Payment Required responses (x402 protocol).
//
// Implementors receive the JSON response body from a 402 response and must
// return a payment header string that will be attached to the retry request.
//
// This interface lives in core so that the LLM client can accept a handler
// without importing the wallet package directly.
type PaymentHandler interface {
	HandlePaymentRequired(ctx context.Context, responseBody []byte) (string, error)
}
