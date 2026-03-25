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

// Config returns the treasury configuration.
func (p *TreasuryPolicy) Config() TreasuryConfig { return p.cfg }
