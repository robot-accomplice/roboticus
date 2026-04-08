package wallet

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// TxReceipt represents the result of a confirmed EVM transaction.
type TxReceipt struct {
	TxHash      string `json:"tx_hash"`
	BlockNumber uint64 `json:"block_number"`
	Status      int    `json:"status"` // 1 = success, 0 = revert
	GasUsed     uint64 `json:"gas_used"`
}

// EVMSubmitter handles broadcasting signed transactions to an EVM-compatible chain.
type EVMSubmitter struct {
	rpcURL  string
	chainID uint64
}

// NewEVMSubmitter creates a new EVM transaction submitter.
func NewEVMSubmitter(rpcURL string, chainID uint64) *EVMSubmitter {
	return &EVMSubmitter{
		rpcURL:  rpcURL,
		chainID: chainID,
	}
}

// ChainID returns the configured chain ID.
func (s *EVMSubmitter) ChainID() uint64 { return s.chainID }

// SubmitTx broadcasts a signed transaction and returns the transaction hash.
func (s *EVMSubmitter) SubmitTx(ctx context.Context, signedTx []byte) (string, error) {
	if len(signedTx) == 0 {
		return "", fmt.Errorf("evm_submit: empty transaction")
	}

	txHex := "0x" + hex.EncodeToString(signedTx)

	result, err := s.rpcCall(ctx, "eth_sendRawTransaction", []any{txHex})
	if err != nil {
		return "", fmt.Errorf("evm_submit: %w", err)
	}

	txHash, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("evm_submit: unexpected response type for tx hash")
	}

	return txHash, nil
}

// WaitForReceipt polls for a transaction receipt until confirmed or timeout.
func (s *EVMSubmitter) WaitForReceipt(ctx context.Context, txHash string, timeout time.Duration) (*TxReceipt, error) {
	if txHash == "" {
		return nil, fmt.Errorf("evm_submit: empty tx hash")
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, fmt.Errorf("evm_submit: timeout waiting for receipt of %s", txHash)
		case <-ticker.C:
			receipt, err := s.getReceipt(ctx, txHash)
			if err != nil {
				continue // receipt not yet available
			}
			return receipt, nil
		}
	}
}

func (s *EVMSubmitter) getReceipt(ctx context.Context, txHash string) (*TxReceipt, error) {
	result, err := s.rpcCall(ctx, "eth_getTransactionReceipt", []any{txHash})
	if err != nil {
		return nil, err
	}

	// Result is nil when the receipt is not yet available.
	if result == nil {
		return nil, fmt.Errorf("receipt not available")
	}

	m, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("evm_submit: unexpected receipt type")
	}

	receipt := &TxReceipt{TxHash: txHash}

	if bn, ok := m["blockNumber"].(string); ok {
		n := new(big.Int)
		n.SetString(strings.TrimPrefix(bn, "0x"), 16)
		receipt.BlockNumber = n.Uint64()
	}

	if st, ok := m["status"].(string); ok {
		n := new(big.Int)
		n.SetString(strings.TrimPrefix(st, "0x"), 16)
		receipt.Status = int(n.Int64())
	}

	if gu, ok := m["gasUsed"].(string); ok {
		n := new(big.Int)
		n.SetString(strings.TrimPrefix(gu, "0x"), 16)
		receipt.GasUsed = n.Uint64()
	}

	return receipt, nil
}

func (s *EVMSubmitter) rpcCall(ctx context.Context, method string, params []any) (any, error) {
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling RPC request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.rpcURL, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("RPC request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("reading RPC response: %w", err)
	}

	var rpcResp struct {
		Result any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("decoding RPC response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}
