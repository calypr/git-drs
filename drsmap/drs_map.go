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

	processed := 0
	total := len(drsLfsObjs)
	for drsObjKey, val := range drsLfsObjs {
		processed++
		if processed%100 == 0 || processed == total {
			myLogger.Info(fmt.Sprintf("Pushing local DRS objects: %d/%d...", processed, total))
		}
		records, err := drsClient.GetObjectByHash(context.Background(), &hash.Checksum{
			Checksum: drsObjKey,
			Type:     hash.ChecksumTypeSHA256,
		})
		if err != nil {
			return fmt.Errorf("error checking server for %s: %v", drsObjKey, err)
		}

		if len(records) > 0 {
			myLogger.Debug(fmt.Sprintf("Object %s (path: %s) already exists on DRS server, skipping registration", drsObjKey, val.Name))
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
				myLogger.Info(fmt.Sprintf("Pushing file %s to DRS server (OID: %s)", val.Name, drsObjKey))
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
func UpdateDrsObjects(drsClient client.DRSClient, builder drs.ObjectBuilder, gitRemoteName, gitRemoteLocation string, branches []string, checkAll bool, logger *slog.Logger) error {

	if logger == nil {
		return fmt.Errorf("logger is required")
	}
	logger.Debug("Update to DRS objects started")

	lfsFileMap := make(map[string]lfs.LfsFileInfo)
	repoDir, err := os.Getwd()
	if err != nil {
		return err
	}

	if checkAll {
		logger.Debug("Performing deep LFS file scan (checkAll=true)")
		if err := addFilesFromLsFiles(repoDir, logger, lfsFileMap); err != nil {
			logger.Warn(fmt.Sprintf("Warning: deep discovery encountered issues: %v", err))
		}
	} else {
		// Normal case: use push dry-run
		if gitRemoteName == "" {
			gitRemoteName = "origin"
		}
		for _, branch := range branches {
			ref := branch
			if branch != "HEAD" && !strings.HasPrefix(branch, "refs/") {
				ref = fmt.Sprintf("refs/heads/%s", branch)
			}
			out, err := lfs.RunPushDryRun(context.Background(), repoDir, lfs.DryRunSpec{Remote: gitRemoteName, Ref: ref}, logger)
			if err != nil {
				return err
			}
			if err := parseLfsPushDryRun(out, logger, lfsFileMap); err != nil {
				return err
			}
		}

		// Enrich normal discovery with sizes
		for oid, info := range lfsFileMap {
			absPath := info.Name
			if !filepath.IsAbs(absPath) {
				absPath = filepath.Join(repoDir, info.Name)
			}
			if stat, err := os.Stat(absPath); err == nil {
				info.Size = stat.Size()
				lfsFileMap[oid] = info
			} else {
				// If not on disk, try reaching into DRS cache since dry-run found it
				if drsObj, err := DrsInfoFromOid(oid); err == nil {
					info.Size = drsObj.Size
					lfsFileMap[oid] = info
				} else {
					logger.Warn(fmt.Sprintf("Object %s (path %s) identified for push but missing from disk and local DRS cache. This record may be broken.", oid, info.Name))
				}
			}
		}
	}

	return UpdateDrsObjectsWithFiles(builder, lfsFileMap, UpdateOptions{Logger: logger, DrsClient: drsClient})
}

type UpdateOptions struct {
	Cache          *precommit_cache.Cache
	PreferCacheURL bool
	Logger         *slog.Logger
	DrsClient      client.DRSClient
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

	processed := 0
	total := len(lfsFiles)
	for _, file := range lfsFiles {
		processed++
		if processed%100 == 0 || processed == total {
			opts.Logger.Info(fmt.Sprintf("Updating DRS objects: %d/%d...", processed, total))
		}

		// Optimization: If local record exists, we've already prepared it
		if _, err := DrsInfoFromOid(file.Oid); err == nil {
			continue
		}

		// check if record already exists remotely
		if opts.DrsClient != nil {
			records, err := opts.DrsClient.GetObjectByHash(context.Background(), &hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: file.Oid})
			if err == nil && len(records) > 0 {
				matching, _ := FindMatchingRecord(records, opts.DrsClient.GetProjectId(), file.Name)
				if matching != nil {
					opts.Logger.Debug(fmt.Sprintf("Object %s (path: %s) already indexed remote, skipping local preparation", file.Oid, file.Name))
					continue
				}
			}
		}

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
		opts.Logger.Debug(fmt.Sprintf("Prepared File %s OID %s with DRS ID %s for commit", file.Name, file.Oid, authoritativeObj.Id))
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

func addFilesFromLsFiles(repoDir string, logger *slog.Logger, lfsFileMap map[string]lfs.LfsFileInfo) error {
	cmd := exec.Command("git", "lfs", "ls-files")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		logger.Debug(fmt.Sprintf("git lfs ls-files failed: %v", err))
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var truncatedMap map[string]string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 3 {
			continue
		}
		oid := parts[0]
		path := parts[2]

		if _, exists := lfsFileMap[path]; !exists {
			absPath := path
			if repoDir != "" && !filepath.IsAbs(path) {
				absPath = filepath.Join(repoDir, path)
			}

			size := int64(0)
			fullOid := ""

			// Stage 1: Try reading as pointer from disk
			if stat, err := os.Stat(absPath); err == nil {
				if stat.Size() < 2048 {
					if data, err := os.ReadFile(absPath); err == nil {
						s := string(data)
						if strings.Contains(s, "version https://git-lfs.github.com/spec/v1") && strings.Contains(s, "oid sha256:") {
							fullOid, size = parsePointer(s)
						}
					}
				} else {
					size = stat.Size()
				}
			}

			// Stage 2: Try getting pointer from git index (fallback for non-checked-out files)
			if len(fullOid) != 64 {
				cmd := exec.Command("git", "show", ":"+path)
				cmd.Dir = repoDir
				if out, err := cmd.Output(); err == nil {
					s := string(out)
					if strings.Contains(s, "version https://git-lfs.github.com/spec/v1") && strings.Contains(s, "oid sha256:") {
						fullOid, size = parsePointer(s)
					}
				}
			}

			// Stage 3: Resolve truncated OID via local cache
			if len(fullOid) != 64 && len(oid) < 64 {
				if truncatedMap == nil {
					truncatedMap = buildTruncatedOidMap(logger)
				}
				if full, ok := truncatedMap[oid]; ok {
					fullOid = full
					if size == 0 {
						if drsObj, err := DrsInfoFromOid(fullOid); err == nil {
							size = drsObj.Size
						}
					}
				}
			} else if len(oid) == 64 {
				fullOid = oid
			}

			// Stage 4: Last resort - use git lfs ls-files --debug <path>
			if len(fullOid) != 64 {
				cmd := exec.Command("git", "lfs", "ls-files", "--debug", path)
				cmd.Dir = repoDir
				if out, err := cmd.Output(); err == nil {
					lines := strings.Split(string(out), "\n")
					for _, l := range lines {
						l = strings.TrimSpace(l)
						if strings.HasPrefix(l, "oid: sha256:") {
							fullOid = strings.TrimPrefix(l, "oid: sha256:")
						} else if strings.HasPrefix(l, "size: ") {
							fmt.Sscanf(l, "size: %d", &size)
						}
					}
				}
			}

			if len(fullOid) == 64 {
				lfsFileMap[path] = lfs.LfsFileInfo{
					Name:    path,
					Size:    size,
					OidType: "sha256",
					Oid:     fullOid,
					Version: "https://git-lfs.github.com/spec/v1",
				}
			} else {
				logger.Warn(fmt.Sprintf("Skipping %s: could not resolve truncated OID %s to a full SHA256. Ensure this file exists in the repository as a valid LFS pointer or has a local DRS record.", path, oid))
			}
		}
	}
	return nil
}

func parsePointer(content string) (string, int64) {
	var oid string
	var size int64
	lines := strings.Split(content, "\n")
	for _, pl := range lines {
		pl = strings.TrimSpace(pl)
		if strings.HasPrefix(pl, "oid sha256:") {
			oid = strings.TrimPrefix(pl, "oid sha256:")
		} else if strings.HasPrefix(pl, "size ") {
			fmt.Sscanf(pl, "size %d", &size)
		}
	}
	return oid, size
}

func buildTruncatedOidMap(logger *slog.Logger) map[string]string {
	m := make(map[string]string)
	objs, err := lfs.GetPendingObjects(logger)
	if err != nil {
		return m
	}
	for _, obj := range objs {
		if len(obj.OID) >= 10 {
			prefix := obj.OID[:10]
			m[prefix] = obj.OID
		}
	}
	return m
}
