package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newListTablesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-tables",
		Short: "List source base tables",
		Args:  cobra.NoArgs,
		RunE:  runListTables,
	}
}

func runListTables(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	tunnel := newTunnel(cfg.SSH, defaultSourceDBAddr)
	if err := tunnel.Open(ctx); err != nil {
		return fmt.Errorf("open ssh tunnel: %w", err)
	}
	defer tunnel.Close()

	sourceClient, err := connectSource(cfg.Source, tunnel.LocalAddress())
	if err != nil {
		return fmt.Errorf("connect source database: %w", err)
	}
	defer sourceClient.Close()

	tables, err := sourceClient.ListTables(ctx)
	if err != nil {
		return err
	}

	for _, table := range tables {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), table.Name); err != nil {
			return err
		}
	}

	return nil
}
