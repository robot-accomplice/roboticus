package daemon

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/sha3"

	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/wallet"
)

// startWalletPoller periodically fetches on-chain balances and caches them in
// the wallet_balances table. The poll interval is configurable via
// wallet.balance_poll_seconds (default 60s, minimum 10s to respect RPC rate limits).
func startWalletPoller(ctx context.Context, cfg *core.Config, store *db.Store, ks *core.Keystore) {
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

	// Ensure wallet_balances table exists (handles DBs created before migration 030).
	_, _ = store.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS wallet_balances (
			symbol TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', balance REAL NOT NULL DEFAULT 0.0,
			contract TEXT NOT NULL DEFAULT '', decimals INTEGER NOT NULL DEFAULT 18,
			is_native INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL DEFAULT (datetime('now')))`)


	// Check for plaintext wallet and encrypt it on first run.
	result, err := wallet.MigratePlaintextWallet(cfg.Wallet.Path)
	if err != nil {
		log.Error().Err(err).Msg("wallet migration failed")
	}

	// Resolve passphrase: migration result > keystore > env > machine-derived.
	passphrase := ""
	if result != nil && result.Migrated {
		passphrase = result.Passphrase

		// Store in keystore for future auto-unlock.
		if ks != nil && ks.IsUnlocked() {
			if err := ks.Set("wallet_passphrase", passphrase); err == nil {
				_ = ks.Save()
				log.Info().Msg("wallet passphrase stored in keystore")
			}
		}

		// Display the passphrase exactly once.
		fmt.Println()
		fmt.Println("╔══════════════════════════════════════════════════════════════════════╗")
		fmt.Println("║  WALLET ENCRYPTED — SAVE THIS PASSPHRASE (shown once only):          ║")
		fmt.Printf("║  %s    ║\n", passphrase)
		fmt.Printf("║  Address: %-56s   ║\n", result.Address)
		fmt.Println("║                                                                      ║")
		fmt.Println("║  The passphrase has been stored in the keystore for auto-unlock.     ║")
		fmt.Println("║  You can also set ROBOTICUS_WALLET_PASSPHRASE as a backup.           ║")
		fmt.Println("╚══════════════════════════════════════════════════════════════════════╝")
		fmt.Println()
	}

	// If no migration happened, resolve passphrase from existing sources.
	if passphrase == "" {
		if ks != nil && ks.IsUnlocked() {
			passphrase = ks.GetOrEmpty("wallet_passphrase")
		}
	}
	if passphrase == "" {
		passphrase = os.Getenv("ROBOTICUS_WALLET_PASSPHRASE")
	}
	if passphrase == "" {
		passphrase = walletMachinePassphrase()
	}

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
		log.Warn().Err(err).Msg("wallet poller: failed to fetch ETH balance")
	}

	// Fetch USDC balance (Base mainnet USDC contract).
	usdcContract := "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	if balance, err := w.GetERC20Balance(usdcContract); err == nil {
		usdcBalance := float64(balance.Int64()) / 1e6 // USDC has 6 decimals
		upsertBalance(ctx, store, "USDC", "USD Coin", usdcBalance, usdcContract, 6, false)
	} else {
		log.Warn().Err(err).Msg("wallet poller: failed to fetch USDC balance")
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
		log.Warn().Err(err).Str("symbol", symbol).Msg("wallet poller: failed to upsert balance")
	}
}

// walletMachinePassphrase derives the wallet passphrase using the same algorithm
// as Rust's Wallet::machine_passphrase(): keccak256("roboticus-wallet-machine-key::{hostname}::{user}").
func walletMachinePassphrase() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	if user == "" {
		user = "unknown-user"
	}
	input := fmt.Sprintf("roboticus-wallet-machine-key::%s::%s", hostname, user)
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil))
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
