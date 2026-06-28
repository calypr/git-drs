package copyrecords

import (
	"fmt"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/spf13/cobra"
)

var (
	batchSize             int
	overwriteNameFileName bool
)

var Cmd = &cobra.Command{
	Use:   "copy-records [source-remote] <target-remote> <organization/project>",
	Short: "Copy Syfon records between remotes for one organization/project scope",
	Long:  "Read all Syfon records for a source organization/project scope and bulk load them into a target Syfon instance, only merging controlled_access and access_methods for records that already exist on the target.",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %w", err)
		}

		sourceRemote := ""
		targetRemote := ""
		scopeArg := ""
		if len(args) == 2 {
			targetRemote = args[0]
			scopeArg = args[1]
		} else {
			sourceRemote = args[0]
			targetRemote = args[1]
			scopeArg = args[2]
		}

		srcRemoteName, err := cfg.GetRemoteOrDefault(sourceRemote)
		if err != nil {
			return fmt.Errorf("error resolving source remote: %w", err)
		}
		if strings.TrimSpace(targetRemote) == "" {
			return fmt.Errorf("target remote is required")
		}
		dstRemoteName := config.Remote(targetRemote)
		if srcRemoteName == dstRemoteName {
			return fmt.Errorf("source and target remotes must be different")
		}

		srcCfg := cfg.GetRemote(srcRemoteName)
		if srcCfg == nil {
			return fmt.Errorf("source remote %q not found", srcRemoteName)
		}

		org, proj, err := parseScopeArg(scopeArg)
		if err != nil {
			return err
		}

		srcCtx, err := remoteruntime.New(cfg, srcRemoteName, logger)
		if err != nil {
			return fmt.Errorf("error creating source client: %w", err)
		}
		dstCtx, err := remoteruntime.New(cfg, dstRemoteName, logger)
		if err != nil {
			return fmt.Errorf("error creating target client: %w", err)
		}

		stats, err := copyProjectRecords(
			cmd.Context(),
			logger,
			newRawIndexAPI(srcCtx.Client.Requestor()),
			newRawIndexAPI(dstCtx.Client.Requestor()),
			org,
			proj,
			batchSize,
			overwriteNameFileName,
		)
		if err != nil {
			return err
		}

		logger.Info("copy-records complete",
			"source_remote", srcRemoteName,
			"target_remote", dstRemoteName,
			"organization", org,
			"project", proj,
			"source_seen", stats.SourceSeen,
			"created", stats.Created,
			"updated", stats.Updated,
			"unchanged", stats.Unchanged,
			"written", stats.Written,
		)
		return nil
	},
}

func init() {
	Cmd.Flags().IntVar(&batchSize, "batch-size", 250, "records per source page and target bulk write")
	Cmd.Flags().BoolVar(&overwriteNameFileName, "overwrite-name-file-name", false, "for existing target records, replace target name and file_name with the source values")
}
