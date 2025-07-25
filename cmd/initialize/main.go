package initialize

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bmeg/git-drs/client"
	"github.com/bmeg/git-drs/utils"
	"github.com/spf13/cobra"
	"github.com/uc-cdis/gen3-client/gen3-client/jwt"
)

var (
	anvilMode    bool
	apiEndpoint  string
	bucket       string
	credFile     string
	profile      string
	projectId    string
	terraProject string
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "init",
	Short: "initialize required setup for git-drs",
	Long:  "initialize hooks, config required for git-drs",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// check if .git dir exists to ensure you're in a git repository
		_, err := utils.GitTopLevel()
		if err != nil {
			return fmt.Errorf("Error: not in a git repository. Please run this command in the root of your git repository.\n")
		}

		// if anvilMode is not set, ensure all other flags are provided
		if !anvilMode {
			if profile == "" || credFile == "" || apiEndpoint == "" || projectId == "" || bucket == "" {
				return fmt.Errorf("Error: --profile, --cred, --apiendpoint, --projectId, and --bucket are required for gen3 setup. See 'git drs init --help' for details.\n")
			}
		}
		if anvilMode && terraProject == "" {
			return fmt.Errorf("Error: --terraProject is required for anvil mode. See 'git drs init --help' for details.\n")
		}

		// populate config if empty or does not exist
		// if !client.HasConfig() {
		// 	// create config file
		// 	if err := client.CreateConfig(); err != nil {
		// 		return fmt.Errorf("Error: unable to create config file: %v\n", err)
		// 	}
		// }
		var serversMap *client.ServersMap
		if anvilMode {
			// populate anvil config
			serversMap = &client.ServersMap{
				Anvil: &client.AnvilServer{
					Endpoint: utils.ANVIL_ENDPOINT,
					Auth: client.AnvilAuth{
						Type:         client.ANVIL_TYPE,
						TerraProject: terraProject,
					},
				},
			}
		} else {
			// populate gen3 config
			serversMap = &client.ServersMap{
				Gen3: &client.Gen3Server{
					Endpoint: apiEndpoint,
					Auth: client.Gen3Auth{
						Type:      client.GEN3_TYPE,
						Profile:   profile,
						ProjectID: projectId,
						Bucket:    bucket,
					},
				},
			}
		}
		if err := client.UpdateServer(serversMap); err != nil {
			return fmt.Errorf("Error: unable to update config file: %v\n", err)
		}

		// add .drs/objects to .gitignore if not already present
		if err := ensureDrsObjectsIgnore(client.DRS_OBJS_PATH); err != nil {
			return fmt.Errorf("Error: %v\n", err)
		}

		// set git config so git lfs uses either anvil/gen3 custom transfer agent
		var cmdName string
		if anvilMode {
			cmdName = "transfer-ref"
		} else {
			cmdName = "transfer"
		}

		configs := [][]string{
			{"lfs.standalonetransferagent", "gen3"},
			{"lfs.customtransfer.gen3.path", "git-drs"},
			{"lfs.customtransfer.gen3.concurrent", "false"},
			{"lfs.customtransfer.gen3.args", cmdName},
		}
		for _, cfg := range configs {
			cmd := exec.Command("git", "config", cfg[0], cfg[1])
			if err := cmd.Run(); err != nil {
				fmt.Printf("Error: unable to set git config %s: %v\n", cfg[0], err)
				return err
			}
		}

		// do platform-specific setup
		if anvilMode { // anvil setup
			// ensure that the custom transfer is skipped during git push
			cmd := exec.Command("git", "config", "lfs.allowincompletepush", "true")
			if err := cmd.Run(); err != nil {
				fmt.Println("[ERROR] unable to set git config lfs.allowincompletepush true:", err)
				return err
			}

			// remove the pre-commit hook if it exists
			hooksDir := filepath.Join(".git", "hooks")
			preCommitPath := filepath.Join(hooksDir, "pre-commit")
			if _, err := os.Stat(preCommitPath); err == nil {
				if err := os.Remove(preCommitPath); err != nil {
					fmt.Println("[ERROR] unable to remove pre-commit hook:", err)
					return err
				}
			}
		}
		if !anvilMode { // gen3 setup
			// set incomplete push to false if previously set by anvil mode
			cmd := exec.Command("git", "config", "lfs.allowincompletepush", "false")
			if err := cmd.Run(); err != nil {
				fmt.Println("[ERROR] unable to unset git config lfs.allowincompletepush:", err)
				return err
			}

			// Create .git/hooks/pre-commit file
			hooksDir := filepath.Join(".git", "hooks")
			preCommitPath := filepath.Join(hooksDir, "pre-commit")
			if err := os.MkdirAll(hooksDir, 0755); err != nil {
				fmt.Println("[ERROR] unable to create pre-commit hook file:", err)
				return err
			}
			hookContent := "#!/bin/sh\ngit drs precommit\n"
			if err := os.WriteFile(preCommitPath, []byte(hookContent), 0755); err != nil {
				fmt.Println("[ERROR] unable to write to pre-commit hook:", err)
				return err
			}

			// Call jwt.UpdateConfig with CLI parameters
			err := jwt.UpdateConfig(profile, apiEndpoint, credFile, "false", "")
			if err != nil {
				fmt.Printf("[ERROR] unable to configure your gen3 profile: %v\n", err)
				return err
			}
		}

		fmt.Println("Git DRS initialized successfully!")
		return nil
	},
}

func init() {
	Cmd.Flags().BoolVar(&anvilMode, "anvil", false, "Use anvil mode for initialization")
	Cmd.Flags().StringVar(&apiEndpoint, "apiendpoint", "", "Specify the API endpoint of the data commons")
	Cmd.Flags().StringVar(&bucket, "bucket", "", "Specify the bucket name")
	Cmd.Flags().StringVar(&credFile, "cred", "", "Specify the gen3 credential file that you want to use")
	Cmd.Flags().StringVar(&profile, "profile", "", "Specify the gen3 profile to use")
	Cmd.Flags().StringVar(&projectId, "projectId", "", "Specify the gen3 project ID in the format <program>-<project>")
	Cmd.Flags().StringVar(&terraProject, "terraProject", "", "Specify the Terra project ID")
}

// ensureDrsObjectsIgnore ensures that ".drs/objects" is ignored in .gitignore.
// It creates the file if it doesn't exist, and adds the line if not present.
func ensureDrsObjectsIgnore(ignorePattern string) error {
	const (
		gitignorePath = ".gitignore"
	)

	var found bool

	// Check if .gitignore exists
	var lines []string
	file, err := os.Open(gitignorePath)
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			// Normalize slashes for comparison, trim spaces
			if strings.TrimSpace(line) == ignorePattern {
				found = true
			}
			lines = append(lines, line)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading %s: %w", gitignorePath, err)
		}
	} else if os.IsNotExist(err) {
		// .gitignore doesn't exist, will create it
		lines = []string{}
	} else {
		return fmt.Errorf("could not open %s: %w", gitignorePath, err)
	}

	if found {
		return nil
	}

	// Add the ignore pattern (ensure a blank line before if file is not empty)
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	lines = append(lines, ignorePattern)

	// Write back the file
	f, err := os.OpenFile(gitignorePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("could not write to %s: %w", gitignorePath, err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for i, l := range lines {
		if i > 0 {
			_, _ = w.WriteString("\n")
		}
		_, _ = w.WriteString(l)
	}
	// Always end with a trailing newline
	_, _ = w.WriteString("\n")
	if err := w.Flush(); err != nil {
		return fmt.Errorf("error writing %s: %w", gitignorePath, err)
	}

	return nil
}
