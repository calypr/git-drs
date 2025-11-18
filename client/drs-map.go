package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/utils"
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

func UpdateDrsObjects(logger *Logger) error {

	logger.Log("Update to DRS objects started")

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
	logger.Log("staged files: ", stagedFiles)

	// create list of lfsStagedFiles from the lfsFiles
	lfsStagedFiles := make([]LfsFileInfo, 0)
	for _, stagedFileName := range stagedFiles {
		if lfsFileInfo, ok := lfsFiles[stagedFileName]; ok {
			lfsStagedFiles = append(lfsStagedFiles, lfsFileInfo)
		}
	}
	logger.Logf("Preparing %d LFS files out of %d staged files", len(lfsStagedFiles), len(stagedFiles))

	processedDrsIds := map[string]bool{}
	// Create a DRS object for each staged LFS file
	// which will be used at push-time
	for _, file := range lfsStagedFiles {
		drsId := ComputeDeterministicUUID(file.Name, file.Oid)
		if processedDrsIds[drsId] {
			logger.Logf("Skipping staged file %s with OID %s, DRS ID %s already processed in this batch.", file.Name, file.Oid, drsId)
			continue
		}
		processedDrsIds[drsId] = true

		// check if indexd object already prepared, skip if so
		drsObjPath, err := GetObjectPath(config.DRS_OBJS_PATH, file.Oid, file.Name)
		if err != nil {
			return fmt.Errorf("error getting object path for oid %s: %v", file.Oid, err)
		}

		// confirm file contents are localized
		if !file.Downloaded {
			return fmt.Errorf("Staged file %s is not cached. Please unstage the file, then git add the file again", file.Name)
		}

		// if file is in cache, hasn't been committed to git or pushed to indexd
		// create a local DRS object for it using deterministic UUID
		logger.Logf("File: %s, OID: %s, DRS ID: %s\n", file.Name, file.Oid, drsId)

		// get file info needed to create indexd record
		path, err := GetObjectPath(config.LFS_OBJS_PATH, file.Oid, file.Name)
		if err != nil {
			return fmt.Errorf("error getting object path for oid %s: %v", file.Oid, err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("Error: File %s does not exist in LFS objects path %s. Aborting.", file.Name, path)
		}

		// get gen3 config
		cfg, err := config.LoadConfig() // should this be handled only via indexd client?
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		// get auth info from config
		gen3Auth := cfg.Servers.Gen3.Auth
		if gen3Auth.Bucket == "" {
			return fmt.Errorf("error: bucket name is empty in config file")
		}
		fileURL := fmt.Sprintf("s3://%s", filepath.Join(gen3Auth.Bucket, drsId, file.Oid))

		authzStr, err := utils.ProjectToResource(gen3Auth.ProjectID)
		if err != nil {
			return err
		}

		// create IndexdRecord
		indexdObj := IndexdRecord{
			Did:      drsId,
			FileName: file.Name,
			URLs:     []string{fileURL},
			Hashes:   HashInfo{SHA256: file.Oid},
			Size:     file.Size,
			Authz:    []string{authzStr},
		}

		// write drs objects to DRS_OBJS_PATH
		err = writeDrsObj(indexdObj, file.Oid, drsObjPath)
		if err != nil {
			return fmt.Errorf("error writing DRS object for oid %s: %v", file.Oid, err)
		}
		logger.Logf("Prepared %s with DRS ID %s for commit", file.Name, indexdObj.Did)
		logger.Log("DRS OBJECT PATH: ++++++++++++++++++++++++++++", drsObjPath)
	}

	return nil
}

func writeDrsObj(indexdObj IndexdRecord, oid string, drsObjPath string) error {
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
func DrsInfoFromOid(oid string, path string) (*IndexdRecord, error) {
	// unmarshal the DRS object
	path, err := GetObjectPath(config.DRS_OBJS_PATH, oid, path)
	if err != nil {
		return nil, fmt.Errorf("error getting object path for oid %s: %v", oid, err)
	}

	indexdObjBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading DRS object for oid %s: %v", oid, err)
	}

	var indexdObj IndexdRecord
	err = json.Unmarshal(indexdObjBytes, &indexdObj)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling DRS object for oid %s: %v", oid, err)
	}

	return &indexdObj, nil
}

func GetObjectPath(basePath string, oid string, path string) (string, error) {
	// check that oid is a valid sha256 hash
	if len(oid) != 64 {
		return "", errors.New(fmt.Sprintf("Error: %s is not a valid sha256 hash", oid))
	}

	return filepath.Join(basePath, oid[:2], oid[2:4], oid, path), nil
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
