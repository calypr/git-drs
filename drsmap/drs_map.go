package drsmap

// Utilities to map between Git LFS files and DRS objects

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"regexp"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/git-drs/precommit_cache"
	"github.com/google/uuid"
)

// execCommand is a variable to allow mocking in tests
var execCommand = exec.Command
var execCommandContext = exec.CommandContext

func PushLocalDrsObjects(drsClient client.DRSClient, myLogger *slog.Logger, shouldStage bool) error {
	// Gather all objects in .git/drs/lfs/objects store
	drsLfsObjs, err := lfs.GetDrsLfsObjects(myLogger)
	if err != nil {
		return err
	}

	for drsObjKey, val := range drsLfsObjs {
		records, err := drsClient.GetObjectByHash(context.Background(), &hash.Checksum{
			Checksum: drsObjKey,
			Type:     hash.ChecksumTypeSHA256,
		})
		if err != nil {
			return fmt.Errorf("error checking server for %s: %v", drsObjKey, err)
		}

		if len(records) > 0 {
			myLogger.Info(fmt.Sprintf("Object %s already exists on DRS server, skipping registration", drsObjKey))
		} else {
			// Check if we have the actual blob locally
			hasBlob := false
			if info, statErr := os.Stat(val.Name); statErr == nil {
				if info.Size() > 2048 {
					hasBlob = true
				} else {
					if data, err := os.ReadFile(val.Name); err == nil {
						s := strings.TrimSpace(string(data))
						if !strings.Contains(s, "version https://git-lfs.github.com/spec/v1") {
							hasBlob = true
						}
					}
				}
			}

			if !hasBlob {
				myLogger.Info(fmt.Sprintf("Object record found locally, but blob does not exist locally. Registering metadata only for %s", val.Name))
				_, err = drsClient.RegisterRecord(context.Background(), val)
				if err != nil {
					return fmt.Errorf("failed to register record for %s: %v", val.Name, err)
				}
			} else {
				myLogger.Info(fmt.Sprintf("Pushing file %s (OID: %s) to DRS server", val.Name, drsObjKey))
				_, err = drsClient.RegisterFile(context.Background(), drsObjKey, val.Name)
				if err != nil {
					return fmt.Errorf("failed to register file %s: %v", val.Name, err)
				}
			}
		}

		// Optional: Stage the object in the working tree
		if shouldStage {
			if _, statErr := os.Stat(val.Name); os.IsNotExist(statErr) {
				myLogger.Info(fmt.Sprintf("Staging missing LFS pointer for: %s", val.Name))

				dir := filepath.Dir(val.Name)
				if dir != "." && dir != "/" {
					if err := os.MkdirAll(dir, 0755); err != nil {
						myLogger.Error(fmt.Sprintf("Failed to create directory %s: %v", dir, err))
						continue
					}
				}

				if err := lfs.CreateLfsPointer(val, val.Name); err != nil {
					myLogger.Error(fmt.Sprintf("Failed to create LFS pointer for %s: %v", val.Name, err))
					continue
				}

				cmd := exec.Command("git", "add", val.Name)
				if err := cmd.Run(); err != nil {
					myLogger.Error(fmt.Sprintf("Failed to git add %s: %v", val.Name, err))
				}
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

	if logger == nil {
		return fmt.Errorf("logger is required")
	}
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
		drsID := DrsUUID(builder.ProjectID, file.Oid)
		authoritativeObj, err := builder.Build(file.Name, file.Oid, file.Size, drsID)
		if err != nil {
			opts.Logger.Error(fmt.Sprintf("Could not build DRS object for %s OID %s %v", file.Name, file.Oid, err))
			continue
		}

		authoritativeURL := ""
		if len(authoritativeObj.AccessMethods) > 0 {
			authoritativeURL = authoritativeObj.AccessMethods[0].AccessURL.URL
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
			if len(authoritativeObj.AccessMethods) > 0 {
				authoritativeObj.AccessMethods[0].AccessURL = drs.AccessURL{URL: hint}
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

// FindMatchingRecord finds a record from the list that matches the given project ID authz.
// If multiple records match the authz, it uses the filenameHint (if provided) to disambiguate.
func FindMatchingRecord(records []drs.DRSObject, projectId string, filenameHint string) (*drs.DRSObject, error) {
	if len(records) == 0 {
		return nil, nil
	}

	// Convert project ID to resource path format for comparison
	expectedAuthz, err := common.ProjectToResource(projectId)
	if err != nil {
		return nil, fmt.Errorf("error converting project ID to resource format: %v", err)
	}

	var projectMatches []*drs.DRSObject

	for i := range records {
		record := &records[i]
		matchesProject := false

		// Check AccessMethods for authz (standard GA4GH/Gen3 DRS)
		for _, access := range record.AccessMethods {
			if access.Authorizations != nil && access.Authorizations.Value == expectedAuthz {
				matchesProject = true
				break
			}
		}

		// Fallback: check top-level authz if it exists as an extension (common in some Indexd responses)
		// Since we don't have direct access to the struct fields beyond common ones,
		// we'll rely on the AccessMethods check primarily, but if data-client-drs
		// populates a top-level field we'd want to check it.

		if matchesProject {
			projectMatches = append(projectMatches, record)
			// If we have a filename hint, check if it matches
			if filenameHint != "" && (record.Name == filenameHint || strings.HasSuffix(record.Name, "/"+filenameHint)) {
				return record, nil
			}
		}
	}

	// If we have any project matches but no perfect name match, return the first project match
	if len(projectMatches) > 0 {
		return projectMatches[0], nil
	}

	return nil, nil
}

// output of git lfs ls-files
type LfsLsOutput struct {
	Files []lfs.LfsFileInfo `json:"files"`
}

// parseLfsPushDryRun parses the output of `git lfs push --dry-run` to identify objects needing push.
func parseLfsPushDryRun(out string, logger *slog.Logger, lfsFileMap map[string]lfs.LfsFileInfo) error {
	if strings.TrimSpace(out) == "" {
		return nil
	}

	sha256Re := regexp.MustCompile(`(?i)^[a-f0-9]{64}$`)

	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		var oid string
		var pathStart int
		for i, p := range parts {
			if sha256Re.MatchString(p) {
				oid = p
				pathStart = i + 1
				break
			}
		}

		if oid == "" || pathStart >= len(parts) {
			continue
		}

		// Skip leading '=>' or '->' if present
		if parts[pathStart] == "=>" || parts[pathStart] == "->" {
			pathStart++
		}

		if pathStart >= len(parts) {
			continue
		}
		path := strings.Join(parts[pathStart:], " ")

		// Remove size suffix if present: "path/to/file.dat (100 KB)"
		if idx := strings.LastIndex(path, " ("); idx != -1 && strings.HasSuffix(path, ")") {
			path = strings.TrimSpace(path[:idx])
		}

		lfsFileMap[oid] = lfs.LfsFileInfo{
			Name: path,
			Oid:  oid,
		}
	}
	return nil
}
