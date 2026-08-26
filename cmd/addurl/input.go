package addurl

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// addURLInput holds the parsed CLI state for the add-url command.
type addURLInput struct {
	sourceArg    string
	objectURL    string
	path         string
	sha256       string
	scheme       string
	recursive    bool
	dryRun       bool
	manifest     string
	remote       string
	explicitPath bool
}

// parseAddURLInput parses CLI args and flags into an addURLInput.
func parseAddURLInput(cmd *cobra.Command, args []string) (addURLInput, error) {
	sourceArg := strings.TrimSpace(args[0])

	pathArg, err := resolvePathArg(sourceArg, args)
	if err != nil {
		return addURLInput{}, err
	}

	sha256Param, err := cmd.Flags().GetString("sha256")
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag sha256: %w", err)
	}
	scheme, err := cmd.Flags().GetString("scheme")
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag scheme: %w", err)
	}
	recursive, _ := cmd.Flags().GetBool("recursive")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	manifest, _ := cmd.Flags().GetString("manifest")
	remote, _ := cmd.Flags().GetString("remote")

	return addURLInput{
		sourceArg:    sourceArg,
		path:         pathArg,
		sha256:       sha256Param,
		scheme:       strings.ToLower(strings.TrimSpace(scheme)),
		recursive:    recursive,
		dryRun:       dryRun,
		manifest:     strings.TrimSpace(manifest),
		remote:       strings.TrimSpace(remote),
		explicitPath: len(args) == 2,
	}, nil
}

// resolvePathArg returns the explicit destination path argument when provided,
// otherwise derives the worktree path from the given cloud URL or object key.
func resolvePathArg(sourceArg string, args []string) (string, error) {
	if len(args) == 2 {
		return args[1], nil
	}
	if looksLikeCloudURL(sourceArg) {
		u, err := url.Parse(sourceArg)
		if err != nil {
			return "", err
		}
		return strings.TrimPrefix(u.Path, "/"), nil
	}
	return strings.Trim(strings.TrimSpace(sourceArg), "/"), nil
}

func looksLikeCloudURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if strings.TrimSpace(u.Scheme) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(u.Scheme)) {
	case "s3", "gs", "gcs", "azblob", "http", "https", "globus":
		return strings.TrimSpace(u.Host) != ""
	default:
		return false
	}
}

func isGlobusURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && strings.EqualFold(u.Scheme, "globus")
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
