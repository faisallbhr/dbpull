package cmd

import (
	"fmt"

	syncpkg "dbpull/internal/sync"

	"github.com/spf13/cobra"
)

func newPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Show what DBPull would synchronize without changing the target database",
		Args:  cobra.NoArgs,
		RunE:  runPlan,
	}
}

func runPlan(cmd *cobra.Command, args []string) error {
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

	plan, err := newPlanner(cfg, sourceClient).Build(ctx, nil)
	if err != nil {
		return err
	}

	return printPlan(cmd, plan)
}

func printPlan(cmd *cobra.Command, plan syncpkg.SyncPlan) error {
	out := cmd.OutOrStdout()

	var schemaOnly int
	for _, table := range plan.Tables {
		if table.DataExcluded {
			schemaOnly++
		}
	}

	tablesToSync := len(plan.Tables) - schemaOnly
	excluded := len(plan.Skipped)

	if _, err := fmt.Fprintf(out, "Tables to sync       %d\n", tablesToSync); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Schema only          %d\n", schemaOnly); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Excluded             %d\n", excluded); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Target database      %s\n", plan.TargetDatabase); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "No changes were made.")
	return err
}
