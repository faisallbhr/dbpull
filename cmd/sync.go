package cmd

import (
	"context"
	"errors"
	"fmt"

	syncpkg "dbpull/internal/sync"
	"dbpull/internal/terminal"

	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "sync [tables...]",
		Short: "Synchronize the target database from the remote source database",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd, args, verbose)
		},
	}

	cmd.Flags().BoolVar(&verbose, "verbose", false, "Print verbose sync output")
	return cmd
}

func runSync(cmd *cobra.Command, args []string, verbose bool) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	renderer := terminal.NewSyncProgressRenderer(cmd.OutOrStdout(), verbose)
	if err := renderer.Start(); err != nil {
		return err
	}

	if err := renderer.StartPhase(terminal.PhaseSSH); err != nil {
		return err
	}
	tunnel := newTunnel(cfg.SSH, defaultSourceDBAddr)
	if err := tunnel.Open(ctx); err != nil {
		return finishSyncError(renderer, fmt.Errorf("open ssh tunnel: %w", err))
	}
	defer tunnel.Close()
	if err := renderer.CompletePhase(terminal.PhaseSSH); err != nil {
		return err
	}

	if err := renderer.StartPhase(terminal.PhaseSource); err != nil {
		return err
	}
	sourceClient, err := connectSource(cfg.Source, tunnel.LocalAddress())
	if err != nil {
		return finishSyncError(renderer, fmt.Errorf("connect source database: %w", err))
	}
	defer sourceClient.Close()
	if err := sourceClient.Ping(ctx); err != nil {
		return finishSyncError(renderer, err)
	}
	if err := renderer.CompletePhase(terminal.PhaseSource); err != nil {
		return err
	}

	if err := renderer.StartPhase(terminal.PhaseTarget); err != nil {
		return err
	}
	targetClient, err := connectTarget(cfg.Target)
	if err != nil {
		return finishSyncError(renderer, fmt.Errorf("connect target database: %w", err))
	}
	defer targetClient.Close()
	if err := targetClient.Ping(ctx); err != nil {
		return finishSyncError(renderer, err)
	}
	if err := renderer.CompletePhase(terminal.PhaseTarget); err != nil {
		return err
	}

	if err := renderer.StartPhase(terminal.PhasePlan); err != nil {
		return err
	}
	plan, err := newPlanner(cfg, sourceClient).Build(ctx, args)
	if err != nil {
		return finishSyncError(renderer, err)
	}
	if err := renderer.SetPlan(plan); err != nil {
		return err
	}
	if err := renderer.CompletePhase(terminal.PhasePlan); err != nil {
		return err
	}

	if err := targetClient.DisableForeignKeyChecks(ctx); err != nil {
		return finishSyncError(renderer, fmt.Errorf("disable foreign key checks: %w", err))
	}
	fkChecksDisabled := true
	defer func() {
		if fkChecksDisabled {
			_ = targetClient.EnableForeignKeyChecks(ctx)
		}
	}()

	if err := renderer.StartPhase(terminal.PhaseSchema); err != nil {
		return err
	}
	if err := newSchemaSyncer(sourceClient, targetClient).Sync(ctx, plan); err != nil {
		return finishSyncError(renderer, err)
	}
	if err := renderer.CompletePhase(terminal.PhaseSchema); err != nil {
		return err
	}

	if err := renderer.StartPhase(terminal.PhaseData); err != nil {
		return err
	}

	var renderErr error
	progress := func(update syncpkg.DataProgress) {
		if renderErr != nil {
			return
		}
		renderErr = renderer.Handle(update)
	}
	if err := newDataSyncer(sourceClient, targetClient, progress).Sync(ctx, plan, cfg.Sync.BatchSize); err != nil {
		if renderErr != nil {
			err = errors.Join(err, fmt.Errorf("render sync progress: %w", renderErr))
		}
		return finishSyncError(renderer, err)
	}
	if renderErr != nil {
		return finishSyncError(renderer, fmt.Errorf("render sync progress: %w", renderErr))
	}

	if err := targetClient.EnableForeignKeyChecks(ctx); err != nil {
		return finishSyncError(renderer, fmt.Errorf("enable foreign key checks: %w", err))
	}
	fkChecksDisabled = false
	if err := renderer.CompletePhase(terminal.PhaseData); err != nil {
		return err
	}
	if err := renderer.Close(); err != nil {
		return fmt.Errorf("close sync progress: %w", err)
	}

	return nil
}

func finishSyncError(renderer *terminal.SyncProgressRenderer, err error) error {
	var finalErr error
	if errors.Is(err, context.Canceled) {
		finalErr = renderer.Cancel(err)
	} else {
		finalErr = renderer.Failure(err)
	}

	if finalErr != nil && !errors.Is(finalErr, err) {
		return errors.Join(err, finalErr)
	}
	return finalErr
}
