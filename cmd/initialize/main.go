package initialize

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/calypr/data-client/client/jwt"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/utils"
	"github.com/spf13/cobra"
)

var (
	server       string
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
	Long: "Description:" +
		"\n  Initialize repo and server access for git-drs with a gen3 or AnVIL server, gen3 as default" +
		"\n  How to Use:" +
		"\n   ~ gen3 first init: provide a --url, --bucket, --profile, --project, and either a --cred or --token flag" +
		"\n   ~ general gen3 inits: just pass in a --cred or --token flag" +
		"\n   ~ AnVIL first init: set --server as anvil and provide a --terraProject" +
		"\n   ~ general AnVIL inits: set --server as anvil" +
		"\n   ~ See below for the flag requirements for each server",
	Args: cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		return Init(server, apiEndpoint, bucket, credFile, fenceToken, profile, project, terraProject)
	},
}

func init() {
	Cmd.Flags().StringVar(&server, "server", "gen3", "Options for DRS server: gen3 or anvil")
	Cmd.Flags().StringVar(&apiEndpoint, "url", "", "[gen3] Specify the API endpoint of the data commons")
	Cmd.Flags().StringVar(&bucket, "bucket", "", "[gen3] Specify the bucket name")
	Cmd.Flags().StringVar(&credFile, "cred", "", "[gen3] Specify the gen3 credential file that you want to use")
	Cmd.Flags().StringVar(&fenceToken, "token", "", "[gen3] Specify the token to be used as a replacement for a credential file for temporary access")
	Cmd.Flags().StringVar(&profile, "profile", "", "[gen3] Specify the gen3 profile to use")
	Cmd.Flags().StringVar(&project, "project", "", "[gen3] Specify the gen3 project ID in the format <program>-<project>")
	Cmd.Flags().StringVar(&terraProject, "terraProject", "", "[AnVIL] Specify the Terra project ID")
}

func Init(server string, apiEndpoint string, bucket string, credFile string, fenceToken string, profile string, project string, terraProject string) error {
	// validate server

	err := config.IsValidServerType(server)
	if err != nil {
		return err
	}

	if server == "gen3" && (bucket == "" || project == "" || profile == "") {
		return fmt.Errorf("Error: bucket, project and profile must be configured for initialize to work.")
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

	// create config file if it doesn't exist
	err = config.CreateEmptyConfig()
	if err != nil {
		return fmt.Errorf("Error: unable to create config file: %v\n", err)
	}

	// load the config
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("Error: unable to load config file: %v\n", err)
	}

	// if anvilMode is not set, ensure all other flags are provided
	switch server {
	case string(config.Gen3ServerType):
		// if the config file is missing anything, require all gen3 params
		if cfg.Servers.Gen3 == nil || cfg.Servers.Gen3.Auth.Bucket == "" || cfg.Servers.Gen3.Auth.ProjectID == "" {
			if bucket == "" || project == "" || profile == "" {
				return fmt.Errorf("Error: No gen3 server configured yet. Please provide a --profile, --project, and --bucket, as well as either a --cred or --token. See 'git drs init --help' for more details")
			}
		}

		err = gen3Init(profile, credFile, fenceToken, project, bucket, logg)
		if err != nil {
			return fmt.Errorf("Error configuring gen3 server: %v", err)
		}
	case string(config.AnvilServerType):
		// ensure either terraProject is provided or already in config
		if terraProject == "" && (cfg.Servers.Anvil == nil || cfg.Servers.Anvil.Auth.TerraProject == "") {
			return fmt.Errorf("Error: --terraProject is required for anvil mode. See 'git drs init --help' for details.\n")
		}

		err = anvilInit(terraProject, logg)
		if err != nil {
			return fmt.Errorf("Error configuring anvil server: %v", err)
		}
	}

	// add some patterns to the .gitignore if not already present
	configStr := "!" + filepath.Join(config.DRS_DIR, config.CONFIG_YAML)
	drsDirStr := fmt.Sprintf("%s/**", config.DRS_DIR)

	gitignorePatterns := []string{drsDirStr, configStr, "drs_downloader.log"}
	for _, pattern := range gitignorePatterns {
		if err := ensureDrsObjectsIgnore(pattern, logg); err != nil {
			return fmt.Errorf("Init Error: %v\n", err)
		}
	}

	// log message based on if .gitignore is untracked or modified (i.e. if we actually made changes something)
	statusCmd := exec.Command("git", "status", "--porcelain", ".gitignore")
	output, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("Error checking git status of .gitignore file: %v", err)
	}
	if len(output) > 0 {
		logg.Log(".gitignore has been updated and staged")
	} else {
		logg.Log(".gitignore already up to date")
	}

	// git add .gitignore
	cmd := exec.Command("git", "add", ".gitignore")
	if cmdOut, err := cmd.Output(); err != nil {
		return fmt.Errorf("Error adding .gitignore to git: %s", cmdOut)
	}

	// final logs
	logg.Log("Git DRS initialized successfully!")
	logg.Log("To stage any configuration changes, use 'git add .drs/config.yaml'")
	return nil
}

func gen3Init(profile string, credFile string, fenceToken string, project string, bucket string, log *client.Logger) error {
	// double check that one of the credentials params is provided

	var err error
	if fenceToken == "" {
		cred := jwt.Configure{}
		if credFile == "" {
			client.ProfileConfig, err = cred.ParseConfig(profile)
			fenceToken = client.ProfileConfig.AccessToken
		} else {
			optCredential, err := cred.ReadCredentials(credFile, "")
			if err != nil {
				return err
			}
			client.ProfileConfig = *optCredential
			fenceToken = optCredential.AccessToken
		}
		apiEndpoint, err = utils.ParseAPIEndpointFromToken(client.ProfileConfig.APIKey)
		if err != nil {
			return err
		}
	}
	if apiEndpoint == "" {
		apiEndpoint, err = utils.ParseAPIEndpointFromToken(fenceToken)
		if err != nil {
			return err
		}
	}

	if credFile == "" && fenceToken == "" {
		return fmt.Errorf("Error: Gen3 requires a credentials file or accessToken to setup project locally")
	}

	// if all of the necessary params are filled, then configure the gen3 server
	firstTimeSetup := apiEndpoint != "" && project != "" && bucket != "" && profile != ""
	if firstTimeSetup {
		// update config file with gen3 server info
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
		_, err := config.UpdateServer(serversMap)
		if err != nil {
			return fmt.Errorf("Error: unable to update config file with the requested parameters: %v\n", err)
		}
	}

	// update current server in config
	cfg, err := config.UpdateCurrentServer(config.Gen3ServerType)
	if err != nil {
		return fmt.Errorf("Error: unable to update current server to gen3: %v\n", err)
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

	// authenticate with gen3
	// if no credFile is specified, don't go for the update
	if credFile != "" {
		cred := &jwt.Credential{
			Profile:            profile,
			APIEndpoint:        apiEndpoint,
			AccessToken:        fenceToken,
			UseShepherd:        "false",
			MinShepherdVersion: "",
			KeyId:              client.ProfileConfig.KeyId,
			APIKey:             client.ProfileConfig.APIKey,
		}
		err = jwt.UpdateConfig(cred)
		if err != nil {
			errStr := fmt.Sprintf("[ERROR] unable to configure your gen3 profile: %v", err)
			if strings.Contains(errStr, "apiendpoint") {
				errStr += " If you are accessing an internal website, make sure you are connected to the internal network."
			}
			return fmt.Errorf(errStr)
		}
	}

	return nil

}

func anvilInit(terraProject string, log *client.Logger) error {
	// make sure terra project is provided
	if terraProject != "" {
		// populate anvil config
		serversMap := &config.ServersMap{
			Anvil: &config.AnvilServer{
				Endpoint: client.ANVIL_ENDPOINT,
				Auth: config.AnvilAuth{
					TerraProject: terraProject,
				},
			},
		}
		_, err := config.UpdateServer(serversMap)
		if err != nil {
			return fmt.Errorf("Error: unable to update config file: %v\n", err)
		}
	}

	// update current server in config
	cfg, err := config.UpdateCurrentServer(config.AnvilServerType)
	if err != nil {
		return fmt.Errorf("Error: unable to update current server to AnVIL: %v\n", err)
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
		if cmdOut, err := cmd.Output(); err != nil {
			return fmt.Errorf("Unable to set git config %s: %s", args[0], cmdOut)
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
