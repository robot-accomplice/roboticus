package wallet

import (
	"encoding/hex"
	"fmt"
	"math/big"
)

// EIP3009Authorization models a transferWithAuthorization payload.
type EIP3009Authorization struct {
	Token       string
	From        string
	To          string
	Value       *big.Int
	ValidAfter  int64
	ValidBefore int64
	Nonce       []byte
	ChainID     int64
}

// NormalizedNonce returns the authorization nonce as a 32-byte value.
func (a EIP3009Authorization) NormalizedNonce() ([]byte, error) {
	if len(a.Nonce) == 0 {
		return make([]byte, 32), nil
	}
	if len(a.Nonce) != 32 {
		return nil, fmt.Errorf("eip3009: nonce must be 32 bytes, got %d", len(a.Nonce))
	}
	nonce := make([]byte, 32)
	copy(nonce, a.Nonce)
	return nonce, nil
}

// Digest builds the EIP-712 digest for transferWithAuthorization.
func (a EIP3009Authorization) Digest() ([]byte, error) {
	if a.Value == nil {
		return nil, fmt.Errorf("eip3009: value is required")
	}
	if a.ChainID == 0 {
		return nil, fmt.Errorf("eip3009: chain ID is required")
	}
	nonce, err := a.NormalizedNonce()
	if err != nil {
		return nil, err
	}

	domainSep := eip712DomainSeparator(a.Token, a.ChainID)
	typeHash := keccak256([]byte("TransferWithAuthorization(address from,address to,uint256 value,uint256 validAfter,uint256 validBefore,bytes32 nonce)"))

	structData := append(typeHash, addressToBytes32(a.From)...)
	structData = append(structData, addressToBytes32(a.To)...)
	structData = append(structData, uint256ToBytes32(a.Value)...)
	structData = append(structData, uint256ToBytes32(big.NewInt(a.ValidAfter))...)
	structData = append(structData, uint256ToBytes32(big.NewInt(a.ValidBefore))...)
	structData = append(structData, nonce...)
	structHash := keccak256(structData)

	return keccak256(append([]byte{0x19, 0x01}, append(domainSep, structHash...)...)), nil
}

// NonceHex returns the authorization nonce as lowercase hex without a 0x prefix.
func (a EIP3009Authorization) NonceHex() (string, error) {
	nonce, err := a.NormalizedNonce()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce), nil
}

// SignEIP3009TransferWithAuthorization signs an EIP-3009 transfer authorization.
func (w *Wallet) SignEIP3009TransferWithAuthorization(auth EIP3009Authorization) ([]byte, error) {
	if w.privateKey == nil {
		return nil, fmt.Errorf("eip3009: wallet private key not loaded")
	}
	if auth.From == "" {
		auth.From = w.Address()
	}
	if auth.ChainID == 0 {
		auth.ChainID = w.ChainID()
	}

	digest, err := auth.Digest()
	if err != nil {
		return nil, err
	}
	return signDigest(w.privateKey, digest)
}
