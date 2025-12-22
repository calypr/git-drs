package listconfig

import (
	"fmt"
	"os"

	"github.com/bytedance/sonic"
	"github.com/calypr/git-drs/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	jsonOutput bool
)

// Cmd represents the list-config command
var Cmd = &cobra.Command{
	Use:   "list-config",
	Short: "Display the current configuration",
	Long:  "Pretty prints the current configuration file in YAML format",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts no arguments, received %d\n\nUsage: %s\n\nSee 'git drs list-config --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load the current configuration
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if jsonOutput {
			// Output as JSON if requested
			encoder := sonic.ConfigFastest.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(cfg)
		} else {
			// Default YAML output with nice formatting
			encoder := yaml.NewEncoder(os.Stdout)
			encoder.SetIndent(2)
			defer encoder.Close()

			return encoder.Encode(cfg)
		}
	},
}

func init() {
	Cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "output in JSON format")
}
