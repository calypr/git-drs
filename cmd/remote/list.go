package remote

import (
	"fmt"

	"github.com/calypr/data-client/credentials"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	syconf "github.com/calypr/syfon/client/config"
	"github.com/spf13/cobra"
)

var (
	loadConfig            = config.LoadConfig
	loadProfileCredential = func(profile string) (*syconf.Credential, error) {
		return syconf.NewConfigure(drslog.GetLogger()).Load(profile)
	}
	ensureValidCredential = credentials.EnsureValidCredential
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List DRS repos",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts no arguments, received %d\n\nUsage: %s\n\nSee 'git drs remote list --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()
		cfg, err := loadConfig()
		if err != nil {
			logg.Debug(fmt.Sprintf("Error loading config: %s", err))
			return err
		}

		for name, remoteSelect := range cfg.Remotes {
			// Determine if this is the default
			isDefault := name == cfg.DefaultRemote
			marker := " "
			if isDefault {
				marker = "*"
			}

			// Determine remote type and endpoint
			var remoteType string
			var remote config.DRSRemote
			if remoteSelect.Gen3 != nil {
				remoteType = string(config.Gen3ServerType)
				remote = remoteSelect.Gen3
			} else if remoteSelect.Local != nil {
				remoteType = string(config.LocalServerType)
				remote = remoteSelect.Local
			} else {
				remoteType = "unknown"
			}

			endpoint := "N/A"
			if remote != nil {
				endpoint = remote.GetEndpoint()
			}

			fmt.Printf("%s %-10s %-8s %s\n", marker, name, remoteType, endpoint)
			if remoteSelect.Gen3 != nil {
				cred, err := loadProfileCredential(string(name))
				if err != nil {
					logg.Warn(fmt.Sprintf("remote %s credential check skipped: %v", name, err))
					continue
				}
				if err := ensureValidCredential(cmd.Context(), cred, logg); err != nil {
					logg.Warn(config.WrapCredentialValidationError(string(name), err).Error())
				}
			}
		}
		return nil
	},
}
