package wallet

import "testing"

func TestTreasuryPolicy_CheckPerPayment(t *testing.T) {
	p := NewTreasuryPolicy(TreasuryConfig{PerPaymentCap: 50.0})
	if err := p.CheckPerPayment(49.0); err != nil {
		t.Errorf("49 should be under 50 cap: %v", err)
	}
	if err := p.CheckPerPayment(51.0); err == nil {
		t.Error("51 should exceed 50 cap")
	}
}

func TestTreasuryPolicy_CheckHourlyLimit(t *testing.T) {
	p := NewTreasuryPolicy(TreasuryConfig{HourlyTransferLimit: 100.0})
	if err := p.CheckHourlyLimit(50.0, 25.0); err != nil {
		t.Errorf("50+25=75 should be under 100: %v", err)
	}
	if err := p.CheckHourlyLimit(80.0, 25.0); err == nil {
		t.Error("80+25=105 should exceed 100")
	}
}

func TestTreasuryPolicy_CheckDailyLimit(t *testing.T) {
	p := NewTreasuryPolicy(TreasuryConfig{DailyTransferLimit: 500.0})
	if err := p.CheckDailyLimit(400.0, 50.0); err != nil {
		t.Errorf("400+50=450 should be under 500: %v", err)
	}
	if err := p.CheckDailyLimit(480.0, 30.0); err == nil {
		t.Error("480+30=510 should exceed 500")
	}
}

func TestTreasuryPolicy_CheckMinimumReserve(t *testing.T) {
	p := NewTreasuryPolicy(TreasuryConfig{MinimumReserve: 10.0})
	if err := p.CheckMinimumReserve(100.0); err != nil {
		t.Errorf("100 should maintain 10 reserve: %v", err)
	}
	if err := p.CheckMinimumReserve(5.0); err == nil {
		t.Error("5 should violate 10 reserve")
	}
}

func TestTreasuryPolicy_Config(t *testing.T) {
	cfg := TreasuryConfig{
		PerPaymentCap:       25.0,
		HourlyTransferLimit: 100.0,
		DailyTransferLimit:  500.0,
		MinimumReserve:      10.0,
	}
	p := NewTreasuryPolicy(cfg)
	got := p.Config()
	if got.PerPaymentCap != 25.0 {
		t.Errorf("cap = %f", got.PerPaymentCap)
	}
}
