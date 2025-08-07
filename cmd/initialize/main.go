package initialize

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/calypr/data-client/data-client/jwt"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/utils"
	"github.com/spf13/cobra"
)

var (
	mode         string
	apiEndpoint  string
	bucket       string
	credFile     string
	fenceToken   string
	profile      string
	project      string
	terraProject string
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize repo and server access for git-drs",
	Long:  "Initialize repo and server access required for git-drs. Defaults to gen3 server unless a --mode is specified. See below for the flag requirements for each server",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		return Init(mode, apiEndpoint, bucket, credFile, fenceToken, profile, project, terraProject)
	},
}

func init() {
	Cmd.Flags().StringVar(&mode, "mode", "gen3", "Options for DRS server: gen3 or anvil. Defaults to gen3")
	Cmd.Flags().StringVar(&apiEndpoint, "url", "", "[gen3] Specify the API endpoint of the data commons")
	Cmd.Flags().StringVar(&bucket, "bucket", "", "[gen3] Specify the bucket name")
	Cmd.Flags().StringVar(&credFile, "cred", "", "[gen3] Specify the gen3 credential file that you want to use")
	Cmd.Flags().StringVar(&fenceToken, "token", "", "[gen3] Specify the token to be used as a replacement for a credential file for temporary access")
	Cmd.Flags().StringVar(&profile, "profile", "", "[gen3] Specify the gen3 profile to use")
	Cmd.Flags().StringVar(&project, "project", "", "[gen3] Specify the gen3 project ID in the format <program>-<project>")
	Cmd.Flags().StringVar(&terraProject, "terraProject", "", "[AnVIL] Specify the Terra project ID")
}

func Init(mode string, apiEndpoint string, bucket string, credFile string, fenceToken string, profile string, project string, terraProject string) error {
	// validate mode

	err := config.IsValidServerType(mode)
	if err != nil {
		return err
	}

	// setup logging
	logg, err := client.NewLogger("", true)
	if err != nil {
		return err
	}
	defer logg.Close()

	// check if .git dir exists to ensure you're in a git repository
	_, err = utils.GitTopLevel()
	if err != nil {
		return fmt.Errorf("Error: not in a git repository. Please run this command in the root of your git repository.\n")
	}

	// if anvilMode is not set, ensure all other flags are provided
	switch mode {
	case string(config.Gen3ServerType):
		if profile == "" || (credFile == "" && fenceToken == "") || apiEndpoint == "" || project == "" || bucket == "" {
			return fmt.Errorf("Error: --profile, --url, --project, and --bucket are required, as well as --cred or --token, for gen3 setup. See 'git drs init --help' for details.\n")
		}

		err = gen3Init(profile, credFile, apiEndpoint, project, bucket, logg)
		if err != nil {
			return fmt.Errorf("Error configuring gen3 server: %v", err)
		}
	case string(config.AnvilServerType):
		if terraProject == "" {
			return fmt.Errorf("Error: --terraProject is required for anvil mode. See 'git drs init --help' for details.\n")
		}

		err = anvilInit(terraProject, logg)
		if err != nil {
			return fmt.Errorf("Error configuring anvil server: %v", err)
		}
	}

	// add .drs/objects to .gitignore if not already present
	if err := ensureDrsObjectsIgnore(config.DRS_OBJS_PATH, logg); err != nil {
		return fmt.Errorf("Init Error: %v\n", err)
	}

	// final logs
	logg.Log("Git DRS initialized successfully!")
	logg.Log("To stage any configuration changes, use 'git add .drs/config.yaml'")
	return nil
}

func gen3Init(profile string, credFile string, apiEndpoint string, project string, bucket string, log *client.Logger) error {
	// update config.yaml with gen3 server info
	serversMap := &config.ServersMap{
		Gen3: &config.Gen3Server{
			Endpoint: apiEndpoint,
			Auth: config.Gen3Auth{
				Profile:   profile,
				ProjectID: project,
				Bucket:    bucket,
			},
		},
	}
	cfg, err := config.UpdateServer(serversMap)
	if err != nil {
		return fmt.Errorf("Error: unable to update config file: %v\n", err)
	}
	log.Logf("Current server set to %s\n", cfg.CurrentServer)

	// init git config
	err = initGitConfig(config.Gen3ServerType)
	if err != nil {
		return err
	}

	// Create .git/hooks/pre-commit file
	hooksDir := filepath.Join(".git", "hooks")
	preCommitPath := filepath.Join(hooksDir, "pre-commit")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("[ERROR] unable to create pre-commit hook file: %v", err)
	}
	hookContent := "#!/bin/sh\ngit drs precommit\n"
	if err := os.WriteFile(preCommitPath, []byte(hookContent), 0755); err != nil {
		return fmt.Errorf("[ERROR] unable to write to pre-commit hook: %v", err)
	}

	// Call jwt.UpdateConfig with CLI parameters
	err = jwt.UpdateConfig(profile, apiEndpoint, credFile, fenceToken, "false", "")
	if err != nil {
		errStr := fmt.Sprintf("[ERROR] unable to configure your gen3 profile: %v", err)
		if strings.Contains(errStr, apiEndpoint) {
			errStr += " If you are accessing an internal instance, make sure you are on the right network."
		}
		return fmt.Errorf(errStr)
	}

	return nil

}

func anvilInit(terraProject string, log *client.Logger) error {
	// populate anvil config
	serversMap := &config.ServersMap{
		Anvil: &config.AnvilServer{
			Endpoint: client.ANVIL_ENDPOINT,
			Auth: config.AnvilAuth{
				TerraProject: terraProject,
			},
		},
	}
	cfg, err := config.UpdateServer(serversMap)
	if err != nil {
		return fmt.Errorf("Error: unable to update config file: %v\n", err)
	}
	log.Logf("Current server set to %s\n", cfg.CurrentServer)

	// init git config for anvil
	err = initGitConfig(config.AnvilServerType)
	if err != nil {
		return err
	}

	// remove the pre-commit hook if it exists
	hooksDir := filepath.Join(".git", "hooks")
	preCommitPath := filepath.Join(hooksDir, "pre-commit")
	if _, err := os.Stat(preCommitPath); err == nil {
		if err := os.Remove(preCommitPath); err != nil {
			log.Log("[ERROR] unable to remove pre-commit hook:", err)
			return err
		}
	}

	return nil
}

func initGitConfig(mode config.ServerType) error {
	var cmdName string
	var allowIncompletePush string
	switch mode {
	case config.Gen3ServerType:
		cmdName = "transfer"
		allowIncompletePush = "false"
	case config.AnvilServerType:
		cmdName = "transfer-ref"
		allowIncompletePush = "true"
	}

	configs := [][]string{
		{"lfs.standalonetransferagent", "gen3"},
		{"lfs.customtransfer.gen3.path", "git-drs"},
		{"lfs.customtransfer.gen3.concurrent", "false"},
		{"lfs.customtransfer.gen3.args", cmdName},
		{"lfs.allowincompletepush", allowIncompletePush},
	}

	for _, args := range configs {
		cmd := exec.Command("git", "config", args[0], args[1])
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("Unable to set git config %s: %v", args[0], err)
		}
	}

	return nil
}

// ensureDrsObjectsIgnore ensures that ".drs/objects" is ignored in .gitignore.
// It creates the file if it doesn't exist, and adds the line if not present.
func ensureDrsObjectsIgnore(ignorePattern string, logger *client.Logger) error {
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
		logger.Log(config.DRS_OBJS_PATH, "already present in .gitignore")
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

	logger.Log("Added", config.DRS_OBJS_PATH, "to .gitignore")
	return nil
}
