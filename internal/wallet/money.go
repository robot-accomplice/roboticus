package wallet

import (
	"fmt"
	"math"
)

// Money represents a fixed-point dollar amount in cents.
// Rust parity: Money(i64) where 100 = $1.00.
// Arithmetic uses saturating add/sub to prevent overflow wrapping.
type Money struct {
	cents int64
}

// MoneyFromDollars converts a USD float64 to Money.
// Rust parity: Money::from_dollars(f64) -> Result<Money>.
func MoneyFromDollars(dollars float64) (Money, error) {
	if math.IsNaN(dollars) || math.IsInf(dollars, 0) {
		return Money{}, fmt.Errorf("non-finite dollar amount: %v", dollars)
	}
	c := math.Round(dollars * 100.0)
	if c < float64(math.MinInt64) || c > float64(math.MaxInt64) {
		return Money{}, fmt.Errorf("dollar amount out of representable range: %v", dollars)
	}
	return Money{cents: int64(c)}, nil
}

// MoneyFromCents creates a Money from raw cent units.
func MoneyFromCents(cents int64) Money {
	return Money{cents: cents}
}

// MoneyZero returns a zero Money value.
// Rust parity: Money::zero().
func MoneyZero() Money {
	return Money{cents: 0}
}

// Dollars returns the amount as a float64 dollar value.
// Rust parity: Money::dollars(&self) -> f64.
func (m Money) Dollars() float64 {
	return float64(m.cents) / 100.0
}

// Cents returns the raw cent units.
// Rust parity: Money::cents(&self) -> i64.
func (m Money) Cents() int64 {
	return m.cents
}

// Add returns the sum of two Money values (saturating).
// Rust parity: impl Add for Money { Money(self.0.saturating_add(rhs.0)) }.
func (m Money) Add(other Money) Money {
	result, overflow := addInt64(m.cents, other.cents)
	if overflow {
		if other.cents > 0 {
			return Money{cents: math.MaxInt64}
		}
		return Money{cents: math.MinInt64}
	}
	return Money{cents: result}
}

// Sub returns the difference of two Money values (saturating).
// Rust parity: impl Sub for Money { Money(self.0.saturating_sub(rhs.0)) }.
func (m Money) Sub(other Money) Money {
	result, overflow := subInt64(m.cents, other.cents)
	if overflow {
		if other.cents > 0 {
			return Money{cents: math.MinInt64}
		}
		return Money{cents: math.MaxInt64}
	}
	return Money{cents: result}
}

// CheckedAdd returns the sum, or false if overflow.
// Rust parity: Money::checked_add(self, rhs) -> Option<Money>.
func (m Money) CheckedAdd(other Money) (Money, bool) {
	result, overflow := addInt64(m.cents, other.cents)
	if overflow {
		return Money{}, false
	}
	return Money{cents: result}, true
}

// CheckedSub returns the difference, or false if underflow.
// Rust parity: Money::checked_sub(self, rhs) -> Option<Money>.
func (m Money) CheckedSub(other Money) (Money, bool) {
	result, overflow := subInt64(m.cents, other.cents)
	if overflow {
		return Money{}, false
	}
	return Money{cents: result}, true
}

// LessThan returns true if m is less than other.
func (m Money) LessThan(other Money) bool {
	return m.cents < other.cents
}

// IsZero returns true if the amount is zero.
func (m Money) IsZero() bool {
	return m.cents == 0
}

// String returns a formatted dollar string.
// Rust parity: Display for Money { write!(f, "${:.2}", self.dollars()) }.
func (m Money) String() string {
	return fmt.Sprintf("$%.2f", m.Dollars())
}

// addInt64 adds two int64s and detects overflow.
func addInt64(a, b int64) (int64, bool) {
	result := a + b
	if (b > 0 && result < a) || (b < 0 && result > a) {
		return 0, true
	}
	return result, false
}

// subInt64 subtracts two int64s and detects overflow.
func subInt64(a, b int64) (int64, bool) {
	result := a - b
	if (b > 0 && result > a) || (b < 0 && result < a) {
		return 0, true
	}
	return result, false
}
