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
var secp256k1Instance *secp256k1curve

// secp256k1curve implements elliptic.Curve with correct field arithmetic for secp256k1.
// Go's generic CurveParams doesn't work for non-NIST curves because its point
// validation assumes specific field properties.
type secp256k1curve struct {
	elliptic.CurveParams
}

// secp256k1Curve returns the secp256k1 elliptic curve used by Ethereum and Bitcoin.
func secp256k1Curve() elliptic.Curve {
	initSecp256k1Once.Do(func() {
		secp256k1Instance = &secp256k1curve{}
		secp256k1Instance.P, _ = new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F", 16)
		secp256k1Instance.N, _ = new(big.Int).SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16)
		secp256k1Instance.B, _ = new(big.Int).SetString("0000000000000000000000000000000000000000000000000000000000000007", 16)
		secp256k1Instance.Gx, _ = new(big.Int).SetString("79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798", 16)
		secp256k1Instance.Gy, _ = new(big.Int).SetString("483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8", 16)
		secp256k1Instance.Name = "secp256k1"
		secp256k1Instance.BitSize = 256
	})
	return secp256k1Instance
}

// Params returns the curve parameters.
func (c *secp256k1curve) Params() *elliptic.CurveParams {
	return &c.CurveParams
}

// IsOnCurve checks if (x, y) is on the secp256k1 curve: y^2 = x^3 + 7 (mod P).
func (c *secp256k1curve) IsOnCurve(x, y *big.Int) bool {
	// y^2 mod P
	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, c.P)

	// x^3 + 7 mod P
	x3 := new(big.Int).Mul(x, x)
	x3.Mul(x3, x)
	x3.Add(x3, c.B)
	x3.Mod(x3, c.P)

	return y2.Cmp(x3) == 0
}

// ScalarBaseMult returns k*G where G is the generator point.
func (c *secp256k1curve) ScalarBaseMult(k []byte) (*big.Int, *big.Int) {
	return c.ScalarMult(c.Gx, c.Gy, k)
}

// ScalarMult returns k*(x,y) using the double-and-add algorithm.
func (c *secp256k1curve) ScalarMult(Bx, By *big.Int, k []byte) (*big.Int, *big.Int) {
	// Convert k to big.Int to iterate bits.
	scalar := new(big.Int).SetBytes(k)
	// Reduce modulo N.
	scalar.Mod(scalar, c.N)

	// Double-and-add.
	rx, ry := new(big.Int), new(big.Int) // point at infinity
	px, py := new(big.Int).Set(Bx), new(big.Int).Set(By)
	atInfinity := true

	for i := scalar.BitLen() - 1; i >= 0; i-- {
		if !atInfinity {
			rx, ry = c.addPoint(rx, ry, rx, ry) // double
		}
		if scalar.Bit(i) == 1 {
			if atInfinity {
				rx.Set(px)
				ry.Set(py)
				atInfinity = false
			} else {
				rx, ry = c.addPoint(rx, ry, px, py) // add
			}
		}
	}

	if atInfinity {
		return new(big.Int), new(big.Int)
	}
	return rx, ry
}

// Add returns (x1,y1) + (x2,y2) on the curve.
func (c *secp256k1curve) Add(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) {
	return c.addPoint(x1, y1, x2, y2)
}

// Double returns 2*(x,y) on the curve.
func (c *secp256k1curve) Double(x1, y1 *big.Int) (*big.Int, *big.Int) {
	return c.addPoint(x1, y1, x1, y1)
}

// addPoint performs elliptic curve point addition over secp256k1.
func (c *secp256k1curve) addPoint(x1, y1, x2, y2 *big.Int) (*big.Int, *big.Int) {
	p := c.P

	if x1.Sign() == 0 && y1.Sign() == 0 {
		return new(big.Int).Set(x2), new(big.Int).Set(y2)
	}
	if x2.Sign() == 0 && y2.Sign() == 0 {
		return new(big.Int).Set(x1), new(big.Int).Set(y1)
	}

	var lam *big.Int

	if x1.Cmp(x2) == 0 && y1.Cmp(y2) == 0 {
		// Point doubling: lambda = (3*x1^2) / (2*y1) mod p
		num := new(big.Int).Mul(x1, x1)
		num.Mul(num, big.NewInt(3))
		num.Mod(num, p)
		den := new(big.Int).Mul(big.NewInt(2), y1)
		den.Mod(den, p)
		denInv := new(big.Int).ModInverse(den, p)
		if denInv == nil {
			return new(big.Int), new(big.Int) // point at infinity
		}
		lam = num.Mul(num, denInv)
		lam.Mod(lam, p)
	} else {
		// Point addition: lambda = (y2 - y1) / (x2 - x1) mod p
		num := new(big.Int).Sub(y2, y1)
		num.Mod(num, p)
		den := new(big.Int).Sub(x2, x1)
		den.Mod(den, p)
		denInv := new(big.Int).ModInverse(den, p)
		if denInv == nil {
			return new(big.Int), new(big.Int) // point at infinity
		}
		lam = num.Mul(num, denInv)
		lam.Mod(lam, p)
	}

	// x3 = lambda^2 - x1 - x2 mod p
	x3 := new(big.Int).Mul(lam, lam)
	x3.Sub(x3, x1)
	x3.Sub(x3, x2)
	x3.Mod(x3, p)

	// y3 = lambda*(x1 - x3) - y1 mod p
	y3 := new(big.Int).Sub(x1, x3)
	y3.Mul(y3, lam)
	y3.Sub(y3, y1)
	y3.Mod(y3, p)

	return x3, y3
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
