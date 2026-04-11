package wallet

import (
	"math"
	"testing"
)

// --- Rust parity tests (matching money.rs test suite) ---

func TestMoneyFromDollars_Roundtrip(t *testing.T) {
	// Rust: from_dollars_roundtrip
	tests := []struct {
		dollars float64
		cents   int64
	}{
		{0.0, 0},
		{1.0, 100},
		{10.50, 1050},
		{99.99, 9999},
	}
	for _, tc := range tests {
		m, err := MoneyFromDollars(tc.dollars)
		if err != nil {
			t.Fatalf("MoneyFromDollars(%f): %v", tc.dollars, err)
		}
		if m.Cents() != tc.cents {
			t.Errorf("MoneyFromDollars(%f).Cents() = %d, want %d", tc.dollars, m.Cents(), tc.cents)
		}
	}
	// Dollars() round-trip.
	m, _ := MoneyFromDollars(99.99)
	if m.Dollars() != 99.99 {
		t.Errorf("Dollars() = %f, want 99.99", m.Dollars())
	}
	m2, _ := MoneyFromDollars(33.33)
	if math.Abs(m2.Dollars()-33.33) > 0.001 {
		t.Errorf("Dollars() = %f, want ~33.33", m2.Dollars())
	}
}

func TestMoney_DisplayFormat(t *testing.T) {
	// Rust: display_format
	tests := []struct {
		dollars float64
		display string
	}{
		{0.0, "$0.00"},
		{1.5, "$1.50"},
		{100.0, "$100.00"},
	}
	for _, tc := range tests {
		m, _ := MoneyFromDollars(tc.dollars)
		if s := m.String(); s != tc.display {
			t.Errorf("MoneyFromDollars(%f).String() = %q, want %q", tc.dollars, s, tc.display)
		}
	}
}

func TestMoney_Arithmetic(t *testing.T) {
	// Rust: arithmetic
	a, _ := MoneyFromDollars(10.00)
	b, _ := MoneyFromDollars(5.50)
	if (a.Add(b)).Dollars() != 15.50 {
		t.Errorf("10 + 5.50 = %f, want 15.50", a.Add(b).Dollars())
	}
	if (a.Sub(b)).Dollars() != 4.50 {
		t.Errorf("10 - 5.50 = %f, want 4.50", a.Sub(b).Dollars())
	}
	if MoneyZero().Add(a).Cents() != a.Cents() {
		t.Error("zero + a should equal a")
	}
	if !(a.Sub(a).IsZero()) {
		t.Error("a - a should be zero")
	}
}

func TestMoney_SaturatingArithmetic(t *testing.T) {
	// Rust: saturating_arithmetic
	maxCents := MoneyFromCents(math.MaxInt64)
	one := MoneyFromCents(1)
	if maxCents.Add(one).Cents() != math.MaxInt64 {
		t.Errorf("MAX + 1 should saturate at MAX, got %d", maxCents.Add(one).Cents())
	}
	minCents := MoneyFromCents(math.MinInt64)
	if minCents.Sub(one).Cents() != math.MinInt64 {
		t.Errorf("MIN - 1 should saturate at MIN, got %d", minCents.Sub(one).Cents())
	}
}

func TestMoney_CheckedArithmetic(t *testing.T) {
	// Rust: checked_arithmetic
	a, _ := MoneyFromDollars(10.00)
	b, _ := MoneyFromDollars(5.50)
	sum, ok := a.CheckedAdd(b)
	if !ok || sum.Dollars() != 15.50 {
		t.Errorf("checked_add: ok=%v, dollars=%f", ok, sum.Dollars())
	}
	diff, ok := a.CheckedSub(b)
	if !ok || diff.Dollars() != 4.50 {
		t.Errorf("checked_sub: ok=%v, dollars=%f", ok, diff.Dollars())
	}
	_, ok = MoneyFromCents(math.MaxInt64).CheckedAdd(MoneyFromCents(1))
	if ok {
		t.Error("MAX + 1 should return !ok")
	}
	_, ok = MoneyFromCents(math.MinInt64).CheckedSub(MoneyFromCents(1))
	if ok {
		t.Error("MIN - 1 should return !ok")
	}
}

func TestMoney_RejectsNonFinite(t *testing.T) {
	// Rust: from_dollars_rejects_nan, infinity, extreme values
	badValues := []float64{math.NaN(), math.Inf(1), math.Inf(-1), math.MaxFloat64, -math.MaxFloat64}
	for _, v := range badValues {
		_, err := MoneyFromDollars(v)
		if err == nil {
			t.Errorf("MoneyFromDollars(%v) should return error", v)
		}
	}
}

func TestMoney_LessThan(t *testing.T) {
	a, _ := MoneyFromDollars(1.0)
	b, _ := MoneyFromDollars(2.0)
	if !a.LessThan(b) {
		t.Error("1.0 should be less than 2.0")
	}
	if b.LessThan(a) {
		t.Error("2.0 should not be less than 1.0")
	}
}

func TestMoney_IsZero(t *testing.T) {
	if !MoneyZero().IsZero() {
		t.Error("zero should be zero")
	}
	m, _ := MoneyFromDollars(0.01)
	if m.IsZero() {
		t.Error("0.01 should not be zero")
	}
}

// --- Backward compatibility ---

func TestFromDollars_Convenience(t *testing.T) {
	m := FromDollars(10.50)
	if m.Cents() != 1050 {
		t.Errorf("FromDollars(10.50).Cents() = %d, want 1050", m.Cents())
	}
}
