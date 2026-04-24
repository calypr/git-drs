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
		Use:   "add-url <cloud-url> [path]",
		Short: "Add a file to the Git DRS repo using a cloud object URL",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return errors.New("usage: add-url <cloud-url> [path]")
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
}

// runAddURL is the Cobra RunE wrapper that delegates execution to the service.
func runAddURL(cmd *cobra.Command, args []string) (err error) {
	return NewAddURLService().Run(cmd, args)
}
