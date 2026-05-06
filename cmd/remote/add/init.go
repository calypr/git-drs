package add

import "github.com/spf13/cobra"

var (
	bucket        string
	credFile      string
	fenceToken    string
	localPassword string
	localUsername string
	project       string
	organization  string
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "add",
	Short: "add server access for git-drs",
}

func init() {
	Gen3Cmd.Flags().StringVar(&credFile, "cred", "", "[gen3] Import a Gen3 credential file into this profile")
	Gen3Cmd.Flags().StringVar(&fenceToken, "token", "", "[gen3] Use a temporary bearer token; the API endpoint is derived from the token issuer")

	Cmd.AddCommand(Gen3Cmd)
	LocalCmd.Flags().StringVarP(&project, "project", "p", "", "Project ID")
	LocalCmd.Flags().StringVar(&bucket, "bucket", "", "Bucket Name")
	LocalCmd.Flags().StringVar(&organization, "organization", "", "Organization Name")
	LocalCmd.Flags().StringVar(&localUsername, "username", "", "Username for local DRS HTTP basic auth")
	LocalCmd.Flags().StringVar(&localPassword, "password", "", "Password for local DRS HTTP basic auth")
	Cmd.AddCommand(LocalCmd)
}
