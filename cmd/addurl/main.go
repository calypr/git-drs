package addurl

import (
	"errors"

	"github.com/spf13/cobra"
)

var Cmd = NewCommand()

// NewCommand constructs the Cobra command for the `add-url` subcommand,
// wiring usage, argument validation and the RunE handler.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-url <object-url-or-key> [path]",
		Short: "Add a file from a provider URL or configured bucket object key",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return errors.New("usage: add-url <object-url-or-key> [path]")
			}
			return nil
		},
		RunE: runAddURL,
	}
	addFlags(cmd)
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
	cmd.Flags().Bool("dry-run", false, "List and validate matching Globus objects without writing files")
	cmd.Flags().String("manifest", "", "Optional TSV manifest supplying path, size, and sha256 for Globus collections")
	cmd.Flags().StringP("remote", "r", "", "Target DRS remote")
}

// runAddURL is the Cobra RunE wrapper that delegates execution to the service.
func runAddURL(cmd *cobra.Command, args []string) (err error) {
	return NewAddURLService().Run(cmd, args)
}
