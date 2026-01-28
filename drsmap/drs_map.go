package drsmap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/indexd/drs"
	"github.com/calypr/data-client/indexd/hash"
	"github.com/calypr/data-client/upload"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/lfs"
	"github.com/go-git/go-git/v5"
	"github.com/google/uuid"
)

// execCommand is a variable to allow mocking in tests
var execCommand = exec.Command
var execCommandContext = exec.CommandContext

var NAMESPACE = uuid.NewMD5(uuid.NameSpaceURL, []byte("calypr.org"))

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

// output of git lfs ls-files
type LfsLsOutput struct {
	Files []LfsFileInfo `json:"files"`
}

// LfsFileInfo represents the information about an LFS file
type LfsFileInfo struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Checkout   bool   `json:"checkout"`
	Downloaded bool   `json:"downloaded"`
	OidType    string `json:"oid_type"`
	Oid        string `json:"oid"`
	Version    string `json:"version"`
}

func PushLocalDrsObjects(drsClient client.DRSClient, gen3Client g3client.Gen3Interface, bucketName string, upsert bool, myLogger *slog.Logger) error {
	// Gather all objects in .git/drs/lfs/objects store
	drsLfsObjs, err := lfs.GetDrsLfsObjects(myLogger)
	if err != nil {
		return err
	}

	// Make this a map if it does not exist when hitting the server
	sums := make([]*hash.Checksum, 0)
	for _, obj := range drsLfsObjs {
		for sumType, sum := range hash.ConvertHashInfoToMap(obj.Checksums) {
			if sumType == hash.ChecksumTypeSHA256.String() {
				sums = append(sums, &hash.Checksum{
					Checksum: sum,
					Type:     hash.ChecksumTypeSHA256,
				})
			}
		}
	}

	outobjs := map[string]*drs.DRSObject{}
	for _, sum := range sums {
		records, err := drsClient.GetObjectByHash(context.Background(), sum)
		if err != nil {
			return err
		}

		if len(records) == 0 {
			outobjs[sum.Checksum] = nil
			continue
		}
		found := false
		// Warning: The loop overwrites map entries if multiple records have the same SHA256 hash.
		// If there are multiple records with SHA256 checksums, only the last one will be stored in the map
		for i, rec := range records {
			if rec.Checksums.SHA256 != "" {
				found = true
				outobjs[rec.Checksums.SHA256] = &records[i]
			}
		}
		if !found {
			outobjs[sum.Checksum] = nil
		}
	}

	for drsObjKey := range outobjs {
		val, ok := drsLfsObjs[drsObjKey]
		if !ok {
			myLogger.Debug(fmt.Sprintf("Drs record not found in sha256 map %s", drsObjKey))
		}
		if _, statErr := os.Stat(val.Name); os.IsNotExist(statErr) {
			myLogger.Debug(fmt.Sprintf("Error: Object record found locally, but file does not exist locally. Registering Record %s", val.Name))
			_, err = drsClient.RegisterRecord(context.Background(), val)
			if err != nil {
				return err
			}

		} else {
			filePath, err := GetObjectPath(common.LFS_OBJS_PATH, drsObjKey)
			if err != nil {
				return err
			}

			_, err = upload.RegisterAndUploadFile(
				context.Background(),
				gen3Client,
				val,
				filePath,
				bucketName,
				upsert,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
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
		sumMap := hash.ConvertHashInfoToMap(drsObj.Object.Checksums)
		if len(sumMap) == 0 {
			return fmt.Errorf("error: drs Object '%s' does not contain a checksum", drsObj.Object.Id)
		}
		var drsObjPath, oid string = "", ""
		for sumType, sum := range sumMap {
			if sumType == hash.ChecksumTypeSHA256.String() {
				oid = sum
				drsObjPath, err = GetObjectPath(common.DRS_OBJS_PATH, oid)
				if err != nil {
					return fmt.Errorf("error getting object path for oid %s: %v", oid, err)
				}
			}
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
	lfsFiles, err := GetAllLfsFiles(gitRemoteName, gitRemoteLocation, branches, logger)
	if err != nil {
		return fmt.Errorf("error getting all LFS files: %v", err)
	}

	// get project
	if builder.ProjectID == "" {
		return fmt.Errorf("no project configured")
	}

	return UpdateDrsObjectsWithFiles(builder, lfsFiles, nil, false, logger)
}

func UpdateDrsObjectsWithFiles(builder drs.ObjectBuilder, lfsFiles map[string]drslfs.LfsFileInfo, cache *precommit_cache.Cache, preferCacheURL bool, logger *slog.Logger) error {
	if logger == nil {
		return fmt.Errorf("logger is required")
	}
	logger.Debug("Update to DRS objects started")

	// get project
	if builder.ProjectID == "" {
		return fmt.Errorf("no project configured")
	}
	if len(lfsFiles) == 0 {
		return nil
	}

	for _, file := range lfsFiles {
		drsID := DrsUUID(builder.ProjectID, file.Oid)
		authoritativeObj, err := builder.Build(file.Name, file.Oid, file.Size, drsID)
		if err != nil {
			logger.Error(fmt.Sprintf("Could not build DRS object for %s OID %s %v", file.Name, file.Oid, err))
			continue
		}

		authoritativeURL := ""
		if len(authoritativeObj.AccessMethods) > 0 {
			authoritativeURL = authoritativeObj.AccessMethods[0].AccessURL.URL
		}

		var hint string
		if cache != nil {
			externalURL, ok, err := cache.LookupExternalURLByOID(file.Oid)
			if err != nil {
				logger.Debug(fmt.Sprintf("cache lookup failed for %s: %v", file.Oid, err))
			} else if ok {
				hint = externalURL
			}
		}

		if hint != "" {
			if err := precommit_cache.CheckExternalURLMismatch(hint, authoritativeURL); err != nil {
				logger.Warn(fmt.Sprintf("Warning. %s (path=%s oid=%s)", err.Error(), file.Name, file.Oid))
				fmt.Fprintln(os.Stderr, "Warning.", err.Error())
			}
		}

		if preferCacheURL && hint != "" {
			if len(authoritativeObj.AccessMethods) > 0 {
				authoritativeObj.AccessMethods[0].AccessURL = drs.AccessURL{URL: hint}
			}
		}

		if err := drsstore.WriteObject(projectdir.DRS_OBJS_PATH, authoritativeObj, file.Oid); err != nil {
			logger.Error(fmt.Sprintf("Could not WriteDrsFile for %s OID %s %v", file.Name, file.Oid, err))
			continue
		}
		logger.Info(fmt.Sprintf("Prepared File %s OID %s with DRS ID %s for commit", file.Name, file.Oid, authoritativeObj.Id))
	}

	return nil
}

// WriteDrsFile creates drsObject record from LFS file info
func WriteDrsFile(builder drs.ObjectBuilder, file drslfs.LfsFileInfo, objectPath *string) (*drs.DRSObject, error) {

	// determine drs object path: use provided objectPath if non-nil/non-empty, otherwise compute default

	// if file is in cache, hasn't been committed to git or pushed to indexd
	// create a local DRS object for it
	// TODO: determine git to gen3 project hierarchy mapping (eg repo name to project ID)
	drsId := DrsUUID(builder.ProjectID, file.Oid)
	// logger.Printf("File: %s, OID: %s, DRS ID: %s\n", file.Name, file.Oid, drsId)

	// get file info needed to create indexd record
	//path, err := GetObjectPath(projectdir.LFS_OBJS_PATH, file.Oid)
	//if err != nil {
	//	return fmt.Errorf("error getting object path for oid %s: %v", file.Oid, err)
	//}
	//if _, err := os.Stat(path); os.IsNotExist(err) {
	//	return fmt.Errorf("error: File %s does not exist in LFS objects path %s. Aborting", file.Name, path)
	//}

	drsObj, err := builder.Build(file.Name, file.Oid, file.Size, drsId)
	if err != nil {
		return nil, fmt.Errorf("error building DRS object for oid %s: %v", file.Oid, err)
	}
	if objectPath != nil && *objectPath != "" {
		drsObj.AccessMethods[0].AccessURL = drs.AccessURL{URL: *objectPath}
	}

	// write drs objects to DRS_OBJS_PATH
	err = drsstore.WriteObject(projectdir.DRS_OBJS_PATH, drsObj, file.Oid)
	if err != nil {
		return nil, fmt.Errorf("error writing DRS object for oid %s: %v", file.Oid, err)
	}
	return drsObj, nil
}

func WriteDrsObj(drsObj *drs.DRSObject, oid string, drsObjPath string) error {
	basePath := filepath.Dir(filepath.Dir(filepath.Dir(drsObjPath)))
	return drsstore.WriteObject(basePath, drsObj, oid)
}

func DrsUUID(projectId string, hash string) string {
	// create UUID based on project ID and hash
	hashStr := fmt.Sprintf("%s:%s", projectId, hash)
	return uuid.NewSHA1(NAMESPACE, []byte(hashStr)).String()
}

// creates drsObject record from file
func DrsInfoFromOid(oid string) (*drs.DRSObject, error) {
	return drsstore.ReadObject(projectdir.DRS_OBJS_PATH, oid)
}

func GetObjectPath(basePath string, oid string) (string, error) {
	return drsstore.ObjectPath(basePath, oid)
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
func FindMatchingRecord(records []drs.DRSObject, projectId string) (*drs.DRSObject, error) {
	if len(records) == 0 {
		return nil, nil
	}

	// Convert project ID to resource path format for comparison
	expectedAuthz, err := drs.ProjectToResource(projectId)
	if err != nil {
		return nil, fmt.Errorf("error converting project ID to resource format: %v", err)
	}

	// Get the first record with matching authz if exists

	for _, record := range records {
		for _, access := range record.AccessMethods {
			// assert access has Authorizations
			if access.Authorizations == nil {
				return nil, fmt.Errorf("access method for record %v missing authorizations", record)
			}
			if access.Authorizations.Value == expectedAuthz {
				return &record, nil
			}
		}
	}

	return nil, nil
}
