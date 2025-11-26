package drsmap

// Utilities to map between Git LFS files and DRS objects

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/projectdir"
	"github.com/google/uuid"
)

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

func UpdateDrsObjects(drsClient client.DRSClient, logger *log.Logger) error {

	logger.Print("Update to DRS objects started")

	// get the name of repository
	repoName, err := GetRepoNameFromGit()
	if err != nil {
		return fmt.Errorf("Unable to fetch repository website location: %v", err)
	}

	// get all lfs files
	lfsFiles, err := getAllLfsFiles()
	if err != nil {
		return fmt.Errorf("error getting all LFS files: %v", err)
	}

	// get all staged files
	stagedFiles, err := getStagedFiles()
	if err != nil {
		return fmt.Errorf("error getting staged files: %v", err)
	}
	logger.Print("staged files: ", stagedFiles)

	// create list of lfsStagedFiles from the lfsFiles
	lfsStagedFiles := make([]LfsFileInfo, 0)
	for _, stagedFileName := range stagedFiles {
		if lfsFileInfo, ok := lfsFiles[stagedFileName]; ok {
			lfsStagedFiles = append(lfsStagedFiles, lfsFileInfo)
		}
	}
	logger.Printf("Preparing %d LFS files out of %d staged files", len(lfsStagedFiles), len(stagedFiles))

	// Create a DRS object for each staged LFS file
	// which will be used at push-time
	for _, file := range lfsStagedFiles {

		// check hash to see if record already exists in indexd (source of truth)
		records, err := drsClient.GetObjectsByHash(file.OidType, file.Oid)
		if err != nil {
			return fmt.Errorf("error getting object by hash %s: %v", file.Oid, err)
		}

		// check if record with matching project ID already exists in indexd
		projectId := drsClient.GetProjectId()
		if projectId == "" {
			return fmt.Errorf("Error getting project ID")
		}
		matchingRecord, err := FindMatchingRecord(records, projectId)
		if err != nil {
			return fmt.Errorf("Error finding matching record for project %s: %v", projectId, err)
		}

		// skip if matching record exists
		if matchingRecord != nil {
			logger.Printf("Skipping staged file %s: OID %s already exists in indexd", file.Name, file.Oid)
			continue
		}

		// check if indexd object already prepared, skip if so
		drsObjPath, err := GetObjectPath(projectdir.DRS_OBJS_PATH, file.Oid)
		if err != nil {
			return fmt.Errorf("error getting object path for oid %s: %v", file.Oid, err)
		}
		if _, err := os.Stat(drsObjPath); err == nil {
			logger.Printf("Skipping staged file %s with OID %s, already exists in DRS objects path %s", file.Name, file.Oid, drsObjPath)
			continue
		}

		// confirm file contents are localized
		if !file.Downloaded {
			return fmt.Errorf("Staged file %s is not cached. Please unstage the file, then git add the file again", file.Name)
		}

		// if file is in cache, hasn't been committed to git or pushed to indexd
		// create a local DRS object for it
		// TODO: determine git to gen3 project hierarchy mapping (eg repo name to project ID)
		drsId := DrsUUID(repoName, file.Oid)
		logger.Printf("File: %s, OID: %s, DRS ID: %s\n", file.Name, file.Oid, drsId)

		// get file info needed to create indexd record
		path, err := GetObjectPath(projectdir.LFS_OBJS_PATH, file.Oid)
		if err != nil {
			return fmt.Errorf("error getting object path for oid %s: %v", file.Oid, err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("Error: File %s does not exist in LFS objects path %s. Aborting.", file.Name, path)
		}

		logger.Printf("Error, hit broken code block DRS object for staged file %s with OID %s", file.Name, file.Oid)
		// TODO: why is this here and not in the DRSClient implementation?
		/*
			bucket := drsClient.GetDefaultBucketName()
			if bucket == "" {
				return fmt.Errorf("error: bucket name is empty in config file")
			}
			fileURL := fmt.Sprintf("s3://%s", filepath.Join(bucket, drsId, file.Oid))

			authzStr, err := utils.ProjectToResource(drsClient.GetProjectId())
			if err != nil {
				return err
			}

			// create IndexdRecord
			indexdObj := drs.DRSObject{
				Id:            drsId,
				Name:          file.Name,
				AccessMethods: []drs.AccessMethod{{AccessURL: drs.AccessURL{URL: fileURL}}},
				//URLs:          []string{fileURL},
				//Hashes:    HashInfo{SHA256: file.Oid},
				Checksums: []drs.Checksum{drs.Checksum{Checksum: file.Oid, Type: drs.ChecksumTypeSHA256}},
				Size:      file.Size,
				Authz:     []string{authzStr},
			}

			// write drs objects to DRS_OBJS_PATH
			err = writeDrsObj(indexdObj, file.Oid, drsObjPath)
			if err != nil {
				return fmt.Errorf("error writing DRS object for oid %s: %v", file.Oid, err)
			}
			logger.Logf("Prepared %s with DRS ID %s for commit", file.Name, indexdObj.Id)
		*/
	}

	return nil
}

func writeDrsObj(indexdObj drs.DRSObject, oid string, drsObjPath string) error {
	// get object bytes
	indexdObjBytes, err := json.Marshal(indexdObj)
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

func DrsUUID(repoName string, hash string) string {
	// FIXME: use different UUID method? Used same method as g3t
	hashStr := fmt.Sprintf("%s:%s", repoName, hash)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(hashStr)).String()
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
		return "", errors.New(fmt.Sprintf("Error: %s is not a valid sha256 hash", oid))
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

func GetRepoNameFromGit() (string, error) {
	// prefer simple os.Exec over using go-git
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
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
	// expectedAuthz, err := utils.ProjectToResource(projectId)
	// if err != nil {
	//	return nil, fmt.Errorf("error converting project ID to resource format: %v", err)
	// }

	//TODO: determine what filtering logic should be here
	// Get the first record with matching authz if exists
	for _, record := range records {
		//for _, access := range record.AccessMethods {
		//for _, authz := range access.Authorizations.Value {
		//if authz == expectedAuthz {
		return &record, nil
		//}
		//	}
		//}
	}

	return nil, nil
}
