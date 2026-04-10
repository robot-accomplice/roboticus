package db

import (
	"context"
	"database/sql"
	"strings"
)

// RevenueOpportunityRow represents a row in revenue_opportunities.
type RevenueOpportunityRow struct {
	ID                    string
	Source                string
	Strategy              string
	PayloadJSON           string
	ExpectedRevenueUSDC   float64
	Status                string
	QualificationReason   string
	ConfidenceScore       float64
	EffortScore           float64
	RiskScore             float64
	PriorityScore         float64
	RecommendedApproved   int
	ScoreReason           string
	PlanJSON              string
	EvidenceJSON          string
	RequestID             string
	SettlementRef         string
	SettledAmountUSDC     float64
	AttributableCostsUSDC float64
	NetProfitUSDC         float64
	TaxRate               float64
	TaxAmountUSDC         float64
	RetainedEarningsUSDC  float64
	TaxDestinationWallet  string
	SettledAt             string
	CreatedAt             string
	UpdatedAt             string
}

// RevenueFeedbackRow represents a row in revenue_feedback.
type RevenueFeedbackRow struct {
	ID            string
	OpportunityID string
	Strategy      string
	Grade         float64
	Source        string
	Comment       string
	CreatedAt     string
}

// RevenueOpportunityFilter controls which opportunities to list.
type RevenueOpportunityFilter struct {
	Status   string
	Strategy string
	Limit    int
}

// StrategyAggregate summarizes opportunities grouped by strategy.
type StrategyAggregate struct {
	Strategy             string
	Count                int
	TotalExpectedRevenue float64
}

// StatusCount summarizes opportunity counts by status.
type StatusCount struct {
	Status string
	Count  int
}

// RevenueRepository handles revenue opportunity and feedback persistence.
// All queries go through the Querier interface (centralized connection pool).
type RevenueRepository struct {
	q Querier
}

// NewRevenueRepository creates a revenue repository.
func NewRevenueRepository(q Querier) *RevenueRepository {
	return &RevenueRepository{q: q}
}

// CreateOpportunity inserts a new revenue opportunity.
func (r *RevenueRepository) CreateOpportunity(ctx context.Context, row RevenueOpportunityRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO revenue_opportunities (
			id, source, strategy, payload_json, expected_revenue_usdc, status,
			qualification_reason, confidence_score, effort_score, risk_score,
			priority_score, recommended_approved, score_reason, plan_json,
			evidence_json, request_id, settlement_ref, settled_amount_usdc,
			attributable_costs_usdc, net_profit_usdc, tax_rate, tax_amount_usdc,
			retained_earnings_usdc, tax_destination_wallet, settled_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.Source, row.Strategy, row.PayloadJSON, row.ExpectedRevenueUSDC, row.Status,
		row.QualificationReason, row.ConfidenceScore, row.EffortScore, row.RiskScore,
		row.PriorityScore, row.RecommendedApproved, row.ScoreReason, row.PlanJSON,
		row.EvidenceJSON, row.RequestID, nullString(row.SettlementRef), row.SettledAmountUSDC,
		row.AttributableCostsUSDC, row.NetProfitUSDC, row.TaxRate, row.TaxAmountUSDC,
		row.RetainedEarningsUSDC, row.TaxDestinationWallet, nullString(row.SettledAt),
	)
	return err
}

// UpdateOpportunityStatus updates the status of an opportunity.
func (r *RevenueRepository) UpdateOpportunityStatus(ctx context.Context, id, status string) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE revenue_opportunities SET status = ?, updated_at = datetime('now') WHERE id = ?`,
		status, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoRowsAffected
	}
	return nil
}

// GetOpportunity retrieves a single opportunity by ID.
func (r *RevenueRepository) GetOpportunity(ctx context.Context, id string) (*RevenueOpportunityRow, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT id, source, strategy, payload_json, expected_revenue_usdc, status,
		        qualification_reason, confidence_score, effort_score, risk_score,
		        priority_score, recommended_approved, score_reason, plan_json,
		        evidence_json, request_id, settlement_ref, settled_amount_usdc,
		        attributable_costs_usdc, net_profit_usdc, tax_rate, tax_amount_usdc,
		        retained_earnings_usdc, tax_destination_wallet, settled_at,
		        created_at, updated_at
		 FROM revenue_opportunities WHERE id = ?`,
		id,
	)
	return scanOpportunity(row)
}

// ListOpportunities returns opportunities matching the filter.
func (r *RevenueRepository) ListOpportunities(ctx context.Context, filter RevenueOpportunityFilter) ([]RevenueOpportunityRow, error) {
	query := `SELECT id, source, strategy, payload_json, expected_revenue_usdc, status,
	                 qualification_reason, confidence_score, effort_score, risk_score,
	                 priority_score, recommended_approved, score_reason, plan_json,
	                 evidence_json, request_id, settlement_ref, settled_amount_usdc,
	                 attributable_costs_usdc, net_profit_usdc, tax_rate, tax_amount_usdc,
	                 retained_earnings_usdc, tax_destination_wallet, settled_at,
	                 created_at, updated_at
	          FROM revenue_opportunities`

	var args []any
	var where []string

	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.Strategy != "" {
		where = append(where, "strategy = ?")
		args = append(args, filter.Strategy)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []RevenueOpportunityRow
	for rows.Next() {
		row, err := scanOpportunityRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// CreateFeedback inserts a new revenue feedback entry.
func (r *RevenueRepository) CreateFeedback(ctx context.Context, row RevenueFeedbackRow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO revenue_feedback (id, opportunity_id, strategy, grade, source, comment)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		row.ID, row.OpportunityID, row.Strategy, row.Grade, row.Source, row.Comment,
	)
	return err
}

// ListFeedbackByOpportunity returns all feedback for a given opportunity.
func (r *RevenueRepository) ListFeedbackByOpportunity(ctx context.Context, opportunityID string) ([]RevenueFeedbackRow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, opportunity_id, strategy, grade, source, comment, created_at
		 FROM revenue_feedback WHERE opportunity_id = ? ORDER BY created_at DESC`,
		opportunityID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []RevenueFeedbackRow
	for rows.Next() {
		var fb RevenueFeedbackRow
		var comment sql.NullString
		if err := rows.Scan(&fb.ID, &fb.OpportunityID, &fb.Strategy, &fb.Grade, &fb.Source, &comment, &fb.CreatedAt); err != nil {
			return nil, err
		}
		if comment.Valid {
			fb.Comment = comment.String
		}
		result = append(result, fb)
	}
	return result, rows.Err()
}

// AggregateByStrategy returns count and total expected revenue grouped by strategy.
func (r *RevenueRepository) AggregateByStrategy(ctx context.Context) ([]StrategyAggregate, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT strategy, COUNT(*) AS cnt, SUM(expected_revenue_usdc) AS total
		 FROM revenue_opportunities GROUP BY strategy ORDER BY total DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []StrategyAggregate
	for rows.Next() {
		var agg StrategyAggregate
		if err := rows.Scan(&agg.Strategy, &agg.Count, &agg.TotalExpectedRevenue); err != nil {
			return nil, err
		}
		result = append(result, agg)
	}
	return result, rows.Err()
}

// CountByStatus returns the count of opportunities grouped by status.
func (r *RevenueRepository) CountByStatus(ctx context.Context) ([]StatusCount, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT status, COUNT(*) AS cnt FROM revenue_opportunities GROUP BY status ORDER BY cnt DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var result []StatusCount
	for rows.Next() {
		var sc StatusCount
		if err := rows.Scan(&sc.Status, &sc.Count); err != nil {
			return nil, err
		}
		result = append(result, sc)
	}
	return result, rows.Err()
}

// ── Rust-parity lifecycle methods ─────────────────────────────────────────
// These provide semantic lifecycle transitions matching Rust's
// qualify(), plan(), mark_fulfilled() methods. They enforce domain
// invariants (required scores, plan JSON, settlement data) that the
// generic UpdateOpportunityStatus does not.

// QualificationInput carries the data needed to qualify an opportunity.
type QualificationInput struct {
	ID                  string
	Source              string
	Strategy            string
	PayloadJSON         string
	ExpectedRevenueUSDC float64
	QualificationReason string
	ConfidenceScore     float64
	EffortScore         float64
	RiskScore           float64
	PriorityScore       float64
	ScoreReason         string
	RequestID           string
}

// Qualify creates a new opportunity in "qualified" status with scoring data.
// Rust parity: crates/roboticus-revenue/src/repo.rs qualify()
func (r *RevenueRepository) Qualify(ctx context.Context, input QualificationInput) error {
	return r.CreateOpportunity(ctx, RevenueOpportunityRow{
		ID:                  input.ID,
		Source:              input.Source,
		Strategy:            input.Strategy,
		PayloadJSON:         input.PayloadJSON,
		ExpectedRevenueUSDC: input.ExpectedRevenueUSDC,
		Status:              "qualified",
		QualificationReason: input.QualificationReason,
		ConfidenceScore:     input.ConfidenceScore,
		EffortScore:         input.EffortScore,
		RiskScore:           input.RiskScore,
		PriorityScore:       input.PriorityScore,
		ScoreReason:         input.ScoreReason,
		RequestID:           input.RequestID,
	})
}

// Plan attaches a plan to a qualified opportunity, transitioning to "planned".
// Rust parity: crates/roboticus-revenue/src/repo.rs plan()
func (r *RevenueRepository) Plan(ctx context.Context, id, planJSON string) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE revenue_opportunities
		 SET status = 'planned', plan_json = ?, updated_at = datetime('now')
		 WHERE id = ? AND status = 'qualified'`,
		planJSON, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoRowsAffected
	}
	return nil
}

// SettlementInput carries the data needed to mark an opportunity fulfilled.
type SettlementInput struct {
	SettlementRef         string
	SettledAmountUSDC     float64
	AttributableCostsUSDC float64
	NetProfitUSDC         float64
	TaxRate               float64
	TaxAmountUSDC         float64
	RetainedEarningsUSDC  float64
	TaxDestinationWallet  string
	EvidenceJSON          string
}

// MarkFulfilled transitions an opportunity to "fulfilled" with settlement data.
// Rust parity: crates/roboticus-revenue/src/repo.rs mark_fulfilled()
func (r *RevenueRepository) MarkFulfilled(ctx context.Context, id string, settlement SettlementInput) error {
	res, err := r.q.ExecContext(ctx,
		`UPDATE revenue_opportunities
		 SET status = 'fulfilled',
		     settlement_ref = ?, settled_amount_usdc = ?,
		     attributable_costs_usdc = ?, net_profit_usdc = ?,
		     tax_rate = ?, tax_amount_usdc = ?, retained_earnings_usdc = ?,
		     tax_destination_wallet = ?, evidence_json = ?,
		     settled_at = datetime('now'), updated_at = datetime('now')
		 WHERE id = ? AND status IN ('planned', 'executing')`,
		settlement.SettlementRef, settlement.SettledAmountUSDC,
		settlement.AttributableCostsUSDC, settlement.NetProfitUSDC,
		settlement.TaxRate, settlement.TaxAmountUSDC, settlement.RetainedEarningsUSDC,
		settlement.TaxDestinationWallet, settlement.EvidenceJSON,
		id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNoRowsAffected
	}
	return nil
}

// QualifyRevenueOpportunity bridges a service request into a revenue opportunity.
// Reads the service request, creates a qualified opportunity linked via request_id.
// Rust parity: crates/roboticus-revenue/src/service.rs qualify_revenue_opportunity()
func (r *RevenueRepository) QualifyRevenueOpportunity(ctx context.Context, svcRepo *ServiceRequestsRepository, serviceRequestID string, strategy string, confidence, effort, risk float64, reason string) error {
	// Verify the service request exists.
	svcReq, err := svcRepo.Get(ctx, serviceRequestID)
	if err != nil {
		return err
	}

	expectedRevenue, _ := svcReq["quoted_amount"].(float64)
	priority := confidence * (1.0 - effort) * (1.0 - risk)

	return r.Qualify(ctx, QualificationInput{
		ID:                  NewID(),
		Source:              "service_request",
		Strategy:            strategy,
		ExpectedRevenueUSDC: expectedRevenue,
		QualificationReason: reason,
		ConfidenceScore:     confidence,
		EffortScore:         effort,
		RiskScore:           risk,
		PriorityScore:       priority,
		ScoreReason:         "auto-qualified from service request",
		RequestID:           serviceRequestID,
	})
}

// nullString returns a sql.NullString for empty strings.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// scanOpportunity scans a single row from QueryRowContext.
func scanOpportunity(row *sql.Row) (*RevenueOpportunityRow, error) {
	var r RevenueOpportunityRow
	var qualReason, scoreReason, planJSON, evidenceJSON, requestID sql.NullString
	var settlementRef, settledAt, taxDest sql.NullString
	err := row.Scan(
		&r.ID, &r.Source, &r.Strategy, &r.PayloadJSON, &r.ExpectedRevenueUSDC, &r.Status,
		&qualReason, &r.ConfidenceScore, &r.EffortScore, &r.RiskScore,
		&r.PriorityScore, &r.RecommendedApproved, &scoreReason, &planJSON,
		&evidenceJSON, &requestID, &settlementRef, &r.SettledAmountUSDC,
		&r.AttributableCostsUSDC, &r.NetProfitUSDC, &r.TaxRate, &r.TaxAmountUSDC,
		&r.RetainedEarningsUSDC, &taxDest, &settledAt,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.QualificationReason = qualReason.String
	r.ScoreReason = scoreReason.String
	r.PlanJSON = planJSON.String
	r.EvidenceJSON = evidenceJSON.String
	r.RequestID = requestID.String
	r.SettlementRef = settlementRef.String
	r.SettledAt = settledAt.String
	r.TaxDestinationWallet = taxDest.String
	return &r, nil
}

// scannerRow is an interface for rows.Scan (used to share scan logic).
type scannerRow interface {
	Scan(dest ...any) error
}

// scanOpportunityRow scans a row from QueryContext (rows.Next loop).
func scanOpportunityRow(rows scannerRow) (RevenueOpportunityRow, error) {
	var r RevenueOpportunityRow
	var qualReason, scoreReason, planJSON, evidenceJSON, requestID sql.NullString
	var settlementRef, settledAt, taxDest sql.NullString
	err := rows.Scan(
		&r.ID, &r.Source, &r.Strategy, &r.PayloadJSON, &r.ExpectedRevenueUSDC, &r.Status,
		&qualReason, &r.ConfidenceScore, &r.EffortScore, &r.RiskScore,
		&r.PriorityScore, &r.RecommendedApproved, &scoreReason, &planJSON,
		&evidenceJSON, &requestID, &settlementRef, &r.SettledAmountUSDC,
		&r.AttributableCostsUSDC, &r.NetProfitUSDC, &r.TaxRate, &r.TaxAmountUSDC,
		&r.RetainedEarningsUSDC, &taxDest, &settledAt,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return r, err
	}
	r.QualificationReason = qualReason.String
	r.ScoreReason = scoreReason.String
	r.PlanJSON = planJSON.String
	r.EvidenceJSON = evidenceJSON.String
	r.RequestID = requestID.String
	r.SettlementRef = settlementRef.String
	r.SettledAt = settledAt.String
	r.TaxDestinationWallet = taxDest.String
	return r, nil
}
