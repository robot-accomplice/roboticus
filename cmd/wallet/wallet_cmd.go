package wallet

import (
	"roboticus/cmd/internal/cmdutil"
	"fmt"

	"github.com/spf13/cobra"
)

var walletCmd = &cobra.Command{
	Use:   "wallet",
	Short: "Wallet balance and address management",
}

var walletBalanceCmd = &cobra.Command{
	Use:   "balance",
	Short: "Show wallet balance",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/wallet/balance")
		if err != nil {
			return err
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

var walletAddressCmd = &cobra.Command{
	Use:   "address",
	Short: "Show wallet address",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/wallet/address")
		if err != nil {
			return err
		}
		cmdutil.PrintJSON(data)
		return nil
	},
}

var walletShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show wallet summary with treasury policy",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := cmdutil.APIGet("/api/wallet/balance")
		if err != nil {
			return err
		}

		// Address.
		address := ""
		if a, ok := data["address"].(string); ok {
			address = a
		} else {
			// Fall back to separate address endpoint.
			if addrData, err := cmdutil.APIGet("/api/wallet/address"); err == nil {
				if a, ok := addrData["address"].(string); ok {
					address = a
				}
			}
		}
		if address != "" {
			fmt.Printf("Wallet Address: %s\n", address)
		}

		// Chain.
		chain := ""
		chainID := ""
		if c, ok := data["chain"].(string); ok {
			chain = c
		}
		if id, ok := data["chain_id"].(float64); ok {
			chainID = fmt.Sprintf("%.0f", id)
		} else if id, ok := data["chain_id"].(string); ok {
			chainID = id
		}
		if chain != "" || chainID != "" {
			if chain != "" && chainID != "" {
				fmt.Printf("Chain: %s (%s)\n", chain, chainID)
			} else if chain != "" {
				fmt.Printf("Chain: %s\n", chain)
			} else {
				fmt.Printf("Chain: %s\n", chainID)
			}
		}

		// Balance.
		balance := "0.00"
		token := "USDC"
		if b, ok := data["balance"].(string); ok {
			balance = b
		} else if b, ok := data["balance"].(float64); ok {
			balance = fmt.Sprintf("%.2f", b)
		}
		if t, ok := data["token"].(string); ok && t != "" {
			token = t
		}
		fmt.Printf("Balance: %s %s\n", balance, token)

		// Treasury policy.
		treasury, _ := data["treasury"].(map[string]any)
		if treasury == nil {
			treasury, _ = data["treasury_policy"].(map[string]any)
		}
		if treasury != nil {
			fmt.Println()
			fmt.Println("Treasury Policy:")
			if cap := fmtDollar(treasury, "per_payment_cap"); cap != "" {
				fmt.Printf("  Per-payment cap: %s\n", cap)
			}
			if budget := fmtDollar(treasury, "daily_budget"); budget != "" {
				fmt.Printf("  Daily budget: %s\n", budget)
			}
			if reserve := fmtDollar(treasury, "minimum_reserve"); reserve != "" {
				fmt.Printf("  Minimum reserve: %s\n", reserve)
			}
		}

		// Tokens.
		fmt.Println()
		fmt.Println("Tokens:")
		tokens, _ := data["tokens"].([]any)
		if len(tokens) == 0 {
			fmt.Println("  (none cached)")
		} else {
			for _, t := range tokens {
				tm, _ := t.(map[string]any)
				symbol, _ := tm["symbol"].(string)
				bal := "0.00"
				if b, ok := tm["balance"].(string); ok {
					bal = b
				} else if b, ok := tm["balance"].(float64); ok {
					bal = fmt.Sprintf("%.2f", b)
				}
				fmt.Printf("  %s: %s\n", symbol, bal)
			}
		}

		return nil
	},
}

// fmtDollar extracts a numeric value from a map and formats it as a dollar amount.
func fmtDollar(m map[string]any, key string) string {
	if v, ok := m[key].(float64); ok {
		return fmt.Sprintf("$%.2f", v)
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func init() {
	walletCmd.AddCommand(walletBalanceCmd, walletAddressCmd, walletShowCmd)}
