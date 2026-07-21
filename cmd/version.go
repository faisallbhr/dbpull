package cmd

import (
	"fmt"

	"github.com/faisallbhr/dbpull/internal/buildinfo"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the DBPull version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), buildinfo.Summary())
			return err
		},
	}
}
