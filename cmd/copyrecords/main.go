package copyrecords

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
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
	newLocalTargetAPI = func() indexAPI {
		return localIndexAPI{}
	}
)

var Cmd = &cobra.Command{
	Use:   "copy-records [source-remote] <target-remote> <organization/project>",
	Short: "Copy Syfon records between remotes for one organization/project scope",
	Long:  "Read source records from either a source remote or the current repo's local DRS metadata and bulk load them into a target Syfon instance or the current repo's local DRS metadata, only merging controlled_access and access_methods for records that already exist on the target. Use `git drs copy-records local <target-remote> <organization/project>` to copy local repo records, or `git drs copy-records <source-remote> local <organization/project>` to copy remote records into local repo metadata.",
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
		targetIsLocal := isLocalSentinel(targetRemote)
		dstRemoteName := config.Remote(strings.TrimSpace(targetRemote))

		org, proj, err := parseScopeArg(scopeArg)
		if err != nil {
			return err
		}

		var dstAPI indexAPI
		if targetIsLocal {
			dstAPI = newLocalTargetAPI()
		} else {
			dstCtx, err := newCopyRuntime(cfg, dstRemoteName, logger)
			if err != nil {
				return fmt.Errorf("error creating target client: %w", err)
			}
			dstAPI = newCopyIndexAPI(dstCtx.Client.Requestor())
		}

		var (
			sourceRecords []copyRecord
			sourceLabel   string
		)
		if isLocalSentinel(sourceRemote) {
			if targetIsLocal {
				return fmt.Errorf("source and target cannot both be local")
			}
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
			if !targetIsLocal && srcRemoteName == dstRemoteName {
				return fmt.Errorf("source and target remotes must be different")
			}
			srcCfg := cfg.GetRemote(srcRemoteName)
			if srcCfg == nil {
				return fmt.Errorf("source remote %q not found. Available remotes: %s. Run `git drs remote list` in this repository to inspect configured git-drs remotes", srcRemoteName, configuredRemoteList(cfg))
			}
			srcCtx, err := newCopyRuntime(cfg, srcRemoteName, logger)
			if err != nil {
				return fmt.Errorf("error creating source client: %w", err)
			}
			stats, err := copyProjectRecordsFromSourceIndex(
				cmd.Context(),
				logger,
				newCopyIndexAPI(srcCtx.Client.Requestor()),
				dstAPI,
				org,
				proj,
				batchSize,
				overwriteName,
			)
			if err != nil {
				return err
			}
			sourceLabel = string(srcRemoteName)
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
		}

		stats, err := copyProjectRecords(
			cmd.Context(),
			logger,
			sourceRecords,
			dstAPI,
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
	Cmd.Flags().IntVar(&batchSize, "batch-size", defaultCopyBatchSize, "records per source page and target bulk write")
	Cmd.Flags().BoolVar(&overwriteName, "overwrite-name", false, "for existing target records, replace target name with the source value")
}

func isLocalSentinel(remote string) bool {
	return strings.EqualFold(strings.TrimSpace(remote), "local")
}

func configuredRemoteList(cfg *config.Config) string {
	if cfg == nil || len(cfg.Remotes) == 0 {
		return "<none>"
	}
	names := make([]string, 0, len(cfg.Remotes))
	for name := range cfg.Remotes {
		names = append(names, string(name))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
