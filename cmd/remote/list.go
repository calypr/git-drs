package remote

import (
	"fmt"

	calyprconf "github.com/calypr/calypr-cli/conf"
	"github.com/calypr/calypr-cli/credentials"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/spf13/cobra"
)

var (
	loadConfig            = config.LoadConfig
	loadProfileCredential = func(profile string) (*calyprconf.Credential, error) {
		return calyprconf.NewConfigure(drslog.GetLogger()).Load(profile)
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
			} else if remoteSelect.Terra != nil {
				remoteType = string(config.TerraServerType)
				remote = remoteSelect.Terra
			} else if remoteSelect.Generic != nil {
				remoteType = remoteSelect.Generic.Provider
				remote = remoteSelect.Generic
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
					logg.Warn(remoteruntime.WrapCredentialValidationError(string(name), err).Error())
				}
			}
		}
		return nil
	},
}
