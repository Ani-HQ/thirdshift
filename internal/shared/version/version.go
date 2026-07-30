package version

import "fmt"

var (
	Version   = "0.1.0-alpha"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func String() string {
	return fmt.Sprintf("%s commit=%s date=%s", Version, Commit, BuildDate)
}
