package add

import "github.com/spf13/cobra"

var (
	credFile       string
	fenceToken     string
	selectedBucket string
	localPassword  string
	localUsername  string
	noSkipSmudge   bool
	terraEndpoint  string
	terraAuth      string
	terraMode      string
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "add",
	Short: "add server access for git-drs",
}

func init() {
	Gen3Cmd.Flags().StringVar(&credFile, "cred", "", "[gen3] Import a Gen3 credential file into this profile")
	Gen3Cmd.Flags().StringVar(&fenceToken, "token", "", "[gen3] Use a temporary bearer token issued from fence")
	Gen3Cmd.Flags().StringVar(&selectedBucket, "bucket", "", "[gen3] Select a specific visible bucket when multiple buckets match the scope")
	Gen3Cmd.Flags().BoolVar(&noSkipSmudge, "no-skip-smudge", false, "Disable skipping smudge filter (force downloading file contents during checkout)")

	Cmd.AddCommand(Gen3Cmd)
	LocalCmd.Flags().StringVar(&selectedBucket, "bucket", "", "Select a specific visible bucket when multiple buckets match the scope")
	LocalCmd.Flags().StringVar(&localUsername, "username", "", "Username for local DRS HTTP basic auth")
	LocalCmd.Flags().StringVar(&localPassword, "password", "", "Password for local DRS HTTP basic auth")
	LocalCmd.Flags().BoolVar(&noSkipSmudge, "no-skip-smudge", false, "Disable skipping smudge filter (force downloading file contents during checkout)")
	Cmd.AddCommand(LocalCmd)

	TerraCmd.Flags().StringVar(&terraEndpoint, "drs-endpoint", "", "Terra DRS service base URL")
	TerraCmd.Flags().StringVar(&terraAuth, "auth", "", "Terra authentication method (google-adc)")
	TerraCmd.Flags().StringVar(&terraMode, "mode", "", "Terra remote mode (read-only)")
	Cmd.AddCommand(TerraCmd)
}
