package version

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Cmd represents the "version" command
var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Get version",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("git-drs", buildVersion())
	},
}

func buildVersion() string {
	tag := ""
	commit := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			tag = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				commit = setting.Value
			case "vcs.tag":
				if tag == "" {
					tag = setting.Value
				}
			}
		}
	}

	commitShort := commit
	if len(commitShort) > 7 {
		commitShort = commitShort[:7]
	}

	switch {
	case tag != "" && commitShort != "":
		return fmt.Sprintf("%s-%s", tag, commitShort)
	case tag != "":
		return tag
	case commitShort != "":
		return fmt.Sprintf("dev-%s", commitShort)
	default:
		return "dev-unknown"
	}
}
