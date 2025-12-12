package drsmap

// Utilities to map between Git LFS files and DRS objects

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
	"github.com/calypr/git-drs/projectdir"
	"github.com/calypr/git-drs/utils"
	"github.com/google/uuid"
)

// NAMESPACE is the UUID namespace used for generating DRS UUIDs
var NAMESPACE = uuid.NewMD5(uuid.NameSpaceURL, []byte("calypr.org"))

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

func PushLocalDrsObjects(drsClient client.DRSClient, myLogger *log.Logger) error {
	// Gather all objects in .drs/lfs/objects store
	drsLfsObjs, err := drs.GetDrsLfsObjects(myLogger)
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
		records, err := drsClient.GetObjectByHash(sum)
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
			myLogger.Printf("Drs record not found in sha256 map %s", drsObjKey)
		}
		if _, statErr := os.Stat(val.Name); os.IsNotExist(statErr) {
			myLogger.Printf("Error: Object record found locally, but file does not exist locally. Registering Record %s", val.Name)
			_, err = drsClient.RegisterRecord(val)
			if err != nil {
				return err
			}

		} else {
			_, err = drsClient.RegisterFile(drsObjKey)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func PullRemoteDrsObjects(drsClient client.DRSClient, logger *log.Logger) error {
	objChan, err := drsClient.ListObjectsByProject(drsClient.GetProjectId())
	if err != nil {
		return err
	}
	writtenObjs := 0
	for drsObj := range objChan {
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
	logger.Printf("Wrote %d new objs to object store", writtenObjs)
	return nil
}

func UpdateDrsObjects(drsClient client.DRSClient, logger *log.Logger) error {

	logger.Print("Update to DRS objects started")

	// get all lfs files
	lfsFiles, err := getAllLfsFiles()
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
			logger.Printf("Skipping record creation, file %s with OID %s already exists in DRS objects path %s", file.Name, file.Oid, drsObjPath)
			continue
		}

		// if file is in cache, hasn't been committed to git or pushed to indexd
		// create a local DRS object for it
		// TODO: determine git to gen3 project hierarchy mapping (eg repo name to project ID)
		drsId := DrsUUID(projectId, file.Oid)
		logger.Printf("File: %s, OID: %s, DRS ID: %s\n", file.Name, file.Oid, drsId)

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
		logger.Printf("Prepared %s with DRS ID %s for commit", file.Name, drsObj.Id)
	}

	return nil
}

func WriteDrsObj(drsObj *drs.DRSObject, oid string, drsObjPath string) error {
	// get object bytes
	indexdObjBytes, err := json.Marshal(drsObj)
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

// creates index record from file
func DrsInfoFromOid(oid string) (*drs.DRSObject, error) {
	// unmarshal the DRS object
	path, err := GetObjectPath(projectdir.DRS_OBJS_PATH, oid)
	if err != nil {
		return nil, fmt.Errorf("error getting object path for oid %s: %v", oid, err)
	}

	indexdObjBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading DRS object for oid %s: %v", oid, err)
	}

	var indexdObj drs.DRSObject
	err = json.Unmarshal(indexdObjBytes, &indexdObj)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling DRS object for oid %s: %v", oid, err)
	}

	return &indexdObj, nil
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
	err = json.Unmarshal(out, &lfsOutput)
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

func getAllLfsFiles() (map[string]LfsFileInfo, error) {
	// get all LFS files' info using json
	cmd := exec.Command("git", "lfs", "ls-files", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error running git lfs ls-files: %v", err)
	}

	var lfsFiles LfsLsOutput
	err = json.Unmarshal(out, &lfsFiles)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling git lfs ls-files output: %v", err)
	}

	// create a map of LFS file info
	lfsFileMap := make(map[string]LfsFileInfo)
	for _, file := range lfsFiles.Files {
		lfsFileMap[file.Name] = file
	}

	return lfsFileMap, nil
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
			if access.Authorizations.Value == expectedAuthz {
				return &record, nil
			}
		}
	}

	return nil, nil
}
