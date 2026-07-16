package buildinfo

import "fmt"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func Summary() string {
	return fmt.Sprintf(
		"DBPull\n\nVersion : %s\nCommit  : %s\nBuilt   : %s",
		Version,
		Commit,
		BuildDate,
	)
}
