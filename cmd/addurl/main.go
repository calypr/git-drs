package addurl

import "github.com/spf13/cobra"

var Cmd = NewCommand()

// NewCommand constructs the Cobra command for the `add-url` subcommand,
// wiring usage, argument validation and the RunE handler.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-url <manifest.tsv>",
		Short: "Index existing S3 objects from a TSV manifest",
		Args:  cobra.ExactArgs(1),
		RunE:  runManifest,
	}
	addFlags(cmd)
	cmd.Flags().String("repo", ".", "Repository containing the git-drs remote configuration")
	cmd.Flags().Bool("commit", false, "Also create and commit Git pointers")
	cmd.Flags().Bool("git-only", false, "Verify existing Syfon records, commit pointers, and push the Git branch")
	cmd.Flags().String("branch", "", "Required current branch when using --commit")
	cmd.Flags().String("strip-prefix", "", "Remove this source prefix and resolve the remainder in the configured project scope")
	for _, name := range []string{"sha256", "scheme", "recursive", "manifest"} {
		_ = cmd.Flags().MarkHidden(name)
	}
	return cmd
}

// addFlags registers optional expected SHA256 checksum.
func addFlags(cmd *cobra.Command) {
	cmd.Flags().String(
		"sha256",
		"",
		"Expected SHA256 checksum (optional)",
	)
	cmd.Flags().String(
		"scheme",
		"",
		"Storage scheme for object-key mode (for example: s3 or gs)",
	)
	cmd.Flags().Bool("recursive", false, "Deprecated: Globus directories and wildcards are detected automatically")
	_ = cmd.Flags().MarkDeprecated("recursive", "Globus directories and wildcards are detected automatically")
	cmd.Flags().Bool("dry-run", false, "Probe and validate the manifest without writing records or a commit")
	cmd.Flags().String("manifest", "", "Optional TSV manifest supplying path, size, and sha256 for Globus collections")
	cmd.Flags().StringP("remote", "r", "", "Target DRS remote")
}
