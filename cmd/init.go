package cmd

import (
	"errors"
	"fmt"

	"github.com/faisallbhr/dbpull/internal/config"

	"github.com/spf13/cobra"
)

var runConfigEditor = config.RunEditor

func newInitCmd() *cobra.Command {
	var outputPath string
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create an initial DBPull configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, outputPath, force)
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path for the generated config file")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing config file")

	return cmd
}

func runInit(cmd *cobra.Command, outputPath string, force bool) error {
	path := outputPath
	explicitOutput := cmd.Flags().Changed("output")
	if path == "" {
		path = configPath
		explicitOutput = true
	}

	result, err := runConfigEditor(config.EditorOptions{
		Path:         path,
		Force:        force,
		RequireNew:   true,
		CreateParent: explicitOutput,
		Input:        cmd.InOrStdin(),
		Output:       cmd.OutOrStdout(),
	})
	if err != nil {
		return err
	}

	if !result.Saved {
		return errors.New("initialization canceled")
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), result.Path)
	return err
}
