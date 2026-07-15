package cmd

import (
	"dbpull/internal/config"

	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "View and update DBPull configuration interactively",
		Args:  cobra.NoArgs,
		RunE:  runConfig,
	}
}

func runConfig(cmd *cobra.Command, args []string) error {
	_, err := runConfigEditor(config.EditorOptions{
		Path:         configPath,
		CreateParent: true,
		Input:        cmd.InOrStdin(),
		Output:       cmd.OutOrStdout(),
	})
	return err
}
