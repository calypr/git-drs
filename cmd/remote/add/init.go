package add

import "github.com/spf13/cobra"

var (
	credFile      string
	fenceToken    string
	localPassword string
	localUsername string
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "add",
	Short: "add server access for git-drs",
}

func init() {
	Gen3Cmd.Flags().StringVar(&credFile, "cred", "", "[gen3] Import a Gen3 credential file into this profile")
	Gen3Cmd.Flags().StringVar(&fenceToken, "token", "", "[gen3] Use a temporary bearer token issued from fence")

	Cmd.AddCommand(Gen3Cmd)
	LocalCmd.Flags().StringVar(&localUsername, "username", "", "Username for local DRS HTTP basic auth")
	LocalCmd.Flags().StringVar(&localPassword, "password", "", "Password for local DRS HTTP basic auth")
	Cmd.AddCommand(LocalCmd)
}
