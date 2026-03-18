package migrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

const numWorkers = 20

var (
	remote      string
	projectId   string
	bucketPref  string
	dryRun      bool
	confirmFlag string
)

// Cmd represents the migrate command category
var Cmd = &cobra.Command{
	Use:   "migrate",
	Short: "Database migration utilities",
}

// MigrateFilenamesCmd represents the migrate filenames command
var MigrateFilenamesCmd = &cobra.Command{
	Use:     "filenames",
	Aliases: []string{"migrate-filenames"},
	Short:   "Migrate DRS records to strip bucket prefix from filenames",
	Long: `Migrate DRS records for a specific project to strip the bucket name prefix from the file_name field.
This is useful when records were imported with full bucket paths but should only have relative paths.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMigration(cmd, migrateFilenames)
	},
}

// MigrateUrlsCmd represents the migrate urls command
var MigrateUrlsCmd = &cobra.Command{
	Use:   "urls",
	Short: "Migrate DRS records to use filename-based S3 URLs",
	Long: `Migrate DRS records for a specific project to use S3 URLs based on the filename (s3://bucket/path)
instead of GUID-based paths (s3://bucket/did/hash).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMigration(cmd, migrateUrls)
	}}

// MigrateCleanupUrlsCmd represents the migrate cleanup-urls command
var MigrateCleanupUrlsCmd = &cobra.Command{
	Use:   "cleanup-urls",
	Short: "Remove redundant/mismatched S3 URLs from DRS records",
	Long: `Remove S3 URLs from DRS records that do not match the expected filename-based path (s3://bucket/filename).
This is useful for cleaning up records that have both filename-based and GUID-based URLs.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMigration(cmd, migrateCleanupUrls)
	},
}

type migrationFunc func(obj *drs.DRSObject, drsClient client.DRSClient) (bool, string)

func migrateFilenames(obj *drs.DRSObject, drsClient client.DRSClient) (bool, string) {
	targetBucket := drsClient.GetBucketName()
	oldName := obj.Name
	newName := oldName

	// Strip s3:// prefix if it erroneously ended up in the name
	if strings.HasPrefix(newName, "s3://") {
		newName = strings.TrimPrefix(newName, "s3://")
	}

	// Strip bucket prefix if present
	if targetBucket != "" {
		prefixes := []string{targetBucket + "/", "/" + targetBucket + "/"}
		for _, p := range prefixes {
			if strings.HasPrefix(newName, p) {
				newName = strings.TrimPrefix(newName, p)
				break
			}
		}
	}

	if newName != oldName {
		msg := fmt.Sprintf("  Updating %s name: '%s' -> '%s'", obj.Id, oldName, newName)
		obj.Name = newName
		return true, msg
	}
	return false, ""
}

func migrateUrls(obj *drs.DRSObject, drsClient client.DRSClient) (bool, string) {
	targetBucket := drsClient.GetBucketName()
	if targetBucket == "" || obj.Name == "" {
		return false, ""
	}

	targetUrl := fmt.Sprintf("s3://%s/%s", strings.TrimPrefix(targetBucket, "/"), strings.TrimPrefix(obj.Name, "/"))
	changed := false
	var newAccessMethods []drs.AccessMethod
	var s3Template *drs.AccessMethod
	var oldS3Urls []string

	for i := range obj.AccessMethods {
		am := obj.AccessMethods[i]
		isS3 := am.Type == "s3" || strings.Contains(am.AccessURL.URL, "s3://")
		if isS3 {
			oldS3Urls = append(oldS3Urls, am.AccessURL.URL)
			if s3Template == nil {
				// Take a copy of the first S3 method to use as template
				copyAm := am
				s3Template = &copyAm
			}
			if am.AccessURL.URL != targetUrl {
				changed = true
			}
		} else {
			newAccessMethods = append(newAccessMethods, am)
		}
	}

	if s3Template != nil {
		if s3Template.AccessURL.URL != targetUrl {
			s3Template.AccessURL.URL = targetUrl
			changed = true
		}
		s3Template.Type = "s3"
		newAccessMethods = append(newAccessMethods, *s3Template)

		if len(obj.AccessMethods) != len(newAccessMethods) {
			changed = true
		}
	}

	if changed {
		msg := fmt.Sprintf("  Updating URLs for %s:\n    Old: %v\n    New: %s",
			obj.Id, oldS3Urls, targetUrl)
		obj.AccessMethods = newAccessMethods
		return true, msg
	}

	return false, ""
}

func migrateCleanupUrls(obj *drs.DRSObject, drsClient client.DRSClient) (bool, string) {
	if obj.Name == "" {
		return false, ""
	}

	targetBucket := drsClient.GetBucketName()
	// Derive the expected path by stripping prefixes from the Name (handling the "90% match")
	cleanPath := obj.Name
	cleanPath = strings.TrimPrefix(cleanPath, "s3://")
	if targetBucket != "" {
		// handle both bucket/ and /bucket/ prefixes
		prefixes := []string{targetBucket + "/", "/" + targetBucket + "/"}
		for _, p := range prefixes {
			if strings.HasPrefix(cleanPath, p) {
				cleanPath = strings.TrimPrefix(cleanPath, p)
				break
			}
		}
	}

	targetUrl := fmt.Sprintf("s3://%s/%s", strings.TrimPrefix(targetBucket, "/"), strings.TrimPrefix(cleanPath, "/"))

	var s3Methods []int
	for i := range obj.AccessMethods {
		am := obj.AccessMethods[i]
		if am.Type == "s3" || strings.Contains(am.AccessURL.URL, "s3://") {
			s3Methods = append(s3Methods, i)
		}
	}

	// If there is only 1 S3 URL (or none), do not perform cleanup on it.
	if len(s3Methods) <= 1 {
		return false, ""
	}

	// Multiple S3 URLs found - check if we have the target URL.
	hasTarget := false
	for _, idx := range s3Methods {
		if obj.AccessMethods[idx].AccessURL.URL == targetUrl {
			hasTarget = true
			break
		}
	}

	// If the target URL isn't present, don't prune anything to avoid data loss.
	if !hasTarget {
		return false, ""
	}

	var newAccessMethods []drs.AccessMethod
	var removed []string
	changed := false

	for i := range obj.AccessMethods {
		am := obj.AccessMethods[i]
		isS3 := am.Type == "s3" || strings.Contains(am.AccessURL.URL, "s3://")
		if isS3 {
			if am.AccessURL.URL == targetUrl {
				newAccessMethods = append(newAccessMethods, am)
			} else {
				removed = append(removed, am.AccessURL.URL)
				changed = true
			}
		} else {
			newAccessMethods = append(newAccessMethods, am)
		}
	}

	if changed {
		msg := fmt.Sprintf("  Cleaning up %d mismatched URLs for %s. Removed: %v",
			len(removed), obj.Id, removed)
		obj.AccessMethods = newAccessMethods
		return true, msg
	}

	return false, ""
}

func runMigration(cmd *cobra.Command, migrateItem migrationFunc) error {
	logger := drslog.GetLogger()
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("error loading config: %v", err)
	}

	remoteName, err := cfg.GetRemoteOrDefault(remote)
	if err != nil {
		return fmt.Errorf("error getting remote: %v", err)
	}

	drsClient, err := cfg.GetRemoteClient(remoteName, logger)
	if err != nil {
		return fmt.Errorf("error creating DRS client: %v", err)
	}

	targetProject := projectId
	if targetProject == "" {
		targetProject = drsClient.GetProjectId()
	}
	if targetProject == "" {
		return fmt.Errorf("no project ID specified and none found for remote %s", remoteName)
	}

	targetBucket := bucketPref
	if targetBucket == "" {
		targetBucket = drsClient.GetBucketName()
	}

	// Confirmation logic
	if !dryRun && confirmFlag != targetProject {
		common.DisplayWarningHeader(os.Stderr, "MIGRATE for a project")
		common.DisplayField(os.Stderr, "Remote", string(remoteName))
		common.DisplayField(os.Stderr, "Project ID", targetProject)
		common.DisplayField(os.Stderr, "Bucket", targetBucket)
		fmt.Fprintf(os.Stderr, "\nThis will update records in project '%s'.\n", targetProject)
		common.DisplayFooter(os.Stderr)

		if err := common.PromptForConfirmation(os.Stderr, fmt.Sprintf("Type the project ID '%s' to confirm", targetProject), targetProject, true); err != nil {
			return err
		}
	}

	totalUpdated := 0
	totalSkipped := 0
	totalChecked := 0

	fmt.Fprintf(os.Stderr, "\nProcessing records...\n")
	objChan, err := drsClient.ListObjectsByProject(context.Background(), targetProject)
	if err != nil {
		return fmt.Errorf("error listing objects: %v", err)
	}

	// Progress Bar setup
	p := mpb.New(mpb.WithOutput(os.Stderr), mpb.WithWidth(60))
	bar := p.AddBar(0,
		mpb.PrependDecorators(
			decor.Name("Progress: "),
			decor.CountersNoUnit("%d / %d"),
		),
		mpb.AppendDecorators(
			decor.Percentage(decor.WCSyncSpace),
			decor.Name(" | "),
			decor.AverageSpeed(0, "%.1f rec/s", decor.WCSyncSpace),
		),
	)

	// Worker Pool Setup
	type workItem struct {
		obj *drs.DRSObject
	}
	type resultItem struct {
		updated bool
		logMsg  string
		err     error
	}

	workChan := make(chan workItem, numWorkers*2)
	resChan := make(chan resultItem, numWorkers*2)
	var workerWg sync.WaitGroup
	var statsWg sync.WaitGroup

	// Start workers
	for i := 0; i < numWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for item := range workChan {
				updatedInRecord, logMsg := migrateItem(item.obj, drsClient)
				if updatedInRecord {
					if logMsg != "" {
						logger.Debug(logMsg)
					}
					if !dryRun {
						// Remodel: Since Indexd's UpdateRecord (PUT) can be additive or merge-only depending on server config,
						// we use a Delete + Register cycle to ensure the record exactly matches our pruned/remodeled state.
						
						// 1. Attempt to delete existing record by its ID (GUID)
						// We ignore the error here because the record might not exist or might fail, 
						// and RegisterRecord will handle the "id already exists" case if delete failed.
						_ = drsClient.GetGen3Interface().Indexd().DeleteIndexdRecord(context.Background(), item.obj.Id)

						// 2. Re-register the canonical object
						_, err := drsClient.RegisterRecord(context.Background(), item.obj)
						if err != nil {
							logger.Error("failed to remodel record", "id", item.obj.Id, "error", err)
							resChan <- resultItem{err: err, logMsg: logMsg}
						} else {
							resChan <- resultItem{updated: true, logMsg: logMsg}
						}
					} else {
						resChan <- resultItem{updated: true, logMsg: logMsg}
					}
				} else {
					resChan <- resultItem{updated: false}
				}
			}
		}()
	}

	// Stats Collector
	var logsShown uint32
	statsWg.Add(1)
	go func() {
		defer statsWg.Done()
		for res := range resChan {
			totalChecked++
			if res.updated {
				totalUpdated++
				currentShown := atomic.LoadUint32(&logsShown)
				if res.logMsg != "" && currentShown < 5 {
					fmt.Fprintf(os.Stderr, "\n[DETAILED LOG] %s\n", res.logMsg)
					atomic.AddUint32(&logsShown, 1)
				}
			} else {
				totalSkipped++
			}
			bar.Increment()
		}
	}()

	// Main Fetch Loop
	itemCount := 0
	for res := range objChan {
		if res.Error != nil {
			logger.Error("error listing object", "error", res.Error)
			continue
		}
		if res.Object == nil {
			continue
		}
		itemCount++
		bar.SetTotal(int64(itemCount), false)
		workChan <- workItem{obj: res.Object}
	}

	close(workChan)
	workerWg.Wait()
	close(resChan)
	statsWg.Wait()
	bar.SetTotal(int64(itemCount), true) // mark as complete
	p.Wait()

	if dryRun {
		fmt.Fprintf(os.Stderr, "\n[DRY RUN COMPLETE] Potential updates: %d, Skipped: %d, Total Checked: %d\n", totalUpdated, totalSkipped, totalChecked)
	} else {
		fmt.Fprintf(os.Stderr, "\n[MIGRATION COMPLETE] Total updated: %d, Skipped: %d, Total Checked: %d\n", totalUpdated, totalSkipped, totalChecked)
	}

	return nil
}

func init() {
	for _, c := range []*cobra.Command{MigrateFilenamesCmd, MigrateUrlsCmd, MigrateCleanupUrlsCmd} {
		c.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server")
		c.Flags().StringVarP(&projectId, "project", "p", "", "project ID to migrate")
		c.Flags().StringVarP(&bucketPref, "bucket", "b", "", "bucket name (e.g. bforepc-prod)")
		c.Flags().BoolVar(&dryRun, "dry-run", false, "show changes without applying them")
		c.Flags().StringVar(&confirmFlag, "confirm", "", "skip interactive confirmation")
		Cmd.AddCommand(c)
	}
}
