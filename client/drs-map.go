package client

import (
	"bytes"
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

	// init indexd client
	indexdClient, err := NewIndexDClient(logger)
	if err != nil {
		return fmt.Errorf("error initializing indexd with credentials: %v", err)
	}

	// get the name of repository
	repoName, err := GetRepoNameFromGit()
	if err != nil {
		return fmt.Errorf("Unable to fetch repository website location: %v", err)
	}
	logger.Logf("Repo Name: %s", repoName)

	// get all staged files
	stagedFiles, err := getStagedFiles()
	if err != nil {
		return fmt.Errorf("error getting staged files: %v", err)
	}
	logger.Logf("Creating DRS objects for staged files: %v", stagedFiles)

	// for each staged file, prepare if it is an LFS file
	for _, stagedFileName := range stagedFiles {
		// check if the staged file is an LFS file
		isLfsFile, lfsFileInfo, err := checkIfLfsFile(stagedFileName)
		if err != nil {
			return fmt.Errorf("error checking if file %s is LFS tracked: %v", stagedFileName, err)
		}
		if !isLfsFile {
			logger.Logf("Skipping non-LFS staged file: %s", stagedFileName)
			continue
		}

		// check hash to see if record already exists in indexd (source of truth)
		records, err := indexdClient.GetObjectsByHash(lfsFileInfo.OidType, lfsFileInfo.Oid)
		if err != nil {
			return fmt.Errorf("error getting object by hash %s: %v", lfsFileInfo.Oid, err)
		}

		// Find a record that matches the project ID
		projectId, err := config.GetProjectId()
		if err != nil {
			return fmt.Errorf("Error getting project ID: %v", err)
		}
		matchingRecord, err := FindMatchingRecord(records, projectId)
		if err != nil {
			return fmt.Errorf("Error finding matching record for project %s: %v", projectId, err)
		}

		// if project ID matches, skip because we have a copy of the file in this project
		if matchingRecord != nil {
			logger.Logf("Skipping staged file %s: OID %s already exists in indexd", lfsFileInfo.Name, lfsFileInfo.Oid)
			continue
		}

		// check if oid already committed to git
		// TODO: need to determine how to manage indexd file name
		// right now, chooses the path of the first committed copy or
		// if there's multiple copies in one commit, the first occurrence from ls-files
		drsObjPath, err := GetObjectPath(config.DRS_OBJS_PATH, lfsFileInfo.Oid)
		if err != nil {
			return fmt.Errorf("error getting object path for oid %s: %v", lfsFileInfo.Oid, err)
		}
		if _, err := os.Stat(drsObjPath); err == nil {
			logger.Logf("Skipping staged file %s with OID %s, already exists in DRS objects path %s", lfsFileInfo.Name, lfsFileInfo.Oid, drsObjPath)
			continue
		}

		// check file exists in the local cache
		if !lfsFileInfo.Downloaded {
			return fmt.Errorf("Staged file %s is not cached. Please unstage the file, then git add the file again", lfsFileInfo.Name)
		}

		// if file is in cache, hasn't been committted to git or pushed to indexd,
		// create a local DRS object for it
		// TODO: determine git to gen3 project hierarchy mapping (eg repo name to project ID)
		drsId := DrsUUID(repoName, lfsFileInfo.Oid) // FIXME: do we want to hash this with the project ID instead of the repoName?
		logger.Logf("Processing staged file: %s, OID: %s, DRS ID: %s\n", lfsFileInfo.Name, lfsFileInfo.Oid, drsId)

		// get file info needed to create indexd record
		path, err := GetObjectPath(config.LFS_OBJS_PATH, lfsFileInfo.Oid)
		if err != nil {
			return fmt.Errorf("error getting object path for oid %s: %v", lfsFileInfo.Oid, err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("Error: File %s does not exist in LFS objects path %s. Aborting.", lfsFileInfo.Name, path)
		}

		// fileInfo, err := os.Stat(path)
		// if err != nil {
		// 	return fmt.Errorf("error getting file info: %v", err)
		// }
		// modDate := fileInfo.ModTime().Format("2025-05-07T21:29:09.585275") // created date per RFC3339

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
		fileURL := fmt.Sprintf("s3://%s", filepath.Join(gen3Auth.Bucket, drsId, lfsFileInfo.Oid))

		authzStr, err := utils.ProjectToResource(gen3Auth.ProjectID)
		if err != nil {
			return err
		}

		// create IndexdRecord
		indexdObj := IndexdRecord{
			Did:      drsId,
			FileName: lfsFileInfo.Name,
			URLs:     []string{fileURL},
			Hashes:   HashInfo{SHA256: lfsFileInfo.Oid},
			Size:     lfsFileInfo.Size,
			Authz:    []string{authzStr},
			// ContentCreatedDate: modDate,
			// ContentUpdatedDate: modDate,
		}
		logger.Logf("Adding to DRS Objects: %s -> %s", lfsFileInfo.Name, indexdObj.Did)

		// write drs objects to DRS_OBJS_PATH
		err = writeDrsObj(indexdObj, lfsFileInfo.Oid, drsObjPath)
		if err != nil {
			return fmt.Errorf("error writing DRS object for oid %s: %v", lfsFileInfo.Oid, err)
		}
		logger.Logf("Created %s for file %s", drsObjPath, lfsFileInfo.Name)
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

func DrsInfoFromOid(oid string) (IndexdRecord, error) {
	// unmarshal the DRS object
	path, err := GetObjectPath(config.DRS_OBJS_PATH, oid)
	if err != nil {
		return IndexdRecord{}, fmt.Errorf("error getting object path for oid %s: %v", oid, err)
	}

	indexdObjBytes, err := os.ReadFile(path)
	if err != nil {
		return IndexdRecord{}, fmt.Errorf("error reading DRS object for oid %s: %v", oid, err)
	}

	var indexdObj IndexdRecord
	err = json.Unmarshal(indexdObjBytes, &indexdObj)
	if err != nil {
		return IndexdRecord{}, fmt.Errorf("error unmarshaling DRS object for oid %s: %v", oid, err)
	}

	return indexdObj, nil
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
func checkIfLfsFile(fileName string) (bool, *LfsFileInfo, error) {
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
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("Error running git command: %s", err)
	}

	stagedFiles := strings.Split(strings.TrimSpace(out.String()), "\n")

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
