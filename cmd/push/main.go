package push

import (
	"fmt"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/spf13/cobra"
)

var (
	stage bool
	all   bool
)

var Cmd = &cobra.Command{
	Use:   "push [remote-name]",
	Short: "push local objects to drs server.",
	Long:  "push local objects to drs server. This command ensures all local DRS records and their corresponding files are synced to the remote DRS server.",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs push --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		myLogger := drslog.GetLogger()
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		var remoteInput string
		if len(args) > 0 {
			remoteInput = args[0]
		}

		remote, err := cfg.GetRemoteOrDefault(remoteInput)
		if err != nil {
			return err
		}

		// Determine which Git remote name to use for discovery (dry-run)
		gitRemote := remoteInput
		if !gitrepo.IsGitRemote(gitRemote) {
			gitRemote = "origin"
		}

		drsClient, err := cfg.GetRemoteClient(remote, myLogger)
		if err != nil {
			return fmt.Errorf("error creating DRS client: %v", err)
		}

		remoteConfig := cfg.GetRemote(remote)
		if remoteConfig == nil {
			return fmt.Errorf("no configuration found for remote: %s", remote)
		}

		myLogger.Info(fmt.Sprintf("Updating DRS objects for remote '%s' (discovery via '%s')...", remote, gitRemote))
		builder := drs.NewObjectBuilder(remoteConfig.GetBucketName(), remoteConfig.GetProjectId())
		err = drsmap.UpdateDrsObjects(
			drsClient,
			builder,
			gitRemote,
			"",
			[]string{"HEAD"},
			all,
			myLogger,
		)
		if err != nil {
			return fmt.Errorf("error updating DRS objects: %v", err)
		}

		return drsmap.PushLocalDrsObjects(drsClient, myLogger, stage)
	},
}

func init() {
	Cmd.Flags().BoolVarP(&stage, "stage", "s", false, "Locally stage LFS objects from DRS server if they don't already exist in git index")
	Cmd.Flags().BoolVarP(&all, "all", "a", false, "Check all LFS-tracked files in the current branch for missing DRS records (slower)")
}
