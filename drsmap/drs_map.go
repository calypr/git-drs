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
	dataClient "github.com/calypr/data-client/g3client"
	drs "github.com/calypr/data-client/indexd/drs"
	hash "github.com/calypr/data-client/indexd/hash"
	"github.com/calypr/data-client/upload"
	"github.com/calypr/git-drs/client"
	localLfs "github.com/calypr/git-drs/lfs"
	"github.com/calypr/git-drs/projectdir"
	"github.com/calypr/git-drs/utils"
	"github.com/google/uuid"
)

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

	cmd := exec.CommandContext(ctx, "git", "lfs", "push", "--dry-run", spec.Remote, spec.Ref)
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

func PushLocalDrsObjects(drsClient client.DRSClient, gen3Client dataClient.Gen3Interface, bucketName string, upsert bool, myLogger *slog.Logger) error {
	// Gather all objects in .git/drs/lfs/objects store
	drsLfsObjs, err := localLfs.GetDrsLfsObjects(myLogger)
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
			filePath, err := GetObjectPath(projectdir.LFS_OBJS_PATH, drsObjKey)
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
				drsObjPath, err = GetObjectPath(projectdir.DRS_OBJS_PATH, oid)
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

func UpdateDrsObjects(drsClient client.DRSClient, gitRemoteName, gitRemoteLocation string, branches []string, logger *slog.Logger) error {

	logger.Debug("Update to DRS objects started")

	// get all lfs files
	lfsFiles, err := GetAllLfsFiles(gitRemoteName, gitRemoteLocation, branches, logger)
	if err != nil {
		return fmt.Errorf("error getting all LFS files: %v", err)
	}

	// get project
	projectId := drsClient.GetProjectId()
	if projectId == "" {
		return fmt.Errorf("no project configured: %v", err)
	}

	// create a DRS object for each LFS file
	// which will be used at push-time
	for _, file := range lfsFiles {
		// check if indexd object already prepared, skip if so
		drsObjPath, err := GetObjectPath(projectdir.DRS_OBJS_PATH, file.Oid)
		if err != nil {
			return fmt.Errorf("error getting object path for oid %s: %v", file.Oid, err)
		}
		if _, err := os.Stat(drsObjPath); err == nil {
			logger.Debug(fmt.Sprintf("Skipping record creation, file %s with OID %s already exists in DRS objects path %s", file.Name, file.Oid, drsObjPath))
			continue
		}

		// if file is in cache, hasn't been committed to git or pushed to indexd
		// create a local DRS object for it
		// TODO: determine git to gen3 project hierarchy mapping (eg repo name to project ID)
		drsId := DrsUUID(projectId, file.Oid)
		// logger.Printf("File: %s, OID: %s, DRS ID: %s\n", file.Name, file.Oid, drsId)

		// get file info needed to create indexd record
		path, err := GetObjectPath(projectdir.LFS_OBJS_PATH, file.Oid)
		if err != nil {
			return fmt.Errorf("error getting object path for oid %s: %v", file.Oid, err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("error: File %s does not exist in LFS objects path %s. Aborting", file.Name, path)
		}

		drsObj, err := drsClient.BuildDrsObj(file.Name, file.Oid, file.Size, drsId)
		if err != nil {
			return fmt.Errorf("error building DRS object for oid %s: %v", file.Oid, err)
		}

		// write drs objects to DRS_OBJS_PATH
		err = WriteDrsObj(drsObj, file.Oid, drsObjPath)
		if err != nil {
			return fmt.Errorf("error writing DRS object for oid %s: %v", file.Oid, err)
		}
		logger.Debug(fmt.Sprintf("Prepared File %s OID %s with DRS ID %s for commit", file.Name, file.Oid, drsObj.Id))
	}

	return nil
}

func WriteDrsObj(drsObj *drs.DRSObject, oid string, drsObjPath string) error {
	// get object bytes
	indexdObjBytes, err := sonic.ConfigFastest.Marshal(drsObj)
	if err != nil {
		return fmt.Errorf("error marshalling indexd object for oid %s: %v", oid, err)
	}
	if err := os.MkdirAll(filepath.Dir(drsObjPath), 0755); err != nil {
		return fmt.Errorf("error creating directory for %s: %v", drsObjPath, err)
	}

	// write indexd obj to file as json
	err = os.WriteFile(drsObjPath, indexdObjBytes, 0644)
	if err != nil {
		return fmt.Errorf("error writing %s: %v", drsObjPath, err)
	}
	return nil
}

func DrsUUID(projectId string, hash string) string {
	// create UUID based on project ID and hash
	hashStr := fmt.Sprintf("%s:%s", projectId, hash)
	return uuid.NewSHA1(NAMESPACE, []byte(hashStr)).String()
}

// creates drsObject record from file
func DrsInfoFromOid(oid string) (*drs.DRSObject, error) {
	// unmarshal the DRS object
	path, err := GetObjectPath(projectdir.DRS_OBJS_PATH, oid)
	if err != nil {
		return nil, fmt.Errorf("error getting object path for oid %s: %v", oid, err)
	}

	drsObjBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading DRS object for oid %s: %v", oid, err)
	}

	var drsObject drs.DRSObject
	err = sonic.ConfigFastest.Unmarshal(drsObjBytes, &drsObject)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling DRS object for oid %s: %v", oid, err)
	}

	return &drsObject, nil
}

func GetObjectPath(basePath string, oid string) (string, error) {
	// check that oid is a valid sha256 hash
	if len(oid) != 64 {
		return "", fmt.Errorf("error: %s is not a valid sha256 hash", oid)
	}

	return filepath.Join(basePath, oid[:2], oid[2:4], oid), nil
}

////////////////
// LFS HELPERS /
////////////////

// checkIfLfsFile checks if a given file is tracked by Git LFS
// Returns true and file info if it's an LFS file, false otherwise
func CheckIfLfsFile(fileName string) (bool, *LfsFileInfo, error) {
	// Use git lfs ls-files -I to check if specific file is LFS tracked
	cmd := exec.Command("git", "lfs", "ls-files", "-I", fileName, "--json")
	out, err := cmd.Output()
	if err != nil {
		// If git lfs ls-files returns error, the file is not LFS tracked
		return false, nil, nil
	}

	// If output is empty, file is not LFS tracked
	if len(strings.TrimSpace(string(out))) == 0 {
		return false, nil, nil
	}

	// Parse the JSON output
	var lfsOutput LfsLsOutput
	err = sonic.ConfigFastest.Unmarshal(out, &lfsOutput)
	if err != nil {
		return false, nil, fmt.Errorf("error unmarshaling git lfs ls-files output for %s: %v", fileName, err)
	}

	// If no files in output, not LFS tracked
	if len(lfsOutput.Files) == 0 {
		return false, nil, nil
	}

	// Convert to our LfsFileInfo struct
	file := lfsOutput.Files[0]
	lfsInfo := &LfsFileInfo{
		Name:       file.Name,
		Size:       file.Size,
		Checkout:   file.Checkout,
		Downloaded: file.Downloaded,
		OidType:    file.OidType,
		Oid:        file.Oid,
		Version:    file.Version,
	}

	return true, lfsInfo, nil
}

func getStagedFiles() ([]string, error) {
	// chose exec here for performance over using go-git
	// tradeoff is very rare concurrency problems which currently aren't relevant to the pre-commit
	// FIXME: filter out files that have been deleted? Bug: if git rm, the DRS object still created
	cmd := exec.Command("git", "diff", "--name-only", "--cached")
	cmdOut, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error running git command: %w: out: '%s'", err, string(cmdOut))
	}
	stagedFiles := strings.Split(strings.TrimSpace(string(cmdOut)), "\n")
	return stagedFiles, nil
}

func GetRepoNameFromGit(remote string) (string, error) {
	// prefer simple os.Exec over using go-git
	cmd := exec.Command("git", "config", "--get", fmt.Sprintf("remote.%s.url", remote))
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	remoteURL := strings.TrimSpace(string(out))
	repoName := strings.TrimSuffix(filepath.Base(remoteURL), ".git")
	return repoName, nil
}

func GetAllLfsFiles(gitRemoteName, gitRemoteLocation string, branches []string, logger *slog.Logger) (map[string]LfsFileInfo, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	repoDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	// no timeout for now
	ctx := context.Background()
	// If needed, can re-enable timeout
	// Set a timeout context for git commands, 3 minutes should be enough
	//ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	//defer cancel()

	if gitRemoteName == "" {
		gitRemoteName = "origin"
	}
	if gitRemoteLocation != "" {
		logger.Debug(fmt.Sprintf("Using git remote %s at %s for LFS dry-run", gitRemoteName, gitRemoteLocation))
	} else {
		logger.Debug(fmt.Sprintf("Using git remote %s for LFS dry-run", gitRemoteName))
	}

	refs := buildLfsRefs(branches)
	lfsFileMap := make(map[string]LfsFileInfo)
	for _, ref := range refs {
		spec := LfsDryRunSpec{
			Remote: gitRemoteName,
			Ref:    ref,
		}
		out, err := RunLfsPushDryRun(ctx, repoDir, spec, logger)
		if err != nil {
			return nil, err
		}

		if err := addLfsFilesFromDryRun(out, repoDir, logger, lfsFileMap); err != nil {
			return nil, err
		}
	}

	return lfsFileMap, nil
}

func buildLfsRefs(branches []string) []string {
	if len(branches) == 0 {
		return []string{"HEAD"}
	}
	refs := make([]string, 0, len(branches))
	seen := make(map[string]struct{})
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		ref := branch
		if branch != "HEAD" && !strings.HasPrefix(branch, "refs/") {
			ref = fmt.Sprintf("refs/heads/%s", branch)
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		return []string{"HEAD"}
	}
	return refs
}

func addLfsFilesFromDryRun(out, repoDir string, logger *slog.Logger, lfsFileMap map[string]LfsFileInfo) error {
	// Log when dry-run returns no output to help with debugging
	if strings.TrimSpace(out) == "" {
		logger.Debug("No LFS files to push (dry-run returned no output)")
		return nil
	}

	// accept lowercase or uppercase hex
	sha256Re := regexp.MustCompile(`(?i)^[a-f0-9]{64}$`)

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		oid := parts[1]
		path := parts[len(parts)-1]

		// Validate OID looks like a SHA256 hex string.
		if !sha256Re.MatchString(oid) {
			logger.Debug(fmt.Sprintf("skipping LFS line with invalid oid %q: %q", oid, line))
			continue
		}

		// see https://github.com/calypr/git-drs/issues/124#issuecomment-3721837089
		if oid == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" && strings.Contains(path, ".gitattributes") {
			logger.Debug(fmt.Sprintf("skipping empty LFS pointer for %s", path))
			continue
		}
		// Remove a trailing parenthetical suffix from p, e.g.:
		// "path/to/file.dat (100 KB)" -> "path/to/file.dat"
		if idx := strings.LastIndex(path, " ("); idx != -1 && strings.HasSuffix(path, ")") {
			path = strings.TrimSpace(path[:idx])
		}
		size := int64(0)
		absPath := path
		if repoDir != "" && !filepath.IsAbs(path) {
			absPath = filepath.Join(repoDir, path)
		}
		if stat, err := os.Stat(absPath); err == nil {
			size = stat.Size()
		} else {
			logger.Error(fmt.Sprintf("could not stat file %s: %v", path, err))
			continue
		}

		// If the file is small, read it and detect LFS pointer signature.
		// Pointer files are textual and include the LFS spec version + an oid line.
		if size > 0 && size < 2048 {
			if data, readErr := os.ReadFile(absPath); readErr == nil {
				s := strings.TrimSpace(string(data))
				if strings.Contains(s, "version https://git-lfs.github.com/spec/v1") && strings.Contains(s, "oid sha256:") {
					logger.Warn(fmt.Sprintf("WARNING: Detected upload of lfs pointer file %s skipping", path))
					continue
				}
			}
		}

		lfsFileMap[path] = LfsFileInfo{
			Name:    path,
			Size:    size,
			OidType: "sha256",
			Oid:     oid,
			Version: "https://git-lfs.github.com/spec/v1",
		}
		//logger.Printf("GetAllLfsFiles added LFS file %s", path)
	}

	return nil
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
	expectedAuthz, err := utils.ProjectToResource(projectId)
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
