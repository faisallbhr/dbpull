package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check whether DBPull can reach SSH and both databases",
		Args:  cobra.NoArgs,
		RunE:  runDoctor,
	}
}

func runDoctor(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	if _, err := os.Stat(cfg.SSH.PrivateKey); err != nil {
		return fmt.Errorf("check ssh key: %w", err)
	}
	printCheck(cmd, "SSH key")

	ctx, cancel := context.WithTimeout(cmd.Context(), doctorTimeout)
	defer cancel()

	tunnel := newTunnel(cfg.SSH, defaultSourceDBAddr)
	if err := tunnel.Open(ctx); err != nil {
		return fmt.Errorf("check ssh connection: %w", err)
	}
	defer tunnel.Close()

	printCheck(cmd, "SSH connection")
	printCheck(cmd, "Tunnel")

	sourceClient, err := connectSource(cfg.Source, tunnel.LocalAddress())
	if err != nil {
		return fmt.Errorf("check source database: %w", err)
	}
	defer sourceClient.Close()

	if err := sourceClient.Ping(ctx); err != nil {
		return fmt.Errorf("check source database: %w", err)
	}
	printCheck(cmd, "Source database")

	targetClient, err := connectTarget(cfg.Target)
	if err != nil {
		return fmt.Errorf("check target database: %w", err)
	}
	defer targetClient.Close()

	if err := targetClient.Ping(ctx); err != nil {
		return fmt.Errorf("check target database: %w", err)
	}
	printCheck(cmd, "Target database")

	return nil
}

func printCheck(cmd *cobra.Command, label string) {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ %s\n", label)
}
