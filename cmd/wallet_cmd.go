package cmd

import (
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
		data, err := apiGet("/api/wallet/balance")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var walletAddressCmd = &cobra.Command{
	Use:   "address",
	Short: "Show wallet address",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := apiGet("/api/wallet/address")
		if err != nil {
			return err
		}
		printJSON(data)
		return nil
	},
}

var walletShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show wallet balance and address",
	RunE: func(cmd *cobra.Command, args []string) error {
		balance, err := apiGet("/api/wallet/balance")
		if err != nil {
			return err
		}
		address, err := apiGet("/api/wallet/address")
		if err != nil {
			return err
		}
		// Merge both maps.
		merged := make(map[string]any)
		for k, v := range balance {
			merged[k] = v
		}
		for k, v := range address {
			merged[k] = v
		}
		printJSON(merged)
		return nil
	},
}

func init() {
	walletCmd.AddCommand(walletBalanceCmd, walletAddressCmd, walletShowCmd)
	rootCmd.AddCommand(walletCmd)
}
