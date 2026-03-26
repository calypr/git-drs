package drsmap

// Utilities to map between Git LFS files and DRS objects

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/git-drs/precommit_cache"
	"github.com/google/uuid"
)

// execCommand is a variable to allow mocking in tests
var execCommandContext = exec.CommandContext

func PushLocalDrsObjects(drsClient client.DRSClient, myLogger *slog.Logger) error {
	// Gather all objects in .git/drs/lfs/objects store
	drsLfsObjs, err := lfs.GetDrsLfsObjects(myLogger)
	if err != nil {
		return err
	}

	return SyncObjectsWithServer(drsClient, drsLfsObjs, myLogger)
}

func SyncObjectsWithServer(drsClient client.DRSClient, drsObjects map[string]*drs.DRSObject, myLogger *slog.Logger) error {
	if len(drsObjects) == 0 {
		return nil
	}

	// 1. Bulk lookup all hashes on server.
	hashes := make([]string, 0, len(drsObjects))
	for h := range drsObjects {
		hashes = append(hashes, h)
	}
	bulkByHash, bulkErr := drsClient.BatchGetObjectsByHash(context.Background(), hashes)
	if bulkErr != nil {
		return fmt.Errorf("bulk hash lookup failed: %w", bulkErr)
	}

	// 2. Identify missing records by hash.
	missingRecords := make([]*drs.DRSObject, 0)
	for h, localObj := range drsObjects {
		foundOnServer := false
		recs := bulkByHash[h]
		if len(recs) > 0 {
			// Check if any record matches our project.
			matched, _ := FindMatchingRecord(recs, drsClient.GetOrganization(), drsClient.GetProjectId())
			foundOnServer = matched != nil
		}

		if !foundOnServer {
			myLogger.Debug(fmt.Sprintf("Record missing on server for hash %s, adding to registration queue", h))
			missingRecords = append(missingRecords, localObj)
		}
	}

	// 3. Register missing records in one bulk request when possible.
	if len(missingRecords) > 0 {
		myLogger.Info(fmt.Sprintf("Registering %d missing records", len(missingRecords)))
		if _, err := drsClient.BatchRegisterRecords(context.Background(), missingRecords); err != nil {
			myLogger.Error(fmt.Sprintf("Failed to register records in bulk: %v", err))
			return fmt.Errorf("error in bulk registration: %v", err)
		}
	}

	myLogger.Info(fmt.Sprintf("Successfully synced %d objects with server (registered %d new)", len(drsObjects), len(missingRecords)))
	return nil
}

func SyncFilesWithServer(drsClient client.DRSClient, lfsFiles map[string]lfs.LfsFileInfo, logger *slog.Logger) error {
	objectsToSync := make(map[string]*drs.DRSObject)
	for _, file := range lfsFiles {
		obj, err := lfs.ReadObject(common.DRS_OBJS_PATH, file.Oid)
		if err == nil && obj != nil {
			objectsToSync[file.Oid] = obj
		}
	}
	return SyncObjectsWithServer(drsClient, objectsToSync, logger)
}

func PullRemoteDrsObjects(drsClient client.DRSClient, logger *slog.Logger) error {
	objChan, err := drsClient.ListObjectsByProject(context.Background(), drsClient.GetProjectId())
	if err != nil {
		return err
	}
	writtenObjs := 0
	for drsObj := range objChan {
		if drsObj.Object == nil {
			logger.Debug(fmt.Sprintf("OBJ is nil: %#v, continuing...", drsObj))
			continue
		}
		hashInfo := hash.ConvertDrsChecksumsToHashInfo(drsObj.Object.Checksums)
		if hashInfo.SHA256 == "" {
			return fmt.Errorf("error: drs Object '%s' does not contain a sha256 checksum", drsObj.Object.Id)
		}
		oid := hashInfo.SHA256
		drsObjPath, err := GetObjectPath(common.DRS_OBJS_PATH, oid)
		if err != nil {
			return fmt.Errorf("error getting object path for oid %s: %v", oid, err)
		}
		// Only write a record if there exists a proper checksum to use. Checksums besides sha256 are not used
		if drsObjPath != "" && oid != "" {
			writtenObjs++
			// write drs objects to DRS_OBJS_PATH
			err = WriteDrsObj(drsObj.Object, oid, drsObjPath)
			if err != nil {
				return fmt.Errorf("error writing DRS object for oid %s: %v", oid, err)
			}
		}
	}
	logger.Debug(fmt.Sprintf("Wrote %d new objs to object store", writtenObjs))
	return nil
}
func UpdateDrsObjects(builder drs.ObjectBuilder, gitRemoteName, gitRemoteLocation string, branches []string, logger *slog.Logger) error {

	logger.Debug("Update to DRS objects started")

	// get all lfs files
	lfsFiles, err := lfs.GetAllLfsFiles(gitRemoteName, gitRemoteLocation, branches, logger)
	if err != nil {
		return fmt.Errorf("error getting all LFS files: %v", err)
	}

	// get project
	if builder.ProjectID == "" {
		return fmt.Errorf("no project configured")
	}

	return UpdateDrsObjectsWithFiles(builder, lfsFiles, UpdateOptions{Logger: logger})
}

type UpdateOptions struct {
	Cache          *precommit_cache.Cache
	PreferCacheURL bool
	Logger         *slog.Logger
}

func UpdateDrsObjectsWithFiles(builder drs.ObjectBuilder, lfsFiles map[string]lfs.LfsFileInfo, opts UpdateOptions) error {
	if opts.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	opts.Logger.Debug("Update to DRS objects started")

	// get project
	if builder.ProjectID == "" {
		return fmt.Errorf("no project configured")
	}
	if len(lfsFiles) == 0 {
		return nil
	}

	for _, file := range lfsFiles {
		var authoritativeObj *drs.DRSObject
		existing, err := lfs.ReadObject(common.DRS_OBJS_PATH, file.Oid)
		if err == nil && existing != nil {
			authoritativeObj = existing
			// Update basic info if necessary
			name := file.Name
			authoritativeObj.Name = &name
			authoritativeObj.Size = file.Size

			// Ensure Authorizations are populated (backwards compatibility for old local records)
			authzStr, _ := common.ProjectToResource(builder.Organization, builder.ProjectID)
			for i := range authoritativeObj.AccessMethods {
				if authoritativeObj.AccessMethods[i].Authorizations == nil {
					authoritativeObj.AccessMethods[i].Authorizations = &drs.Authorizations{}
				}
				if len(authoritativeObj.AccessMethods[i].Authorizations.BearerAuthIssuers) == 0 {
					authoritativeObj.AccessMethods[i].Authorizations.BearerAuthIssuers = []string{authzStr}
				}
				// Ensure URL matches current policy of namespaced CAS-style
				// s3://bucket/{org}/{project}/HASH.
				if builder.Bucket != "" {
					prefix := strings.Trim(strings.TrimSpace(builder.StoragePrefix), "/")
					if prefix == "" {
						prefix = drs.StoragePrefix(builder.Organization, builder.ProjectID)
					}
					if prefix != "" {
						authoritativeObj.AccessMethods[i].AccessUrl = &drs.AccessURL{Url: fmt.Sprintf("s3://%s/%s/%s", builder.Bucket, prefix, file.Oid)}
					} else {
						authoritativeObj.AccessMethods[i].AccessUrl = &drs.AccessURL{Url: fmt.Sprintf("s3://%s/%s", builder.Bucket, file.Oid)}
					}
				}
			}
		} else {
			drsID := DrsUUID(builder.ProjectID, file.Oid)
			authoritativeObj, err = builder.Build(file.Name, file.Oid, file.Size, drsID)
			if err != nil {
				opts.Logger.Error(fmt.Sprintf("Could not build DRS object for %s OID %s %v", file.Name, file.Oid, err))
				continue
			}
		}

		authoritativeURL := ""
		if len(authoritativeObj.AccessMethods) > 0 && authoritativeObj.AccessMethods[0].AccessUrl != nil {
			authoritativeURL = authoritativeObj.AccessMethods[0].AccessUrl.Url
		}

		var hint string
		if opts.Cache != nil {
			externalURL, ok, err := opts.Cache.LookupExternalURLByOID(file.Oid)
			if err != nil {
				opts.Logger.Debug(fmt.Sprintf("cache lookup failed for %s: %v", file.Oid, err))
			} else if ok {
				hint = externalURL
			}
		}

		if hint != "" {
			if err := precommit_cache.CheckExternalURLMismatch(hint, authoritativeURL); err != nil {
				opts.Logger.Warn(fmt.Sprintf("Warning. %s (path=%s oid=%s)", err.Error(), file.Name, file.Oid))
				fmt.Fprintln(os.Stderr, "Warning.", err.Error())
			}
		}

		if opts.PreferCacheURL && hint != "" {
			authzStr, _ := common.ProjectToResource(builder.Organization, builder.ProjectID)
			authz := &drs.Authorizations{
				BearerAuthIssuers: []string{authzStr},
			}
			if len(authoritativeObj.AccessMethods) > 0 {
				authoritativeObj.AccessMethods[0].AccessUrl = &drs.AccessURL{Url: hint}
				authoritativeObj.AccessMethods[0].Authorizations = authz
			} else {
				authoritativeObj.AccessMethods = append(authoritativeObj.AccessMethods, drs.AccessMethod{
					Type:           "s3",
					AccessUrl:      &drs.AccessURL{Url: hint},
					Authorizations: authz,
				})
			}
		}

		if err := lfs.WriteObject(common.DRS_OBJS_PATH, authoritativeObj, file.Oid); err != nil {
			opts.Logger.Error(fmt.Sprintf("Could not WriteDrsFile for %s OID %s %v", file.Name, file.Oid, err))
			continue
		}
		opts.Logger.Info(fmt.Sprintf("Prepared File %s OID %s with DRS ID %s for commit", file.Name, file.Oid, authoritativeObj.Id))
	}

	return nil
}

// WriteDrsFile creates drsObject record from LFS file info
func WriteDrsFile(builder drs.ObjectBuilder, file lfs.LfsFileInfo, objectPath *string) (*drs.DRSObject, error) {

	// determine drs object path: use provided objectPath if non-nil/non-empty, otherwise compute default

	// if file is in cache, hasn't been committed to git or pushed to DRS server
	// create a local DRS object for it
	// TODO: determine git to gen3 project hierarchy mapping (eg repo name to project ID)
	// If objectPath is provided, we use it. Otherwise compute default.
	existing, err := lfs.ReadObject(common.DRS_OBJS_PATH, file.Oid)
	var drsObj *drs.DRSObject
	if err == nil && existing != nil {
		drsObj = existing
		name := file.Name
		drsObj.Name = &name
		drsObj.Size = file.Size
	} else {
		drsId := DrsUUID(builder.ProjectID, file.Oid)
		drsObj, err = builder.Build(file.Name, file.Oid, file.Size, drsId)
		if err != nil {
			return nil, fmt.Errorf("error building DRS object for oid %s: %v", file.Oid, err)
		}
	}

	if objectPath != nil && *objectPath != "" {
		if len(drsObj.AccessMethods) > 0 {
			drsObj.AccessMethods[0].AccessUrl = &drs.AccessURL{Url: *objectPath}
		} else {
			drsObj.AccessMethods = append(drsObj.AccessMethods, drs.AccessMethod{
				Type:      "s3",
				AccessUrl: &drs.AccessURL{Url: *objectPath},
			})
		}
	}

	// write drs objects to DRS_OBJS_PATH
	err = lfs.WriteObject(common.DRS_OBJS_PATH, drsObj, file.Oid)
	if err != nil {
		return nil, fmt.Errorf("error writing DRS object for oid %s: %v", file.Oid, err)
	}
	return drsObj, nil
}

func WriteDrsObj(drsObj *drs.DRSObject, oid string, drsObjPath string) error {
	basePath := filepath.Dir(filepath.Dir(filepath.Dir(drsObjPath)))
	return lfs.WriteObject(basePath, drsObj, oid)
}

func DrsUUID(projectId string, hash string) string {
	// normalize hash - strip sha256: prefix if present
	hash = strings.TrimPrefix(hash, "sha256:")

	// create UUID based on project ID and hash
	hashStr := fmt.Sprintf("%s:%s", projectId, hash)
	return uuid.NewSHA1(drs.NAMESPACE, []byte(hashStr)).String()
}

// creates drsObject record from file
func DrsInfoFromOid(oid string) (*drs.DRSObject, error) {
	return lfs.ReadObject(common.DRS_OBJS_PATH, oid)
}

func GetObjectPath(basePath string, oid string) (string, error) {
	return lfs.ObjectPath(basePath, oid)
}

// CreateCustomPath creates a custom path based on the DRS URI
// For example, DRS URI drs://<namespace>:<drs_id>
// create custom path <baseDir>/<namespace>/<drs_id>
func CreateCustomPath(baseDir, drsURI string) (string, error) {
	const prefix = "drs://"
	if len(drsURI) <= len(prefix) || drsURI[:len(prefix)] != prefix {
		return "", fmt.Errorf("invalid DRS URI: %s", drsURI)
	}
	rest := drsURI[len(prefix):]

	// Split by first colon
	colonIdx := -1
	for i, c := range rest {
		if c == ':' {
			colonIdx = i
			break
		}
	}
	if colonIdx == -1 {
		return "", fmt.Errorf("DRS URI missing colon: %s", drsURI)
	}
	namespace := rest[:colonIdx]
	drsId := rest[colonIdx+1:]
	return filepath.Join(baseDir, namespace, drsId), nil
}

// FindMatchingRecord finds a record from the list that matches the given project ID authz
// If no matching record is found return nil
func FindMatchingRecord(records []drs.DRSObject, organization, projectId string) (*drs.DRSObject, error) {
	if len(records) == 0 {
		return nil, nil
	}

	// Convert project ID to resource path format for comparison
	expectedAuthz, err := common.ProjectToResource(organization, projectId)
	if err != nil {
		return nil, fmt.Errorf("error converting project ID to resource format: %v", err)
	}

	for _, record := range records {
		for _, access := range record.AccessMethods {
			if access.Authorizations == nil {
				continue
			}

			// Check BearerAuthIssuers using a map for O(1) lookup (ref: "lists suck")
			issuersMap := make(map[string]struct{}, len(access.Authorizations.BearerAuthIssuers))
			for _, issuer := range access.Authorizations.BearerAuthIssuers {
				issuersMap[issuer] = struct{}{}
			}

			if _, ok := issuersMap[expectedAuthz]; ok {
				return &record, nil
			}
		}
	}
	return nil, nil
}

// output of git lfs ls-files
type LfsLsOutput struct {
	Files []lfs.LfsFileInfo `json:"files"`
}

type LfsDryRunSpec struct {
	Remote string // e.g. "origin"
	Ref    string // e.g. "refs/heads/main" or "HEAD"
}

// RunLfsPushDryRun executes: git lfs push --dry-run <remote> <ref>
func RunLfsPushDryRun(ctx context.Context, repoDir string, spec LfsDryRunSpec, logger *slog.Logger) (string, error) {
	if spec.Remote == "" || spec.Ref == "" {
		return "", errors.New("missing remote or ref")
	}

	// Debug-print the command to stderr
	fullCmd := []string{"git", "lfs", "push", "--dry-run", spec.Remote, spec.Ref}
	logger.Debug(fmt.Sprintf("running command: %v", fullCmd))

	cmd := execCommandContext(ctx, "git", "lfs", "push", "--dry-run", spec.Remote, spec.Ref)
	cmd.Dir = repoDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := stdout.String()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("git lfs push --dry-run failed: %s", msg)
	}
	return out, nil
}
