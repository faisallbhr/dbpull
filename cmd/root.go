package cmd

import "github.com/spf13/cobra"

const appName = "dbpull"
const defaultConfigPath = "~/.config/dbpull/dbpull.yml"

var configPath = defaultConfigPath

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           appName,
		Short:         "Refresh a target MariaDB database from a remote source database over SSH",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath, "Path to DBPull config file")

	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newPlanCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newListTablesCmd())
	cmd.AddCommand(newVersionCmd())

	return cmd
}
