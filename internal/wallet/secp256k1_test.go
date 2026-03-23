package wallet

import (
	"crypto/ecdsa"
	"math/big"
	"testing"
)

func TestSecp256k1_GeneratorOnCurve(t *testing.T) {
	curve := secp256k1Curve()
	params := curve.Params()
	if !curve.IsOnCurve(params.Gx, params.Gy) {
		t.Fatal("generator point is not on curve")
	}
}

func TestSecp256k1_OffCurveRejected(t *testing.T) {
	curve := secp256k1Curve()
	// Random point almost certainly not on curve.
	x := big.NewInt(12345)
	y := big.NewInt(67890)
	if curve.IsOnCurve(x, y) {
		t.Fatal("random point should not be on curve")
	}
}

func TestSecp256k1_ScalarBaseMult_KnownVector(t *testing.T) {
	curve := secp256k1Curve()
	// Private key = 1 should give the generator point.
	k := big.NewInt(1).Bytes()
	x, y := curve.ScalarBaseMult(k)
	params := curve.Params()
	if x.Cmp(params.Gx) != 0 || y.Cmp(params.Gy) != 0 {
		t.Fatal("k=1 should produce generator point")
	}
}

func TestSecp256k1_ScalarMult_Doubling(t *testing.T) {
	curve := secp256k1Curve()
	params := curve.Params()
	// 2*G via ScalarBaseMult.
	x1, y1 := curve.ScalarBaseMult(big.NewInt(2).Bytes())
	// G + G via Add.
	x2, y2 := curve.Add(params.Gx, params.Gy, params.Gx, params.Gy)
	if x1.Cmp(x2) != 0 || y1.Cmp(y2) != 0 {
		t.Fatal("2*G != G+G")
	}
	// Both should be on curve.
	if !curve.IsOnCurve(x1, y1) {
		t.Fatal("2*G not on curve")
	}
}

func TestSecp256k1_AddCommutativity(t *testing.T) {
	curve := secp256k1Curve()
	// P = 2*G, Q = 3*G.
	px, py := curve.ScalarBaseMult(big.NewInt(2).Bytes())
	qx, qy := curve.ScalarBaseMult(big.NewInt(3).Bytes())
	// P + Q.
	r1x, r1y := curve.Add(px, py, qx, qy)
	// Q + P.
	r2x, r2y := curve.Add(qx, qy, px, py)
	if r1x.Cmp(r2x) != 0 || r1y.Cmp(r2y) != 0 {
		t.Fatal("point addition is not commutative")
	}
	// P + Q = 5*G.
	fiveG_x, fiveG_y := curve.ScalarBaseMult(big.NewInt(5).Bytes())
	if r1x.Cmp(fiveG_x) != 0 || r1y.Cmp(fiveG_y) != 0 {
		t.Fatal("2G + 3G != 5G")
	}
}

func TestPubKeyToAddress_Length(t *testing.T) {
	curve := secp256k1Curve()
	x, y := curve.ScalarBaseMult(big.NewInt(42).Bytes())
	pub := &ecdsa.PublicKey{Curve: curve, X: x, Y: y}
	addr := pubKeyToAddress(pub)
	if len(addr) != 42 {
		t.Errorf("address length = %d, want 42", len(addr))
	}
	if addr[:2] != "0x" {
		t.Errorf("address should start with 0x, got %q", addr[:2])
	}
}

func TestPubKeyToAddress_Deterministic(t *testing.T) {
	curve := secp256k1Curve()
	x, y := curve.ScalarBaseMult(big.NewInt(99).Bytes())
	pub := &ecdsa.PublicKey{Curve: curve, X: x, Y: y}
	addr1 := pubKeyToAddress(pub)
	addr2 := pubKeyToAddress(pub)
	if addr1 != addr2 {
		t.Errorf("address not deterministic: %q != %q", addr1, addr2)
	}
}

func TestWallet_Generate_ValidAddress(t *testing.T) {
	w, err := NewWallet(WalletConfig{})
	if err != nil {
		t.Fatal(err)
	}
	addr := w.Address()
	if len(addr) != 42 || addr[:2] != "0x" {
		t.Errorf("invalid address: %q", addr)
	}

	// Key should be on the curve.
	curve := secp256k1Curve()
	if !curve.IsOnCurve(w.PrivateKey().PublicKey.X, w.PrivateKey().PublicKey.Y) {
		t.Fatal("generated key is not on secp256k1 curve")
	}
}
