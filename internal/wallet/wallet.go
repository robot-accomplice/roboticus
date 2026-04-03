package wallet

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/scrypt"

	"github.com/rs/zerolog/log"
)

var rpcRequestID atomic.Int64

// Wallet constants.
const (
	DefaultChainID = 8453 // Base L2

	// Scrypt key derivation parameters for wallet encryption.
	ScryptN = 262144
	ScryptR = 8
	ScryptP = 1
)

// WalletConfig holds wallet configuration.
type WalletConfig struct {
	Path       string `mapstructure:"path"`     // file path for encrypted key
	ChainID    int64  `mapstructure:"chain_id"` // default 8453 (Base)
	RPCURL     string `mapstructure:"rpc_url"`  // EVM JSON-RPC endpoint
	Passphrase string `mapstructure:"-"`        // from env: GOBOTICUS_WALLET_PASSPHRASE
}

// Wallet manages an Ethereum-compatible HD wallet.
type Wallet struct {
	cfg        WalletConfig
	privateKey *ecdsa.PrivateKey
	address    string
}

// NewWallet creates or loads a wallet.
func NewWallet(cfg WalletConfig) (*Wallet, error) {
	if cfg.ChainID == 0 {
		cfg.ChainID = DefaultChainID
	}
	if cfg.Passphrase == "" {
		cfg.Passphrase = os.Getenv("GOBOTICUS_WALLET_PASSPHRASE")
	}

	w := &Wallet{cfg: cfg}

	if cfg.Path != "" {
		if err := w.loadOrGenerate(); err != nil {
			return nil, err
		}
	} else {
		if err := w.generate(); err != nil {
			return nil, err
		}
	}

	return w, nil
}

// Address returns the wallet's Ethereum address.
func (w *Wallet) Address() string { return w.address }

// ChainID returns the configured chain ID.
func (w *Wallet) ChainID() int64 { return w.cfg.ChainID }

// PrivateKey returns the private key (for signing operations).
func (w *Wallet) PrivateKey() *ecdsa.PrivateKey { return w.privateKey }

func (w *Wallet) loadOrGenerate() error {
	data, err := os.ReadFile(w.cfg.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return w.generateAndSave()
		}
		return fmt.Errorf("wallet load: %w", err)
	}

	// Try decrypting.
	if w.cfg.Passphrase != "" {
		if err := w.decrypt(data); err == nil {
			log.Info().Str("address", w.address).Msg("wallet loaded (encrypted)")
			return nil
		}
	}

	// Plaintext wallet keys are rejected for security.
	// Set GOBOTICUS_WALLET_PASSPHRASE to encrypt the wallet.
	if w.cfg.Passphrase == "" {
		return fmt.Errorf("wallet: passphrase required to load wallet (set GOBOTICUS_WALLET_PASSPHRASE)")
	}
	return fmt.Errorf("wallet: cannot decrypt key (wrong passphrase?)")
}

func (w *Wallet) generateAndSave() error {
	if err := w.generate(); err != nil {
		return err
	}

	keyBytes := w.privateKey.D.Bytes() //nolint:staticcheck // TODO: migrate to modern crypto API
	var data []byte

	if w.cfg.Passphrase != "" {
		var err error
		data, err = w.encrypt(keyBytes)
		if err != nil {
			return fmt.Errorf("wallet encrypt: %w", err)
		}
	} else {
		return fmt.Errorf("wallet: passphrase required to save wallet (set GOBOTICUS_WALLET_PASSPHRASE)")
	}

	if err := os.WriteFile(w.cfg.Path, data, 0600); err != nil {
		return fmt.Errorf("wallet save: %w", err)
	}

	log.Info().Str("address", w.address).Msg("wallet generated and saved")
	return nil
}

func (w *Wallet) generate() error {
	// Generate a random 32-byte private key.
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return err
	}
	return w.fromBytes(keyBytes)
}

func (w *Wallet) fromBytes(keyBytes []byte) error {
	// Construct ECDSA private key on secp256k1 curve.
	privKey := new(ecdsa.PrivateKey)
	privKey.D = new(big.Int).SetBytes(keyBytes)                                                 //nolint:staticcheck // TODO: migrate to modern crypto API
	privKey.PublicKey.Curve = secp256k1Curve()                                                  //nolint:staticcheck // TODO: migrate to modern crypto API
	privKey.PublicKey.X, privKey.PublicKey.Y = privKey.PublicKey.Curve.ScalarBaseMult(keyBytes) //nolint:staticcheck // TODO: migrate to modern crypto API

	w.privateKey = privKey
	w.address = pubKeyToAddress(&privKey.PublicKey)
	return nil
}

func (w *Wallet) encrypt(plaintext []byte) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}

	key, err := deriveKeyFromPassphrase(w.cfg.Passphrase, salt)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Format: salt(16) || nonce(12) || ciphertext
	result := make([]byte, 0, len(salt)+len(nonce)+len(ciphertext))
	result = append(result, salt...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)
	return result, nil
}

func (w *Wallet) decrypt(data []byte) error {
	if len(data) < 28 { // 16 salt + 12 nonce minimum
		return fmt.Errorf("encrypted data too short")
	}

	salt := data[:16]
	key, err := deriveKeyFromPassphrase(w.cfg.Passphrase, salt)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < 16+nonceSize {
		return fmt.Errorf("encrypted data too short")
	}

	nonce := data[16 : 16+nonceSize]
	ciphertext := data[16+nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return err
	}

	return w.fromBytes(plaintext)
}

func deriveKeyFromPassphrase(passphrase string, salt []byte) ([]byte, error) {
	// scrypt with N=262144, r=8, p=1 — designed for low-entropy passphrases.
	// Provides ~100ms key derivation on modern hardware, making brute force
	// infeasible compared to the previous HKDF which was near-instant.
	return scrypt.Key([]byte(passphrase), salt, ScryptN, ScryptR, ScryptP, 32)
}

// GetBalance queries the native balance via JSON-RPC.
func (w *Wallet) GetBalance() (*big.Int, error) {
	if w.cfg.RPCURL == "" {
		return nil, fmt.Errorf("wallet: no RPC URL configured")
	}

	result, err := w.rpcCall("eth_getBalance", []any{w.address, "latest"})
	if err != nil {
		return nil, err
	}

	hexVal, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("wallet: unexpected balance type")
	}

	balance := new(big.Int)
	balance.SetString(strings.TrimPrefix(hexVal, "0x"), 16)
	return balance, nil
}

func (w *Wallet) rpcCall(method string, params []any) (any, error) {
	if w.cfg.RPCURL == "" {
		return nil, fmt.Errorf("wallet: no RPC URL configured")
	}

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      rpcRequestID.Add(1),
		"method":  method,
		"params":  params,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("wallet: marshaling RPC request: %w", err)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(w.cfg.RPCURL, "application/json", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("wallet: RPC request to %s failed: %w", w.cfg.RPCURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("wallet: reading RPC response: %w", err)
	}

	var rpcResp struct {
		Result any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("wallet: decoding RPC response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("wallet: RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// GetERC20Balance queries the balance of an ERC-20 token.
func (w *Wallet) GetERC20Balance(tokenContract string) (*big.Int, error) {
	if w.cfg.RPCURL == "" {
		return nil, fmt.Errorf("wallet: no RPC URL configured")
	}

	// ERC-20 balanceOf(address) selector = 0x70a08231 + padded address.
	addrPadded := fmt.Sprintf("000000000000000000000000%s", strings.TrimPrefix(w.address, "0x"))
	callData := "0x70a08231" + addrPadded

	result, err := w.rpcCall("eth_call", []any{
		map[string]string{
			"to":   tokenContract,
			"data": callData,
		},
		"latest",
	})
	if err != nil {
		return nil, err
	}

	hexVal, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("wallet: unexpected token balance type")
	}

	balance := new(big.Int)
	balance.SetString(strings.TrimPrefix(hexVal, "0x"), 16)
	return balance, nil
}

// GetChainID queries the current chain ID.
func (w *Wallet) GetChainID() (uint64, error) {
	result, err := w.rpcCall("eth_chainId", []any{})
	if err != nil {
		return 0, err
	}
	hexVal, ok := result.(string)
	if !ok {
		return 0, fmt.Errorf("wallet: unexpected chainId type")
	}
	chainID := new(big.Int)
	chainID.SetString(strings.TrimPrefix(hexVal, "0x"), 16)
	return chainID.Uint64(), nil
}

// GetTransactionCount returns the nonce for the wallet address.
func (w *Wallet) GetTransactionCount() (uint64, error) {
	result, err := w.rpcCall("eth_getTransactionCount", []any{w.address, "latest"})
	if err != nil {
		return 0, err
	}
	hexVal, ok := result.(string)
	if !ok {
		return 0, fmt.Errorf("wallet: unexpected nonce type")
	}
	nonce := new(big.Int)
	nonce.SetString(strings.TrimPrefix(hexVal, "0x"), 16)
	return nonce.Uint64(), nil
}

// EthCall performs a read-only contract call and returns the hex result.
func (w *Wallet) EthCall(to string, data string) (string, error) {
	result, err := w.rpcCall("eth_call", []any{
		map[string]string{"to": to, "data": data},
		"latest",
	})
	if err != nil {
		return "", err
	}
	hexVal, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("wallet: unexpected eth_call result type")
	}
	return hexVal, nil
}

// SendTransaction builds, signs (placeholder), and broadcasts a transaction.
// Full EIP-1559 signing requires go-ethereum; this provides the calldata interface.
func (w *Wallet) SendTransaction(to string, value *big.Int, data []byte) (string, error) {
	if w.cfg.RPCURL == "" {
		return "", fmt.Errorf("wallet: no RPC URL")
	}
	// For a complete implementation, this would build an EIP-1559 tx, sign it with
	// the private key, RLP-encode, and call eth_sendRawTransaction.
	// For now, log the intent and return a placeholder.
	log.Info().
		Str("to", to).
		Str("value", value.String()).
		Int("data_len", len(data)).
		Msg("wallet: transaction prepared (signing requires go-ethereum)")
	return fmt.Sprintf("0x_pending_%s_%x", to[:8], data[:4]), nil
}

// SendRawTransaction broadcasts a signed transaction.
func (w *Wallet) SendRawTransaction(signedTxHex string) (string, error) {
	result, err := w.rpcCall("eth_sendRawTransaction", []any{signedTxHex})
	if err != nil {
		return "", err
	}
	txHash, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("wallet: unexpected tx hash type")
	}
	return txHash, nil
}
