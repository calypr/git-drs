package copyrecords

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/calypr/syfon/client/request"
	"github.com/spf13/cobra"
)

var (
	batchSize     int
	overwriteName bool
)

var (
	loadCopyConfig = config.LoadConfig
	newCopyRuntime = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*remoteruntime.GitContext, error) {
		return remoteruntime.New(cfg, remote, logger)
	}
	newCopyIndexAPI = func(requestor request.Requester) indexAPI {
		return newRawIndexAPI(requestor)
	}
	loadLocalSource = func(ctx context.Context, org, project string) ([]copyRecord, error) {
		return loadLocalSourceRecords(org, project)
	}
)

var Cmd = &cobra.Command{
	Use:   "copy-records [source-remote] <target-remote> <organization/project>",
	Short: "Copy Syfon records between remotes for one organization/project scope",
	Long:  "Read source records from either a source remote or the current repo's local DRS metadata and bulk load them into a target Syfon instance, only merging controlled_access and access_methods for records that already exist on the target. Use `git drs copy-records local <target-remote> <organization/project>` to copy local repo records.",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()
		cfg, err := loadCopyConfig()
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

		if strings.TrimSpace(targetRemote) == "" {
			return fmt.Errorf("target remote is required")
		}
		dstRemoteName := config.Remote(strings.TrimSpace(targetRemote))

		org, proj, err := parseScopeArg(scopeArg)
		if err != nil {
			return err
		}

		dstCtx, err := newCopyRuntime(cfg, dstRemoteName, logger)
		if err != nil {
			return fmt.Errorf("error creating target client: %w", err)
		}

		var (
			sourceRecords []copyRecord
			sourceLabel   string
		)
		if strings.EqualFold(strings.TrimSpace(sourceRemote), "local") {
			sourceRecords, err = loadLocalSource(cmd.Context(), org, proj)
			if err != nil {
				return err
			}
			sourceLabel = "local"
		} else {
			srcRemoteName, err := cfg.GetRemoteOrDefault(sourceRemote)
			if err != nil {
				return fmt.Errorf("error resolving source remote: %w", err)
			}
			if srcRemoteName == dstRemoteName {
				return fmt.Errorf("source and target remotes must be different")
			}
			srcCfg := cfg.GetRemote(srcRemoteName)
			if srcCfg == nil {
				return fmt.Errorf("source remote %q not found", srcRemoteName)
			}
			srcCtx, err := newCopyRuntime(cfg, srcRemoteName, logger)
			if err != nil {
				return fmt.Errorf("error creating source client: %w", err)
			}
			sourceRecords, err = listSourceRecordsByControlledAccess(cmd.Context(), newCopyIndexAPI(srcCtx.Client.Requestor()), org, proj, batchSize)
			if err != nil {
				return err
			}
			sourceLabel = string(srcRemoteName)
		}

		stats, err := copyProjectRecords(
			cmd.Context(),
			logger,
			sourceRecords,
			newCopyIndexAPI(dstCtx.Client.Requestor()),
			org,
			proj,
			batchSize,
			overwriteName,
		)
		if err != nil {
			return err
		}

		logger.Info("copy-records complete",
			"source_remote", sourceLabel,
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
	Cmd.Flags().BoolVar(&overwriteName, "overwrite-name", false, "for existing target records, replace target name with the source value")
}
