package add

import "github.com/spf13/cobra"

var (
	apiEndpoint  string
	bucket       string
	credFile     string
	fenceToken   string
	project      string
	organization string
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "add",
	Short: "add server access for git-drs",
}

func init() {
	Gen3Cmd.Flags().StringVar(&apiEndpoint, "url", "", "[gen3] Specify the API endpoint of the data commons")
	Gen3Cmd.Flags().StringVar(&bucket, "bucket", "", "[gen3] Specify the bucket name")
	Gen3Cmd.Flags().StringVar(&credFile, "cred", "", "[gen3] Specify the gen3 credential file that you want to use")
	Gen3Cmd.Flags().StringVar(&fenceToken, "token", "", "[gen3] Specify the token to be used as a replacement for a credential file for temporary access")
	Gen3Cmd.Flags().StringVar(&project, "project", "", "[gen3] Specify the gen3 project ID in the format <program>-<project>")

	Cmd.AddCommand(Gen3Cmd)
	LocalCmd.Flags().StringVarP(&project, "project", "p", "", "Project ID")
	LocalCmd.Flags().StringVar(&bucket, "bucket", "", "Bucket Name")
	LocalCmd.Flags().StringVar(&organization, "organization", "", "Organization Name")
	Cmd.AddCommand(LocalCmd)
}
