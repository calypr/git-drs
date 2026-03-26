package addurl

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/calypr/git-drs/cloud"
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

// runAddURL is the Cobra RunE wrapper that delegates execution to the
func runAddURL(cmd *cobra.Command, args []string) (err error) {
	return NewAddURLService().Run(cmd, args)
}

// addURLInput parses CLI args and flags into an addURLInput and constructs
// cloud.ObjectParameters for metadata inspection.
type addURLInput struct {
	objectURL    string
	path         string
	sha256       string
	objectParams cloud.ObjectParameters
}

func parseAddURLInput(cmd *cobra.Command, args []string) (addURLInput, error) {
	objectURL := args[0]

	pathArg, err := resolvePathArg(objectURL, args)
	if err != nil {
		return addURLInput{}, err
	}

	sha256Param, err := cmd.Flags().GetString("sha256")
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag sha256: %w", err)
	}
	objectParams := cloud.ObjectParameters{
		ObjectURL:       objectURL,
		S3Region:        firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), os.Getenv("TEST_BUCKET_REGION")),
		S3Endpoint:      firstNonEmpty(os.Getenv("AWS_ENDPOINT_URL_S3"), os.Getenv("AWS_ENDPOINT_URL"), os.Getenv("TEST_BUCKET_ENDPOINT")),
		S3AccessKey:     firstNonEmpty(os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("TEST_BUCKET_ACCESS_KEY")),
		S3SecretKey:     firstNonEmpty(os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("TEST_BUCKET_SECRET_KEY")),
		SHA256:          sha256Param,
		DestinationPath: pathArg,
	}

	return addURLInput{
		objectURL:    objectURL,
		path:         pathArg,
		sha256:       sha256Param,
		objectParams: objectParams,
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

// resolvePathArg returns the explicit destination path argument when provided,
// otherwise derives the worktree path from the given cloud URL path component.
func resolvePathArg(objectURL string, args []string) (string, error) {
	if len(args) == 2 {
		return args[1], nil
	}
	u, err := url.Parse(objectURL)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(u.Path, "/"), nil
}
