package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/google/uuid"
)

// output of git lfs ls-files
type LfsLsOutput struct {
	Files []struct {
		Name       string `json:"name"`
		Size       int64  `json:"size"`
		Checkout   bool   `json:"checkout"`
		Downloaded bool   `json:"downloaded"`
		OidType    string `json:"oid_type"`
		Oid        string `json:"oid"`
		Version    string `json:"version"`
	} `json:"files"`
}

const (
	LFS_OBJS_PATH = ".git/lfs/objects"
	DRS_DIR       = ".drs"
	DRS_OBJS_PATH = DRS_DIR + "/lfs/objects"
)

var (
	lfsFiles LfsLsOutput
	drsMap   = make(map[string]IndexdRecord)
)

func UpdateDrsObjects() error {
	logger, err := NewLogger("")
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logger.Close() // Ensures cleanup
	logger.Log("updateDrsMap started")

	// [naive method] Get all LFS files' info using json and overwrite file with new drsMap
	// FIXME: use git-lfs internally instead of exec? (eg git.GetTrackedFiles)
	cmd := exec.Command("git", "lfs", "ls-files", "--json")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("error running git lfs ls-files: %v", err)
	}
	logger.Log("git lfs ls-files output: %s", string(out))

	err = json.Unmarshal(out, &lfsFiles)
	if err != nil {
		return fmt.Errorf("error unmarshaling git lfs ls-files output: %v", err)
	}

	// get the name of repository
	repoName, err := GetRepoNameFromGit()
	if err != nil {
		return fmt.Errorf("error: %v", err)
	}
	logger.Log("Repo Name: %s", repoName)

	// for each LFS file, calculate the DRS ID using repoName and the oid
	for _, file := range lfsFiles.Files {
		// make sure file is both checked out and downloaded
		if !file.Checkout || !file.Downloaded {
			logger.Log("Skipping file: %s (checked out: %v, downloaded: %v)", file.Name, file.Checkout, file.Downloaded)
			continue
		}

		// FIXME: do we want to hash this with the project ID instead of the repoName?
		// TODO: determine git to gen3 project hierarchy mapping
		drsId := DrsUUID(repoName, file.Oid)
		logger.Log("Working with file: %s, OID: %s, DRS ID: %s\n", file.Name, file.Oid, drsId)

		// get file info needed to create indexd record
		path, err := GetObjectPath(LFS_OBJS_PATH, file.Oid)
		if err != nil {
			return fmt.Errorf("error getting object path for oid %s: %v", file.Oid, err)
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("Error: File %s does not exist in LFS objects path %s. Aborting.", file.Name, path)
		}
		// fileInfo, err := os.Stat(path)
		// if err != nil {
		// 	return fmt.Errorf("error getting file info: %v", err)
		// }
		// modDate := fileInfo.ModTime().Format("2025-05-07T21:29:09.585275") // created date per RFC3339

		// get url using bucket name, drsId, and file name
		cfg, err := LoadConfig() // should this be handled only via indexd client?
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}
		bucketName := cfg.Gen3Bucket
		if bucketName == "" {
			return fmt.Errorf("error: bucket name is empty in config file")
		}
		fileURL := fmt.Sprintf("s3://%s", filepath.Join(bucketName, drsId, file.Oid))

		// create authz string from profile
		// check if project ID is valid
		if !strings.Contains(cfg.Gen3Project, "-") {
			return fmt.Errorf("error: invalid project ID %s in config file, ID should look like <program>-<project>", cfg.Gen3Project)
		}
		projectIdArr := strings.SplitN(cfg.Gen3Project, "-", 2)
		authzStr := "/programs/" + projectIdArr[0] + "/projects/" + projectIdArr[1]

		// If the oid exists in drsMap, check if it matches the calculated uuid
		// TODO: currently only the first filename for a given oid is used
		if existing, ok := drsMap[drsId]; ok {
			if existing.Did != drsId {
				return fmt.Errorf("Error: OID %s for file %s has mismatched UUID (existing: %s, calculated: %s). Aborting.", file.Oid, file.Name, existing.Did, drsId)
			}
		} else {
			// Add new mapping from the file name to the IndexdRecord with the correct DRS ID and OID
			drsMap[file.Oid] = IndexdRecord{
				Did:      drsId,
				FileName: file.Name,
				URLs:     []string{fileURL},
				Hashes:   HashInfo{SHA256: file.Oid},
				Size:     file.Size,
				Authz:    []string{authzStr},
				// ContentCreatedDate: modDate,
				// ContentUpdatedDate: modDate,
			}
			logger.Log("Adding to drsMap: %s -> %s", file.Name, drsMap[file.Name].Did)
		}
	}

	// write drs objects to DRS_OBJS_PATH
	for oid, indexdObj := range drsMap {
		// get object bytes
		indexdObjBytes, err := json.Marshal(indexdObj)
		if err != nil {
			logger.Log("error marshalling indexd object for oid %s: %v", oid, err)
			return fmt.Errorf("error marshalling  indexd object for oid %s: %v", oid, err)
		}

		// get and create obj file path
		objFilePath, err := GetObjectPath(DRS_OBJS_PATH, oid)
		if err != nil {
			logger.Log("error getting object path for oid %s: %v", oid, err)
			return fmt.Errorf("error getting object path for oid %s: %v", oid, err)
		}
		if err := os.MkdirAll(filepath.Dir(objFilePath), 0755); err != nil {
			return fmt.Errorf("error creating directory for %s: %v", objFilePath, err)
		}

		// write indexd obj to file as json
		logger.Log("Writing drsMap to %s", objFilePath)
		err = os.WriteFile(objFilePath, indexdObjBytes, 0644)
		if err != nil {
			return fmt.Errorf("error writing %s: %v", objFilePath, err)
		}
		logger.Log("Created %s for file %s", objFilePath, indexdObjBytes)

		// stage the object file
		cmd = exec.Command("git", "add", objFilePath)
		_, err = cmd.Output()
		if err != nil {
			return fmt.Errorf("error adding %s to git: %v", objFilePath, err)
		}
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
	path, err := GetObjectPath(DRS_OBJS_PATH, oid)
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
// git helpers /
////////////////

func getStagedFiles() (git.Status, error) {
	repo, err := git.PlainOpen(".")
	if err != nil {
		return nil, errors.New(fmt.Sprintln("Could not open repo:", err))
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, errors.New(fmt.Sprintln("Could not get worktree:", err))
	}

	status, err := wt.Status()
	if err != nil {
		return nil, errors.New(fmt.Sprintln("Could not get status:", err))
	}
	return status, nil
}

func GetRepoNameFromGit() (string, error) {
	// Open the Git repository in the current directory
	repo, err := git.PlainOpen(".")
	if err != nil {
		log.Fatalf("Failed to open repo: %v", err)
	}

	// Get the config object
	config, err := repo.Config()
	if err != nil {
		log.Fatalf("Failed to get config: %v", err)
	}

	// Get the remote origin URL
	if remote, ok := config.Remotes["origin"]; ok && len(remote.URLs) > 0 {
		remoteURL := strings.TrimSpace(string(remote.URLs[0]))
		repoName := strings.TrimSuffix(filepath.Base(remoteURL), ".git")
		return repoName, nil
	} else {
		return "", errors.New("Origin remote not found")
	}
}
