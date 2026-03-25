package wallet

import (
	"testing"
)

func TestWallet_Generate(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if w.Address() == "" {
		t.Fatal("address should not be empty")
	}
	if w.Address()[:2] != "0x" {
		t.Error("address should start with 0x")
	}
	if len(w.Address()) != 42 { // 0x + 40 hex chars
		t.Errorf("address length should be 42, got %d", len(w.Address()))
	}
}

func TestMoney(t *testing.T) {
	m := FromDollars(10.50)
	if m.Cents() != 1050 {
		t.Errorf("expected 1050 cents, got %d", m.Cents())
	}
	if m.Dollars() != 10.50 {
		t.Errorf("expected $10.50, got $%.2f", m.Dollars())
	}
	if m.String() != "$10.50" {
		t.Errorf("expected '$10.50', got %q", m.String())
	}

	sum := m.Add(FromDollars(5.25))
	if sum.Cents() != 1575 {
		t.Errorf("expected 1575 cents, got %d", sum.Cents())
	}

	diff := m.Sub(FromDollars(3.00))
	if diff.Cents() != 750 {
		t.Errorf("expected 750 cents, got %d", diff.Cents())
	}
}

func TestTreasuryPolicy(t *testing.T) {
	p := NewTreasuryPolicy(TreasuryConfig{
		PerPaymentCap:       100.0,
		HourlyTransferLimit: 500.0,
		DailyTransferLimit:  1000.0,
		MinimumReserve:      50.0,
	})

	// Within cap.
	if err := p.CheckPerPayment(50.0); err != nil {
		t.Errorf("should allow: %v", err)
	}

	// Exceeds cap.
	if err := p.CheckPerPayment(150.0); err == nil {
		t.Error("should reject payment above cap")
	}

	// Hourly limit.
	if err := p.CheckHourlyLimit(400.0, 150.0); err == nil {
		t.Error("should reject hourly limit breach")
	}

	// Reserve check.
	if err := p.CheckMinimumReserve(30.0); err == nil {
		t.Error("should reject below minimum reserve")
	}
	if err := p.CheckMinimumReserve(100.0); err != nil {
		t.Errorf("should allow above reserve: %v", err)
	}
}

func TestYieldEngine(t *testing.T) {
	y := NewYieldEngine(YieldConfig{
		Enabled:             true,
		MinDeposit:          10.0,
		WithdrawalThreshold: 20.0,
	}, nil)

	excess := y.CalculateExcess(150.0, 100.0) // 150 - 100 - 10 buffer = 40
	if excess != 40.0 {
		t.Errorf("expected excess 40.0, got %.2f", excess)
	}

	if !y.ShouldDeposit(40.0) {
		t.Error("should deposit when excess > min_deposit")
	}
	if y.ShouldDeposit(5.0) {
		t.Error("should not deposit when excess < min_deposit")
	}

	if !y.ShouldWithdraw(15.0) {
		t.Error("should withdraw when balance < threshold")
	}
	if y.ShouldWithdraw(25.0) {
		t.Error("should not withdraw when balance > threshold")
	}
}
