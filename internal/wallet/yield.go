package wallet

import (
	"fmt"

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

// YieldEngine manages automated yield strategies (Aave V3 on Base).
type YieldEngine struct {
	cfg YieldConfig
}

// NewYieldEngine creates a yield engine.
func NewYieldEngine(cfg YieldConfig) *YieldEngine {
	return &YieldEngine{cfg: cfg}
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

// Deposit supplies USDC to Aave V3.
// Returns the transaction hash.
func (y *YieldEngine) Deposit(amount float64, agentAddress string) (string, error) {
	if !y.cfg.Enabled {
		return "", fmt.Errorf("yield: engine not enabled")
	}

	log.Info().
		Float64("amount", amount).
		Str("protocol", y.cfg.Protocol).
		Str("agent", agentAddress).
		Msg("yield deposit initiated")

	// In production: build Aave supply() calldata, sign, submit.
	// For now, return a mock response.
	return "0x" + "0000000000000000000000000000000000000000000000000000000000000000", nil
}

// Withdraw removes USDC from Aave V3.
func (y *YieldEngine) Withdraw(amount float64, agentAddress string) (string, error) {
	if !y.cfg.Enabled {
		return "", fmt.Errorf("yield: engine not enabled")
	}

	log.Info().
		Float64("amount", amount).
		Str("protocol", y.cfg.Protocol).
		Str("agent", agentAddress).
		Msg("yield withdrawal initiated")

	return "0x" + "0000000000000000000000000000000000000000000000000000000000000000", nil
}

// GetATokenBalance queries the aToken balance from Aave.
func (y *YieldEngine) GetATokenBalance(agentAddress string) (float64, error) {
	if y.cfg.ChainRPCURL == "" {
		return 0, fmt.Errorf("yield: no RPC URL configured")
	}
	// In production: call aToken.balanceOf(agentAddress) via eth_call.
	return 0, nil
}
