package addurl

import (
	"context"
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
		Use:   "add-url <s3-url> [path]",
		Short: "Add a file to the Git DRS repo using an S3 URL",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return errors.New("usage: add-url <s3-url> [path]")
			}
			return nil
		},
		RunE: runAddURL,
	}
	addFlags(cmd)
	return cmd
}

// addFlags registers command-line flags for AWS credentials, endpoint and an
// optional `sha256` expected checksum.
func addFlags(cmd *cobra.Command) {
	cmd.Flags().String(
		cloud.AWS_KEY_FLAG_NAME,
		os.Getenv(cloud.AWS_KEY_ENV_VAR),
		"AWS access key",
	)

	cmd.Flags().String(
		cloud.AWS_SECRET_FLAG_NAME,
		os.Getenv(cloud.AWS_SECRET_ENV_VAR),
		"AWS secret key",
	)

	cmd.Flags().String(
		cloud.AWS_REGION_FLAG_NAME,
		os.Getenv(cloud.AWS_REGION_ENV_VAR),
		"AWS S3 region",
	)

	cmd.Flags().String(
		cloud.AWS_ENDPOINT_URL_FLAG_NAME,
		os.Getenv(cloud.AWS_ENDPOINT_URL_ENV_VAR),
		"AWS S3 endpoint (optional, for Ceph/MinIO)",
	)

	// New flag: optional expected SHA256
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

// download uses cloud.AgentFetchReader to download the S3 object, returning
// the computed SHA256 and the path to the temporary downloaded file.
// The caller is responsible for moving/deleting the temporary file.
// we include this wrapper function to allow mocking in tests.
var download = func(ctx context.Context, info *cloud.S3Object, input cloud.S3ObjectParameters, lfsRoot string) (string, string, error) {
	return cloud.Download(ctx, info, input, lfsRoot)
}

// addURLInput parses CLI args and flags into an addURLInput, validates
// required AWS credentials and region, and constructs cloud.S3ObjectParameters.
type addURLInput struct {
	s3URL    string
	path     string
	sha256   string
	s3Params cloud.S3ObjectParameters
}

func parseAddURLInput(cmd *cobra.Command, args []string) (addURLInput, error) {
	s3URL := args[0]

	pathArg, err := resolvePathArg(s3URL, args)
	if err != nil {
		return addURLInput{}, err
	}

	sha256Param, err := cmd.Flags().GetString("sha256")
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag sha256: %w", err)
	}

	awsKey, err := cmd.Flags().GetString(cloud.AWS_KEY_FLAG_NAME)
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag %s: %w", cloud.AWS_KEY_FLAG_NAME, err)
	}
	awsSecret, err := cmd.Flags().GetString(cloud.AWS_SECRET_FLAG_NAME)
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag %s: %w", cloud.AWS_SECRET_FLAG_NAME, err)
	}
	awsRegion, err := cmd.Flags().GetString(cloud.AWS_REGION_FLAG_NAME)
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag %s: %w", cloud.AWS_REGION_FLAG_NAME, err)
	}
	awsEndpoint, err := cmd.Flags().GetString(cloud.AWS_ENDPOINT_URL_FLAG_NAME)
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag %s: %w", cloud.AWS_ENDPOINT_URL_FLAG_NAME, err)
	}

	if awsKey == "" || awsSecret == "" {
		return addURLInput{}, errors.New("AWS credentials must be provided via flags or environment variables")
	}
	if awsRegion == "" {
		return addURLInput{}, errors.New("AWS region must be provided via flag or environment variable")
	}

	s3Input := cloud.S3ObjectParameters{
		S3URL:           s3URL,
		AWSAccessKey:    awsKey,
		AWSSecretKey:    awsSecret,
		AWSRegion:       awsRegion,
		AWSEndpoint:     awsEndpoint,
		SHA256:          sha256Param,
		DestinationPath: pathArg,
	}

	return addURLInput{
		s3URL:    s3URL,
		path:     pathArg,
		sha256:   sha256Param,
		s3Params: s3Input,
	}, nil
}

// resolvePathArg returns the explicit destination path argument when provided,
// otherwise derives the worktree path from the given S3 URL path component.
func resolvePathArg(s3URL string, args []string) (string, error) {
	if len(args) == 2 {
		return args[1], nil
	}
	u, err := url.Parse(s3URL)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(u.Path, "/"), nil
}
