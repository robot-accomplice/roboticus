package wallet

import (
	"fmt"
	"math"
)

// USDCMoney represents a USDC amount with 6-decimal precision.
// 1 USDC = 1,000,000 units (matching the ERC-20 token's 6 decimals).
type USDCMoney struct {
	units int64
}

// NewUSDCMoney converts a USD float64 to USDCMoney with 6-decimal precision.
func NewUSDCMoney(usd float64) USDCMoney {
	return USDCMoney{units: int64(math.Round(usd * 1_000_000))}
}

// USDCMoneyFromUnits creates a USDCMoney from raw micro-dollar units.
func USDCMoneyFromUnits(units int64) USDCMoney {
	return USDCMoney{units: units}
}

// USD returns the amount as a float64 dollar value.
func (m USDCMoney) USD() float64 {
	return float64(m.units) / 1_000_000
}

// Units returns the raw micro-dollar units.
func (m USDCMoney) Units() int64 {
	return m.units
}

// Add returns the sum of two USDCMoney values.
func (m USDCMoney) Add(other USDCMoney) USDCMoney {
	return USDCMoney{units: m.units + other.units}
}

// Sub returns the difference of two USDCMoney values.
func (m USDCMoney) Sub(other USDCMoney) USDCMoney {
	return USDCMoney{units: m.units - other.units}
}

// LessThan returns true if m is less than other.
func (m USDCMoney) LessThan(other USDCMoney) bool {
	return m.units < other.units
}

// IsZero returns true if the amount is zero.
func (m USDCMoney) IsZero() bool {
	return m.units == 0
}

// String returns a formatted USDC string.
func (m USDCMoney) String() string {
	sign := ""
	u := m.units
	if u < 0 {
		sign = "-"
		u = -u
	}
	whole := u / 1_000_000
	frac := u % 1_000_000
	return fmt.Sprintf("%s%d.%06d USDC", sign, whole, frac)
}
