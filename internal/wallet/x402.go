package wallet

import (
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"math/big"
	"time"

	"golang.org/x/crypto/sha3"
)

var (
	hmacNew   = hmac.New
	sha256New = func() hash.Hash { return sha256.New() }
)

// PaymentRequirements describes what a 402 response demands.
type PaymentRequirements struct {
	Amount    float64 `json:"amount"`
	Recipient string  `json:"recipient"`
	ChainID   int64   `json:"chain_id"`
	Token     string  `json:"token,omitempty"`    // ERC-20 contract address
	Nonce     string  `json:"nonce,omitempty"`    // authorization nonce
	Deadline  int64   `json:"deadline,omitempty"` // unix timestamp
}

// MaxPaymentAmount is the safety rail for automatic micropayments. Any payment
// request exceeding this amount (in token units, e.g. USD) is rejected.
const MaxPaymentAmount = 1.00

// X402Handler manages the x402 payment protocol (EIP-3009 transferWithAuthorization).
type X402Handler struct {
	wallet *Wallet
}

// NewX402Handler creates an x402 payment handler.
func NewX402Handler() *X402Handler {
	return &X402Handler{}
}

// NewX402HandlerWithWallet creates an x402 payment handler bound to a wallet,
// enabling it to satisfy the PaymentHandler interface for automatic 402 retry.
func NewX402HandlerWithWallet(w *Wallet) *X402Handler {
	return &X402Handler{wallet: w}
}

// HandlePayment satisfies the llm.PaymentHandler interface. It parses payment
// requirements from a 402 response body, validates the amount against the
// safety rail (max $1.00), signs the EIP-3009 authorization, and returns the
// X-Payment header value.
func (h *X402Handler) HandlePayment(body []byte) (string, error) {
	reqs, err := h.ParsePaymentRequirements(body)
	if err != nil {
		return "", err
	}

	// Safety rail: reject payments exceeding the maximum allowed amount.
	if reqs.Amount > MaxPaymentAmount {
		return "", fmt.Errorf("x402: payment amount $%.2f exceeds safety limit $%.2f", reqs.Amount, MaxPaymentAmount)
	}

	if h.wallet == nil {
		return "", fmt.Errorf("x402: no wallet configured for payment handler")
	}

	return h.Handle402(body, h.wallet)
}

// ParsePaymentRequirements extracts payment requirements from a 402 response body.
func (h *X402Handler) ParsePaymentRequirements(body []byte) (*PaymentRequirements, error) {
	var reqs PaymentRequirements
	if err := json.Unmarshal(body, &reqs); err != nil {
		return nil, fmt.Errorf("x402: parse requirements: %w", err)
	}
	if reqs.Amount <= 0 {
		return nil, fmt.Errorf("x402: invalid amount: %f", reqs.Amount)
	}
	if reqs.Recipient == "" {
		return nil, fmt.Errorf("x402: missing recipient")
	}
	return &reqs, nil
}

// Handle402 processes a 402 response by signing an EIP-3009 transferWithAuthorization.
// Returns the x402 payment header value.
func (h *X402Handler) Handle402(body []byte, w *Wallet) (string, error) {
	reqs, err := h.ParsePaymentRequirements(body)
	if err != nil {
		return "", err
	}

	if w.privateKey == nil {
		return "", fmt.Errorf("x402: wallet private key not loaded")
	}

	// Build EIP-712 typed data for transferWithAuthorization (EIP-3009).
	deadline := reqs.Deadline
	if deadline == 0 {
		deadline = time.Now().Add(5 * time.Minute).Unix()
	}

	// Convert amount to token base units (assume 6 decimals for USDC).
	amountWei := new(big.Int)
	amountWei.SetInt64(int64(reqs.Amount * 1e6))

	// Generate a nonce for the transferWithAuthorization payload.
	nonce := make([]byte, 32)
	if reqs.Nonce != "" {
		decoded, _ := hex.DecodeString(reqs.Nonce)
		if len(decoded) == 32 {
			copy(nonce, decoded)
		}
	}

	auth := EIP3009Authorization{
		Token:       reqs.Token,
		From:        w.address,
		To:          reqs.Recipient,
		Value:       amountWei,
		ValidAfter:  0,
		ValidBefore: deadline,
		Nonce:       nonce,
		ChainID:     reqs.ChainID,
	}

	// Sign the EIP-3009 authorization with the wallet's private key.
	sig, err := w.SignEIP3009TransferWithAuthorization(auth)
	if err != nil {
		return "", fmt.Errorf("x402: signing failed: %w", err)
	}

	return h.BuildPaymentHeader(reqs.Amount, reqs.Recipient, hex.EncodeToString(sig)), nil
}

// BuildPaymentHeader formats the x402 payment header.
func (h *X402Handler) BuildPaymentHeader(amount float64, recipient, auth string) string {
	return fmt.Sprintf("x402 amount=%.6f recipient=%s auth=%s", amount, recipient, auth)
}

// eip712DomainSeparator computes the domain separator for a token contract.
func eip712DomainSeparator(tokenAddress string, chainID int64) []byte {
	typeHash := keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	name := keccak256([]byte("USD Coin"))
	version := keccak256([]byte("2"))
	chain := uint256ToBytes32(big.NewInt(chainID))
	contract := addressToBytes32(tokenAddress)

	data := append(typeHash, name...)
	data = append(data, version...)
	data = append(data, chain...)
	data = append(data, contract...)
	return keccak256(data)
}

// keccak256 computes the Keccak-256 hash.
func keccak256(data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	return h.Sum(nil)
}

// addressToBytes32 converts a hex address to a 32-byte left-padded representation.
func addressToBytes32(addr string) []byte {
	addr = trimHexPrefix(addr)
	b, _ := hex.DecodeString(addr)
	result := make([]byte, 32)
	copy(result[32-len(b):], b)
	return result
}

// uint256ToBytes32 converts a big.Int to a 32-byte big-endian representation.
func uint256ToBytes32(v *big.Int) []byte {
	result := make([]byte, 32)
	b := v.Bytes()
	copy(result[32-len(b):], b)
	return result
}

func trimHexPrefix(s string) string {
	if len(s) >= 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}

// signDigest signs a 32-byte digest with deterministic ECDSA (RFC 6979),
// returning the 65-byte Ethereum signature (r || s || v).
func signDigest(key *ecdsa.PrivateKey, digest []byte) ([]byte, error) {
	// Generate deterministic k per RFC 6979 using HMAC-SHA256.
	k := rfc6979K(key, digest)

	curve := key.Curve
	N := curve.Params().N
	halfN := new(big.Int).Rsh(N, 1)

	// Compute R = k*G, r = R.x mod N.
	rx, ry, err := func() (*big.Int, *big.Int, error) {
		x, y := curve.ScalarBaseMult(k.Bytes()) //nolint:staticcheck // TODO: migrate to modern crypto API
		if x.Sign() == 0 && y.Sign() == 0 {
			return nil, nil, fmt.Errorf("invalid k: point at infinity")
		}
		return x, y, nil
	}()
	if err != nil {
		return nil, err
	}

	r := new(big.Int).Mod(rx, N)
	if r.Sign() == 0 {
		return nil, fmt.Errorf("r is zero")
	}

	// Compute s = k^-1 * (hash + r*d) mod N.
	kInv := new(big.Int).ModInverse(k, N)
	e := new(big.Int).SetBytes(digest)
	s := new(big.Int).Mul(r, key.D) //nolint:staticcheck // TODO: migrate to modern crypto API
	s.Add(s, e)
	s.Mul(s, kInv)
	s.Mod(s, N)
	if s.Sign() == 0 {
		return nil, fmt.Errorf("s is zero")
	}

	// Ethereum enforces low-s (EIP-2): if s > N/2, flip to N-s.
	if s.Cmp(halfN) > 0 {
		s.Sub(N, s)
	}

	// Recovery ID: v = 27 + (R.y parity), adjusted if s was flipped.
	v := byte(27)
	if ry.Bit(0) == 1 {
		v = 28
	}

	sig := make([]byte, 65)
	rB := r.Bytes()
	sB := s.Bytes()
	copy(sig[32-len(rB):32], rB)
	copy(sig[64-len(sB):64], sB)
	sig[64] = v
	return sig, nil
}

// rfc6979K generates a deterministic k value per RFC 6979 using HMAC-SHA256.
func rfc6979K(key *ecdsa.PrivateKey, hash []byte) *big.Int {
	q := key.Curve.Params().N
	x := key.D.Bytes() //nolint:staticcheck // TODO: migrate to modern crypto API

	// Pad private key to curve byte length.
	qLen := (q.BitLen() + 7) / 8
	if len(x) < qLen {
		padded := make([]byte, qLen)
		copy(padded[qLen-len(x):], x)
		x = padded
	}

	// Step a: h1 = hash (already provided).
	// Step b: V = 0x01 * 32.
	v := make([]byte, 32)
	for i := range v {
		v[i] = 0x01
	}
	// Step c: K = 0x00 * 32.
	kk := make([]byte, 32)

	// Step d: K = HMAC(K, V || 0x00 || x || h1).
	kk = hmacSHA256(kk, append(append(append(v, 0x00), x...), hash...))
	// Step e: V = HMAC(K, V).
	v = hmacSHA256(kk, v)
	// Step f: K = HMAC(K, V || 0x01 || x || h1).
	kk = hmacSHA256(kk, append(append(append(v, 0x01), x...), hash...))
	// Step g: V = HMAC(K, V).
	v = hmacSHA256(kk, v)

	// Step h: generate k.
	for {
		v = hmacSHA256(kk, v)
		k := new(big.Int).SetBytes(v)
		if k.Sign() > 0 && k.Cmp(q) < 0 {
			return k
		}
		kk = hmacSHA256(kk, append(v, 0x00))
		v = hmacSHA256(kk, v)
	}
}

// hmacSHA256 computes HMAC-SHA256.
func hmacSHA256(key, data []byte) []byte {
	h := hmacNew(sha256New, key)
	h.Write(data)
	return h.Sum(nil)
}
