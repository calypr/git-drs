package remote

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
	bucket     string
	credFile   string
	fenceToken string
	project    string
	ghRepo     string
)

var Remote = &cobra.Command{
	Use:   "remote", // The name the user types: 'remote'
	Short: "Manage git-drs remote servers",
	Long:  `Description: Commands for managing remote data repository servers (e.g., gen3, AnVIL).`,
}

var Cmd = &cobra.Command{
	Use:   "add <remote_name> <github_remote_location>",
	Short: "Add a remote to git-drs",
	Long: "Description:" +
		"\n  Add a remote to a repo and server access for git-drs with a gen3 or AnVIL server, gen3 as default" +
		"\n  How to Use:" +
		"\n   ~ First init: provide a --bucket, --project, and either a --cred or --token" +
		"\n   ~ refresh token re-add: just pass in a --cred or --token flag",
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		profile := args[0]
		ghremote := args[1]
		return Add(config.Profile(profile), ghremote, bucket, credFile, fenceToken, project)
	},
}

func init() {
	Remote.AddCommand(Cmd)
	Cmd.Flags().StringVar(&bucket, "bucket", "", "[gen3] Specify the bucket name")
	Cmd.Flags().StringVar(&credFile, "cred", "", "[gen3] Specify the gen3 credential file that you want to use")
	Cmd.Flags().StringVar(&fenceToken, "token", "", "[gen3] Specify the token to be used as a replacement for a credential file for temporary access")
	Cmd.Flags().StringVar(&project, "project", "", "[gen3] Specify the gen3 project ID in the format <program>-<project>")
}

func Add(profile config.Profile, ghRepo string, bucket string, credFile string, fenceToken string, project string) error {
	resp, err := utils.GitRemoteAdd(string(profile), ghRepo)
	if err != nil {
		return err
	}
	fmt.Println("REMOTE ADD RESP: ", resp)

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

	gsc, err := cfg.SelectGen3ServerConfig(profile)
	if err != nil {
		return err
	}

	// make sure at least one of the credentials params is provided
	if credFile == "" && fenceToken == "" && profile == "" {
		return fmt.Errorf("Error: Gen3 requires a credentials file or accessToken to setup project locally. Please provide either a --cred or --token flag. See 'git drs init --help' for more details")
	}

	if (gsc.Bucket == "" || gsc.ProjectID == "") ||
		(bucket == "" || project == "" || profile == "") {
		return fmt.Errorf("Error: No gen3 server configured yet. Please provide a --profile, --project, and --bucket, as well as either a --cred or --token. See 'git drs init --help' for more details")

	}

	err = gen3Init(profile, credFile, fenceToken, project, bucket, logg)
	if err != nil {
		return fmt.Errorf("Error configuring gen3 server: %v", err)
	}

	// add some patterns to the .gitignore if not already present
	configStr := "!" + filepath.Join(config.DRS_DIR, config.CONFIG_YAML)
	drsDirStr := fmt.Sprintf("%s/**", config.DRS_DIR)

	gitignorePatterns := []string{drsDirStr, configStr, "drs_downloader.log"}
	for _, pattern := range gitignorePatterns {
		if err := ensureDrsObjectsIgnore(pattern); err != nil {
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
	logg.Log("Git DRS configuration added to git.")
	logg.Log("Git DRS initialized successfully!")
	return nil
}

func gen3Init(profile config.Profile, credFile string, fenceToken string, project string, bucket string, log *client.Logger) error {
	// double check that one of the credentials params is provided

	var apiEndpoint string
	var err error
	if fenceToken == "" {
		cred := jwt.Configure{}
		if credFile == "" {
			client.ProfileConfig, err = cred.ParseConfig(string(profile))
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

	if fenceToken == "" {
		cred := jwt.Configure{}
		credential, err := cred.ReadCredentials(credFile, "")
		if err != nil {
			return err
		}
		fenceToken = credential.AccessToken
	}
	if apiEndpoint == "" {
		apiEndpoint, err = utils.ParseAPIEndpointFromToken(fenceToken)
	}

	// if all of the necessary params are filled, then configure the gen3 server
	firstTimeSetup := apiEndpoint != "" && project != "" && bucket != "" && profile != ""
	if firstTimeSetup {
		// update config file with gen3 server info
		serversMap := &config.ServersMap{
			Gen3: map[config.Profile]*config.Gen3Server{
				profile: {
					Endpoint:  apiEndpoint,
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

	// load existing config
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	// update current server in config
	cfg, err = cfg.UpdateConfigFromFile(config.Gen3ServerType)
	if err != nil {
		return fmt.Errorf("Error: unable to update current server to gen3: %v\n", err)
	}
	log.Logf("Current server set to %s\n", cfg.CurrentServer)

	// init git config
	err = initGitConfig()
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
			Profile:            string(profile),
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
			return fmt.Errorf("%s", errStr)
		}
	}

	return nil
}

func initGitConfig() error {
	configs := [][]string{
		{"lfs.standalonetransferagent", "gen3"},
		{"lfs.customtransfer.gen3.path", "git-drs"},
		{"lfs.customtransfer.gen3.concurrent", "false"},
		{"lfs.customtransfer.gen3.args", "transfer"},
		{"lfs.allowincompletepush", "false"},
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
