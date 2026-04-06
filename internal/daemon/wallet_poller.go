package daemon

import (
	"context"
	"math/big"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/wallet"
)

// startWalletPoller periodically fetches on-chain balances and caches them in
// the wallet_balances table. The poll interval is configurable via
// wallet.balance_poll_seconds (default 60s, minimum 10s to respect RPC rate limits).
func startWalletPoller(ctx context.Context, cfg *core.Config, store *db.Store) {
	interval := cfg.Wallet.BalancePollSeconds
	if interval <= 0 {
		log.Info().Msg("wallet balance poller disabled (balance_poll_seconds=0)")
		return
	}
	if interval < 10 {
		interval = 10 // floor to avoid hitting RPC rate limits
	}

	if cfg.Wallet.RPCURL == "" {
		log.Info().Msg("wallet balance poller disabled (no rpc_url configured)")
		return
	}

	// Use machine-derived passphrase (same as keystore) — no separate env var needed.
	passphrase := core.MachinePassphrase()
	w, err := wallet.NewWallet(wallet.WalletConfig{
		Path:       cfg.Wallet.Path,
		ChainID:    int64(cfg.Wallet.ChainID),
		RPCURL:     cfg.Wallet.RPCURL,
		Passphrase: passphrase,
	})
	if err != nil {
		log.Warn().Err(err).Msg("wallet balance poller: failed to load wallet, polling disabled")
		return
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	go func() {
		// Poll immediately on startup, then on interval.
		pollWalletBalance(ctx, w, store)
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				pollWalletBalance(ctx, w, store)
			}
		}
	}()
	log.Info().Int("interval_s", interval).Msg("wallet balance poller started")
}

func pollWalletBalance(ctx context.Context, w *wallet.Wallet, store *db.Store) {
	// Fetch native ETH balance.
	if balance, err := w.GetBalance(); err == nil {
		ethBalance := weiToEther(balance)
		upsertBalance(ctx, store, "ETH", "Ether", ethBalance, "", 18, true)
	} else {
		log.Debug().Err(err).Msg("wallet poller: failed to fetch ETH balance")
	}

	// Fetch USDC balance (Base mainnet USDC contract).
	usdcContract := "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	if balance, err := w.GetERC20Balance(usdcContract); err == nil {
		usdcBalance := float64(balance.Int64()) / 1e6 // USDC has 6 decimals
		upsertBalance(ctx, store, "USDC", "USD Coin", usdcBalance, usdcContract, 6, false)
	} else {
		log.Debug().Err(err).Msg("wallet poller: failed to fetch USDC balance")
	}
}

func upsertBalance(ctx context.Context, store *db.Store, symbol, name string, balance float64, contract string, decimals int, isNative bool) {
	native := 0
	if isNative {
		native = 1
	}
	_, err := store.ExecContext(ctx,
		`INSERT INTO wallet_balances (symbol, name, balance, contract, decimals, is_native, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(symbol) DO UPDATE SET
		   balance = excluded.balance,
		   updated_at = datetime('now')`,
		symbol, name, balance, contract, decimals, native)
	if err != nil {
		log.Debug().Err(err).Str("symbol", symbol).Msg("wallet poller: failed to upsert balance")
	}
}

func weiToEther(wei *big.Int) float64 {
	if wei == nil {
		return 0
	}
	f := new(big.Float).SetInt(wei)
	divisor := new(big.Float).SetFloat64(1e18)
	result, _ := new(big.Float).Quo(f, divisor).Float64()
	return result
}
