package addurl

import (
	"errors"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

var Cmd = NewCommand()

// NewCommand constructs the Cobra command for the `add-url` subcommand,
// wiring usage, argument validation and the RunE handler.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-url <cloud-url> [path]",
		Short: "Add a file to the Git DRS repo from a cloud storage URL",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return errors.New("usage: add-url <cloud-url> [path]")
			}
			return nil
		},
		RunE: runAddURL,
	}
	cmd.Flags().String("sha256", "", "Expected SHA256 checksum (optional)")
	return cmd
}

// runAddURL is the Cobra RunE wrapper that delegates execution to the service.
func runAddURL(cmd *cobra.Command, args []string) error {
	return NewAddURLService().Run(cmd, args)
}

// resolvePathArg returns the explicit destination path argument when provided,
// otherwise derives the worktree path from the given cloud URL path component.
func resolvePathArg(rawURL string, args []string) (string, error) {
	if len(args) == 2 {
		return args[1], nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(u.Path, "/"), nil
}
