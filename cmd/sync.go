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

func runSync(cmd *cobra.Command, args []string, verbose bool) (err error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	renderer := terminal.NewSyncProgressRenderer(cmd.OutOrStdout(), verbose)
	if err := renderer.Start(); err != nil {
		return err
	}
	defer func() {
		if err == nil {
			err = renderer.Close()
			if err != nil {
				err = fmt.Errorf("close sync progress: %w", err)
			}
			return
		}

		if errors.Is(err, context.Canceled) {
			err = joinSyncError(err, renderer.Cancel(err))
			return
		}

		err = joinSyncError(err, renderer.Failure(err))
	}()

	if err := renderer.StartPhase(terminal.PhaseSSH); err != nil {
		return err
	}
	tunnel := newTunnel(cfg.SSH, defaultSourceDBAddr)
	if err := tunnel.Open(ctx); err != nil {
		return fmt.Errorf("open ssh tunnel: %w", err)
	}
	defer func() {
		err = joinSyncError(err, tunnel.Close())
	}()
	if err := renderer.CompletePhase(terminal.PhaseSSH); err != nil {
		return err
	}

	if err := renderer.StartPhase(terminal.PhaseSource); err != nil {
		return err
	}
	sourceClient, err := connectSource(cfg.Source, tunnel.LocalAddress())
	if err != nil {
		return fmt.Errorf("connect source database: %w", err)
	}
	defer func() {
		err = joinSyncError(err, sourceClient.Close())
	}()
	if err := sourceClient.Ping(ctx); err != nil {
		return err
	}
	if err := renderer.CompletePhase(terminal.PhaseSource); err != nil {
		return err
	}

	if err := renderer.StartPhase(terminal.PhaseTarget); err != nil {
		return err
	}
	targetClient, err := connectTarget(cfg.Target)
	if err != nil {
		return fmt.Errorf("connect target database: %w", err)
	}
	defer func() {
		err = joinSyncError(err, targetClient.Close())
	}()
	if err := targetClient.Ping(ctx); err != nil {
		return err
	}
	if err := renderer.CompletePhase(terminal.PhaseTarget); err != nil {
		return err
	}

	if err := renderer.StartPhase(terminal.PhasePlan); err != nil {
		return err
	}
	plan, err := newPlanner(cfg, sourceClient).Build(ctx, args)
	if err != nil {
		return err
	}
	if err := renderer.SetPlan(plan); err != nil {
		return err
	}
	if err := renderer.CompletePhase(terminal.PhasePlan); err != nil {
		return err
	}

	if err := targetClient.PrepareSyncSession(ctx); err != nil {
		return fmt.Errorf("prepare target sync session: %w", err)
	}

	if err := renderer.StartPhase(terminal.PhaseSchema); err != nil {
		return err
	}
	if err := newSchemaSyncer(sourceClient, targetClient).Sync(ctx, plan); err != nil {
		return err
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
		return err
	}
	if renderErr != nil {
		return fmt.Errorf("render sync progress: %w", renderErr)
	}
	if err := renderer.CompletePhase(terminal.PhaseData); err != nil {
		return err
	}

	return nil
}

func joinSyncError(current, next error) error {
	if next == nil {
		return current
	}
	if current == nil {
		return next
	}
	return errors.Join(current, next)
}
