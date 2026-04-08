package wallet

import (
	"math"
	"testing"
)

func TestNewUSDCMoney(t *testing.T) {
	m := NewUSDCMoney(1.5)
	if m.Units() != 1_500_000 {
		t.Errorf("NewUSDCMoney(1.5).Units() = %d, want 1500000", m.Units())
	}
}

func TestUSDCMoney_USD(t *testing.T) {
	m := NewUSDCMoney(42.123456)
	got := m.USD()
	if math.Abs(got-42.123456) > 0.000001 {
		t.Errorf("USD() = %f, want ~42.123456", got)
	}
}

func TestUSDCMoney_Add(t *testing.T) {
	a := NewUSDCMoney(1.0)
	b := NewUSDCMoney(2.5)
	sum := a.Add(b)
	if sum.Units() != 3_500_000 {
		t.Errorf("1.0 + 2.5 = %d units, want 3500000", sum.Units())
	}
}

func TestUSDCMoney_Sub(t *testing.T) {
	a := NewUSDCMoney(5.0)
	b := NewUSDCMoney(2.0)
	diff := a.Sub(b)
	if diff.Units() != 3_000_000 {
		t.Errorf("5.0 - 2.0 = %d units, want 3000000", diff.Units())
	}
}

func TestUSDCMoney_LessThan(t *testing.T) {
	a := NewUSDCMoney(1.0)
	b := NewUSDCMoney(2.0)
	if !a.LessThan(b) {
		t.Error("1.0 should be less than 2.0")
	}
	if b.LessThan(a) {
		t.Error("2.0 should not be less than 1.0")
	}
}

func TestUSDCMoney_IsZero(t *testing.T) {
	zero := NewUSDCMoney(0)
	if !zero.IsZero() {
		t.Error("0 should be zero")
	}
	nonZero := NewUSDCMoney(0.01)
	if nonZero.IsZero() {
		t.Error("0.01 should not be zero")
	}
}

func TestUSDCMoney_String(t *testing.T) {
	m := NewUSDCMoney(3.14)
	s := m.String()
	if s != "3.140000 USDC" {
		t.Errorf("String() = %q, want %q", s, "3.140000 USDC")
	}
	neg := NewUSDCMoney(-1.5)
	ns := neg.String()
	if ns != "-1.500000 USDC" {
		t.Errorf("String() = %q, want %q", ns, "-1.500000 USDC")
	}
}

func TestUSDCMoneyFromUnits(t *testing.T) {
	m := USDCMoneyFromUnits(500_000)
	if m.USD() != 0.5 {
		t.Errorf("USDCMoneyFromUnits(500000).USD() = %f, want 0.5", m.USD())
	}
}

// TestUSDCMoney_BackwardCompatible ensures the existing Money type still works.
func TestMoney_BackwardCompatible(t *testing.T) {
	m := FromDollars(10.50)
	if m.Cents() != 1050 {
		t.Errorf("FromDollars(10.50).Cents() = %d, want 1050", m.Cents())
	}
}
