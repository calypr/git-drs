package add

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/calypr/git-drs/cmd/initialize"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/spf13/cobra"
)

var TerraCmd = &cobra.Command{
	Use:   "terra <remote-name>",
	Short: "Add a read-only Terra DRS remote",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName := strings.TrimSpace(args[0])
		if remoteName == "" {
			return fmt.Errorf("remote name is required")
		}
		endpoint, err := validateTerraOptions(terraEndpoint, terraAuth, terraMode)
		if err != nil {
			return err
		}
		if err := initialize.EnsureInitialized(drslog.GetLogger()); err != nil {
			return fmt.Errorf("failed to initialize repository: %w", err)
		}

		newConfig, err := config.UpdateRemote(config.Remote(remoteName), config.RemoteSelect{
			Terra: &config.TerraRemote{Endpoint: endpoint, Auth: terraAuth, Mode: terraMode},
		})
		if err != nil {
			return fmt.Errorf("failed to update remote config: %w", err)
		}
		fmt.Printf("Added remote '%s'. Config: %v\n", remoteName, newConfig.GetRemote(config.Remote(remoteName)))
		return nil
	},
}

func validateTerraOptions(endpoint, auth, mode string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("--drs-endpoint is required")
	}
	u, err := url.ParseRequestURI(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", fmt.Errorf("--drs-endpoint must be an HTTPS URL without credentials")
	}
	if auth != "google-adc" {
		return "", fmt.Errorf("--auth must be google-adc")
	}
	if mode != "read-only" {
		return "", fmt.Errorf("--mode must be read-only")
	}
	return u.String(), nil
}
