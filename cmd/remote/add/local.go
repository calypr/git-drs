package add

import (
	"context"
	"fmt"
	"strings"

	"github.com/calypr/git-drs/cmd/initialize"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/spf13/cobra"
)

var LocalCmd = &cobra.Command{
	Use:   "local <remote-name> <url> <organization/project>",
	Short: "Add a local DRS server",
	Long:  "Add a local DRS server by specifying its base URL and scope. Optional --username/--password configures basic auth for helper flows.",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName := args[0]
		url := args[1]
		scopeArg := args[2]

		if err := initialize.EnsureInitialized(drslog.GetLogger()); err != nil {
			return fmt.Errorf("failed to initialize repository: %w", err)
		}
		if url == "" {
			return fmt.Errorf("URL cannot be empty")
		}
		organization, project, err := parseScopeArg(scopeArg)
		if err != nil {
			return err
		}
		scope, err := gitrepo.ResolveBucketScope(organization, project, "", "")
		if err != nil {
			scope, err = resolveBucketScopeFromLocalServer(context.Background(), url, strings.TrimSpace(localUsername), strings.TrimSpace(localPassword), organization, project, selectedBucket)
			if err != nil {
				return fmt.Errorf("failed resolving bucket mapping for organization=%q project=%q: %w", organization, project, err)
			}
		}
		resolvedBucket := strings.TrimSpace(scope.Bucket)
		if resolvedBucket == "" {
			return fmt.Errorf("no bucket mapping found for organization=%q project=%q", organization, project)
		}

		newConfig, err := persistLocalRemote(remoteName, url, organization, project, scope)
		if err != nil {
			return err
		}
		if err := configureLocalBasicAuth(remoteName, localUsername, localPassword); err != nil {
			return err
		}

		fmt.Printf("Added remote '%s'. Config: %v\n", remoteName, newConfig.GetRemote(config.Remote(remoteName)))
		if noSkipSmudge {
			if err := gitrepo.SetGitConfigOptions(map[string]string{"drs.skipsmudge": "false"}); err != nil {
				return fmt.Errorf("failed to configure skipsmudge: %w", err)
			}
		}
		return nil
	},
}
