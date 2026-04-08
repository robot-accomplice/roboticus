package wallet

import "fmt"

// TreasuryConfig holds spending limit parameters.
type TreasuryConfig struct {
	PerPaymentCap        float64 `mapstructure:"per_payment_cap"`        // single payment max ($)
	HourlyTransferLimit  float64 `mapstructure:"hourly_transfer_limit"`  // hourly budget ($)
	DailyTransferLimit   float64 `mapstructure:"daily_transfer_limit"`   // daily budget ($)
	MinimumReserve       float64 `mapstructure:"minimum_reserve"`        // balance floor ($)
	DailyInferenceBudget float64 `mapstructure:"daily_inference_budget"` // LLM spend cap ($)
}

// TreasuryPolicy enforces spending limits.
type TreasuryPolicy struct {
	cfg TreasuryConfig
}

// ValidateTreasuryConfig checks that no limit is negative.
func ValidateTreasuryConfig(cfg TreasuryConfig) error {
	if cfg.PerPaymentCap < 0 {
		return fmt.Errorf("treasury: per_payment_cap must not be negative (got %.2f)", cfg.PerPaymentCap)
	}
	if cfg.HourlyTransferLimit < 0 {
		return fmt.Errorf("treasury: hourly_transfer_limit must not be negative (got %.2f)", cfg.HourlyTransferLimit)
	}
	if cfg.DailyTransferLimit < 0 {
		return fmt.Errorf("treasury: daily_transfer_limit must not be negative (got %.2f)", cfg.DailyTransferLimit)
	}
	if cfg.MinimumReserve < 0 {
		return fmt.Errorf("treasury: minimum_reserve must not be negative (got %.2f)", cfg.MinimumReserve)
	}
	if cfg.DailyInferenceBudget < 0 {
		return fmt.Errorf("treasury: daily_inference_budget must not be negative (got %.2f)", cfg.DailyInferenceBudget)
	}
	return nil
}

// NewTreasuryPolicy creates a treasury policy from config.
func NewTreasuryPolicy(cfg TreasuryConfig) *TreasuryPolicy {
	return &TreasuryPolicy{cfg: cfg}
}

// CheckPerPayment validates a single payment amount.
func (p *TreasuryPolicy) CheckPerPayment(amount float64) error {
	if p.cfg.PerPaymentCap > 0 && amount > p.cfg.PerPaymentCap {
		return fmt.Errorf("treasury: payment $%.2f exceeds cap $%.2f", amount, p.cfg.PerPaymentCap)
	}
	return nil
}

// CheckHourlyLimit validates against the hourly transfer budget.
func (p *TreasuryPolicy) CheckHourlyLimit(recentTotal, newAmount float64) error {
	if p.cfg.HourlyTransferLimit > 0 && recentTotal+newAmount > p.cfg.HourlyTransferLimit {
		return fmt.Errorf("treasury: hourly limit $%.2f would be exceeded", p.cfg.HourlyTransferLimit)
	}
	return nil
}

// CheckDailyLimit validates against the daily transfer budget.
func (p *TreasuryPolicy) CheckDailyLimit(recentTotal, newAmount float64) error {
	if p.cfg.DailyTransferLimit > 0 && recentTotal+newAmount > p.cfg.DailyTransferLimit {
		return fmt.Errorf("treasury: daily limit $%.2f would be exceeded", p.cfg.DailyTransferLimit)
	}
	return nil
}

// CheckMinimumReserve validates that the balance stays above the reserve.
func (p *TreasuryPolicy) CheckMinimumReserve(balance float64) error {
	if p.cfg.MinimumReserve > 0 && balance < p.cfg.MinimumReserve {
		return fmt.Errorf("treasury: balance $%.2f below reserve $%.2f", balance, p.cfg.MinimumReserve)
	}
	return nil
}

// CheckInferenceBudget validates that daily LLM inference spend is within budget.
// dailySpend is the total spend so far today in dollars.
func (p *TreasuryPolicy) CheckInferenceBudget(dailySpend float64) error {
	if p.cfg.DailyInferenceBudget > 0 && dailySpend > p.cfg.DailyInferenceBudget {
		return fmt.Errorf("treasury: daily inference spend $%.2f exceeds budget $%.2f", dailySpend, p.cfg.DailyInferenceBudget)
	}
	return nil
}

// Config returns the treasury configuration.
func (p *TreasuryPolicy) Config() TreasuryConfig { return p.cfg }
