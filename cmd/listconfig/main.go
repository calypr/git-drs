package listconfig

import (
	"encoding/json"
	"fmt"
	"os"

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
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load the current configuration
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if jsonOutput {
			// Output as JSON if requested
			encoder := json.NewEncoder(os.Stdout)
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
