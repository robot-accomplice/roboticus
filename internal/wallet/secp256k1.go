package wallet

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/hex"
	"math/big"
	"sync"

	"golang.org/x/crypto/sha3"
)

// secp256k1 curve parameters (Ethereum/Bitcoin standard).
var initSecp256k1Once sync.Once
var secp256k1 *elliptic.CurveParams

// secp256k1Curve returns the secp256k1 elliptic curve used by Ethereum and Bitcoin.
func secp256k1Curve() elliptic.Curve {
	initSecp256k1Once.Do(func() {
		secp256k1 = &elliptic.CurveParams{
			Name:    "secp256k1",
			BitSize: 256,
		}
		secp256k1.P, _ = new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F", 16)
		secp256k1.N, _ = new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16)
		secp256k1.B, _ = new(big.Int).SetString("0000000000000000000000000000000000000000000000000000000000000007", 16)
		secp256k1.Gx, _ = new(big.Int).SetString("79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798", 16)
		secp256k1.Gy, _ = new(big.Int).SetString("483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8", 16)
	})
	return secp256k1
}

// pubKeyToAddress derives an Ethereum address from a public key.
// Address = last 20 bytes of Keccak256(X || Y).
func pubKeyToAddress(pub *ecdsa.PublicKey) string {
	xBytes := pub.X.Bytes()
	yBytes := pub.Y.Bytes()

	// Pad to 32 bytes each.
	pubBytes := make([]byte, 64)
	copy(pubBytes[32-len(xBytes):32], xBytes)
	copy(pubBytes[64-len(yBytes):64], yBytes)

	hash := sha3.NewLegacyKeccak256()
	hash.Write(pubBytes)
	digest := hash.Sum(nil)

	return "0x" + hex.EncodeToString(digest[12:])
}

// --- Money type (fixed-point, cents) ---

// Money represents a monetary amount in cents (hundredths of a dollar).
type Money struct {
	cents int64
}

// FromDollars converts a dollar amount to Money.
func FromDollars(dollars float64) Money {
	return Money{cents: int64(dollars * 100)}
}

// Dollars returns the money amount in dollars.
func (m Money) Dollars() float64 {
	return float64(m.cents) / 100
}

// Cents returns the raw cent value.
func (m Money) Cents() int64 { return m.cents }

// Add returns the sum of two Money values (saturating).
func (m Money) Add(other Money) Money {
	result := m.cents + other.cents
	if (other.cents > 0 && result < m.cents) || (other.cents < 0 && result > m.cents) {
		if other.cents > 0 {
			return Money{cents: maxInt64}
		}
		return Money{cents: minInt64}
	}
	return Money{cents: result}
}

// Sub returns the difference of two Money values (saturating).
func (m Money) Sub(other Money) Money {
	return m.Add(Money{cents: -other.cents})
}

// String returns a formatted dollar string.
func (m Money) String() string {
	sign := ""
	c := m.cents
	if c < 0 {
		sign = "-"
		c = -c
	}
	return sign + "$" + big.NewInt(c/100).String() + "." + padTwo(c%100)
}

func padTwo(n int64) string {
	if n < 10 {
		return "0" + big.NewInt(n).String()
	}
	return big.NewInt(n).String()
}

const (
	maxInt64 = int64(^uint64(0) >> 1)
	minInt64 = -maxInt64 - 1
)
