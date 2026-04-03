package wallet

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/rs/zerolog/log"
)

// YieldConfig holds DeFi yield strategy parameters.
type YieldConfig struct {
	Enabled             bool    `mapstructure:"enabled"`
	Protocol            string  `mapstructure:"protocol"`             // e.g. "aave"
	Chain               string  `mapstructure:"chain"`                // e.g. "base"
	MinDeposit          float64 `mapstructure:"min_deposit"`          // minimum deposit ($)
	WithdrawalThreshold float64 `mapstructure:"withdrawal_threshold"` // withdraw when below ($)
	ChainRPCURL         string  `mapstructure:"chain_rpc_url"`
	PoolAddress         string  `mapstructure:"pool_address"`
	USDCAddress         string  `mapstructure:"usdc_address"`
	ATokenAddress       string  `mapstructure:"atoken_address"`
}

// Aave V3 IPool function selectors (pre-computed byte literals).
var (
	// supply(address asset, uint256 amount, address onBehalfOf, uint16 referralCode)
	aaveSupplySelector = []byte{0x61, 0x7b, 0xa0, 0x37}
	// withdraw(address asset, uint256 amount, address to)
	aaveWithdrawSelector = []byte{0x69, 0x32, 0x8d, 0xec}
	// approve(address spender, uint256 amount)
	erc20ApproveSelector = []byte{0x09, 0x5e, 0xa7, 0xb3}
	// balanceOf(address account)
	erc20BalanceOfSelector = []byte{0x70, 0xa0, 0x82, 0x31}
)

// YieldEngine manages automated yield strategies (Aave V3 on Base).
type YieldEngine struct {
	cfg    YieldConfig
	wallet *Wallet // optional: for signing real transactions
}

// NewYieldEngine creates a yield engine.
func NewYieldEngine(cfg YieldConfig, w *Wallet) *YieldEngine {
	return &YieldEngine{cfg: cfg, wallet: w}
}

// CalculateExcess returns the amount above reserve + 10% buffer.
func (y *YieldEngine) CalculateExcess(balance, minimumReserve float64) float64 {
	buffer := minimumReserve * 0.10
	excess := balance - minimumReserve - buffer
	if excess < 0 {
		return 0
	}
	return excess
}

// ShouldDeposit checks if there's enough excess to deposit.
func (y *YieldEngine) ShouldDeposit(excess float64) bool {
	return y.cfg.Enabled && excess > y.cfg.MinDeposit
}

// ShouldWithdraw checks if balance is below withdrawal threshold.
func (y *YieldEngine) ShouldWithdraw(balance float64) bool {
	return y.cfg.Enabled && balance < y.cfg.WithdrawalThreshold
}

// Deposit supplies USDC to Aave V3 via the IPool.supply() function.
// Builds real calldata: approve USDC spending, then call supply.
func (y *YieldEngine) Deposit(amount float64, agentAddress string) (string, error) {
	if !y.cfg.Enabled {
		return "", fmt.Errorf("yield: engine not enabled")
	}
	if y.cfg.ChainRPCURL == "" {
		return "", fmt.Errorf("yield: no RPC URL configured")
	}
	if y.cfg.PoolAddress == "" || y.cfg.USDCAddress == "" {
		return "", fmt.Errorf("yield: pool or USDC address not configured")
	}

	// Convert amount to USDC units (6 decimals).
	amountWei := usdcToWei(amount)

	log.Info().
		Float64("amount", amount).
		Str("protocol", y.cfg.Protocol).
		Str("agent", agentAddress).
		Str("pool", y.cfg.PoolAddress).
		Msg("yield deposit: building supply() calldata")

	// Step 1: Build approve calldata — approve(poolAddress, amount)
	approveData := buildCalldata(erc20ApproveSelector,
		padAddress(y.cfg.PoolAddress),
		padUint256(amountWei),
	)

	// Step 2: Build supply calldata — supply(usdcAddress, amount, onBehalfOf, 0)
	supplyData := buildCalldata(aaveSupplySelector,
		padAddress(y.cfg.USDCAddress),
		padUint256(amountWei),
		padAddress(agentAddress),
		padUint256(big.NewInt(0)), // referralCode
	)

	// If wallet is available, sign and broadcast. Otherwise return calldata.
	if y.wallet != nil && y.wallet.cfg.RPCURL != "" {
		// Send approve tx.
		_, err := y.wallet.SendTransaction(y.cfg.USDCAddress, big.NewInt(0), approveData)
		if err != nil {
			return "", fmt.Errorf("yield: approve failed: %w", err)
		}

		// Send supply tx.
		txHash, err := y.wallet.SendTransaction(y.cfg.PoolAddress, big.NewInt(0), supplyData)
		if err != nil {
			return "", fmt.Errorf("yield: supply failed: %w", err)
		}
		return txHash, nil
	}

	// Dry-run: return encoded calldata hash.
	return fmt.Sprintf("0x_dryrun_supply_%x", supplyData[:8]), nil
}

// Withdraw removes USDC from Aave V3 via IPool.withdraw().
func (y *YieldEngine) Withdraw(amount float64, agentAddress string) (string, error) {
	if !y.cfg.Enabled {
		return "", fmt.Errorf("yield: engine not enabled")
	}
	if y.cfg.ChainRPCURL == "" {
		return "", fmt.Errorf("yield: no RPC URL configured")
	}

	amountWei := usdcToWei(amount)

	log.Info().
		Float64("amount", amount).
		Str("protocol", y.cfg.Protocol).
		Str("agent", agentAddress).
		Msg("yield withdrawal: building withdraw() calldata")

	// withdraw(usdcAddress, amount, to)
	withdrawData := buildCalldata(aaveWithdrawSelector,
		padAddress(y.cfg.USDCAddress),
		padUint256(amountWei),
		padAddress(agentAddress),
	)

	if y.wallet != nil && y.wallet.cfg.RPCURL != "" {
		txHash, err := y.wallet.SendTransaction(y.cfg.PoolAddress, big.NewInt(0), withdrawData)
		if err != nil {
			return "", fmt.Errorf("yield: withdraw failed: %w", err)
		}
		return txHash, nil
	}

	return fmt.Sprintf("0x_dryrun_withdraw_%x", withdrawData[:8]), nil
}

// GetATokenBalance queries the aToken balance from Aave via balanceOf().
func (y *YieldEngine) GetATokenBalance(agentAddress string) (float64, error) {
	if y.cfg.ChainRPCURL == "" {
		return 0, fmt.Errorf("yield: no RPC URL configured")
	}
	if y.cfg.ATokenAddress == "" {
		return 0, fmt.Errorf("yield: aToken address not configured")
	}

	calldata := buildCalldata(erc20BalanceOfSelector, padAddress(agentAddress))
	calldataHex := "0x" + hex.EncodeToString(calldata)

	if y.wallet != nil {
		result, err := y.wallet.EthCall(y.cfg.ATokenAddress, calldataHex)
		if err != nil {
			return 0, fmt.Errorf("yield: balanceOf failed: %w", err)
		}
		balance := parseUint256(result)
		return weiToUSDC(balance), nil
	}

	return 0, nil
}

// --- ABI encoding helpers ---

func usdcToWei(amount float64) *big.Int {
	// USDC has 6 decimals.
	wei := new(big.Float).Mul(big.NewFloat(amount), big.NewFloat(1e6))
	result, _ := wei.Int(nil)
	return result
}

func weiToUSDC(wei *big.Int) float64 {
	if wei == nil {
		return 0
	}
	f := new(big.Float).SetInt(wei)
	f.Quo(f, big.NewFloat(1e6))
	result, _ := f.Float64()
	return result
}

func padAddress(addr string) []byte {
	// Remove 0x prefix, left-pad to 32 bytes.
	if len(addr) >= 2 && addr[:2] == "0x" {
		addr = addr[2:]
	}
	b, _ := hex.DecodeString(addr)
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return padded
}

func padUint256(v *big.Int) []byte {
	b := v.Bytes()
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return padded
}

func buildCalldata(selector []byte, args ...[]byte) []byte {
	data := make([]byte, 0, 4+len(args)*32)
	data = append(data, selector...)
	for _, arg := range args {
		data = append(data, arg...)
	}
	return data
}

func parseUint256(hexStr string) *big.Int {
	if len(hexStr) >= 2 && hexStr[:2] == "0x" {
		hexStr = hexStr[2:]
	}
	n := new(big.Int)
	n.SetString(hexStr, 16)
	return n
}
