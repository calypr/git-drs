package push

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/spf13/cobra"
)

var stage bool

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
			myLogger.Debug(fmt.Sprintf("Error loading config: %v", err))
			return err
		}

		var remote config.Remote
		if len(args) > 0 {
			remote = config.Remote(args[0])
		} else {
			remote, err = cfg.GetDefaultRemote()
			if err != nil {
				myLogger.Debug(fmt.Sprintf("Error getting default remote: %v", err))
				return err
			}
		}

		drsClient, err := cfg.GetRemoteClient(remote, myLogger)
		if err != nil {
			if gitrepo.IsGitRemote(string(remote)) {
				myLogger.Info(fmt.Sprintf("Remote '%s' is a Git remote. Recording LFS metadata and pushing to Git...", remote))

				// We need a DRS client to record metadata if it results in new records.
				// Use default DRS remote for metadata registration.
				defaultRemote, dErr := cfg.GetDefaultRemote()
				if dErr == nil {
					drsClient, dErr = cfg.GetRemoteClient(defaultRemote, myLogger)
					if dErr == nil {
						remoteConfig := cfg.GetRemote(defaultRemote)
						if remoteConfig != nil {
							builder := drs.NewObjectBuilder(remoteConfig.GetBucketName(), remoteConfig.GetProjectId())
							// Discover LFS files (including missing blobs via our fix) and update records
							drsmap.UpdateDrsObjects(builder, string(defaultRemote), "", []string{"HEAD"}, myLogger)
							// Push existing/new records to the default DRS server and stage if requested
							drsmap.PushLocalDrsObjects(drsClient, myLogger, stage)
						}
					}
				}

				// Now push to the actual Git remote
				myLogger.Info(fmt.Sprintf("Executing: git push %s HEAD", remote))
				cmd := exec.Command("git", "push", string(remote), "HEAD")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				return cmd.Run()
			}
			myLogger.Debug(fmt.Sprintf("Error creating indexd client: %s", err))
			return err
		}

		remoteConfig := cfg.GetRemote(remote)
		if remoteConfig != nil {
			myLogger.Info("Proactively updating DRS objects before push...")
			builder := drs.NewObjectBuilder(remoteConfig.GetBucketName(), remoteConfig.GetProjectId())
			// Automatically discover LFS files on current branch to ensure we have records for them
			err = drsmap.UpdateDrsObjects(builder, string(remote), "", []string{"HEAD"}, myLogger)
			if err != nil {
				myLogger.Warn(fmt.Sprintf("Warning: could not proactively update DRS objects: %v", err))
			}
		}

		err = drsmap.PushLocalDrsObjects(drsClient, myLogger, stage)
		if err != nil {
			return err
		}

		return nil
	},
}

func init() {
	Cmd.Flags().BoolVarP(&stage, "stage", "s", false, "Locally stage LFS objects from DRS server if they don't already exist in git index")
}
