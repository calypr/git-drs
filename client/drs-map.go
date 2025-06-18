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
	LFS_OBJS_PATH     = ".git/lfs/objects"
	DRS_MAP_FILE_NAME = "drs-map.json"
)

var (
	lfsFiles LfsLsOutput
	drsMap   = make(map[string]IndexdRecord)
	// drsMapFilePath = filepath.Join(LFS_OBJS_PATH, DRS_MAP_FILE_NAME)
	drsMapFilePath = DRS_MAP_FILE_NAME
)

func UpdateDrsMap() error {
	logger, err := NewLogger("")
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logger.Close() // Ensures cleanup
	logger.Log("updateDrsMap started")

	// [naive method] Get all LFS file and info using json
	// and replace the drsMap file with the new data
	// FIXME: use git-lfs internally instead of exec? (eg git.GetTrackedFiles)
	// https://github.com/git-lfs/git-lfs/blob/main/git/git.go/#L1515
	// or get diff directly in the commit ie git cat-files (if pointer info is stored there)?
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

		// FIXME: do we want to hash this with the project ID instead?
		drsId := DrsUUID(repoName, file.Oid)
		logger.Log("Working with file: %s, OID: %s, DRS ID: %s\n", file.Name, file.Oid, drsId)

		// get file info needed to create indexd record
		path, err := GetObjectPath(file.Oid)
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

		// If the oid exists in drsMap, check if it matches the calculated uuid
		// TODO: naive method, where only the first file with the same oid is stored
		// in the future, will need to handle multiple files with the same oid
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
				Authz:    []string{repoName},
				// ContentCreatedDate: modDate,
				// ContentUpdatedDate: modDate,
			}
			logger.Log("Adding to drsMap: %s -> %s", file.Name, drsMap[file.Name].Did)
		}
	}

	// write drsMap to json at drsMapPath
	drsMapBytes, err := json.Marshal(drsMap)
	if err != nil {
		logger.Log("error marshalling %s: %v", DRS_MAP_FILE_NAME, err)
		return fmt.Errorf("error marshalling %s: %v", DRS_MAP_FILE_NAME, err)
	}
	logger.Log("Writing drsMap to %s", drsMapFilePath)

	err = os.WriteFile(drsMapFilePath, drsMapBytes, 0644)
	if err != nil {
		return fmt.Errorf("error writing %s: %v", DRS_MAP_FILE_NAME, err)
	}
	logger.Log("Updated %s with %d entries", DRS_MAP_FILE_NAME, len(drsMap))

	// stage the drsMap file
	cmd = exec.Command("git", "add", drsMapFilePath)
	_, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("error adding %s to git: %v", DRS_MAP_FILE_NAME, err)
	}

	return nil
}

func GetRepoNameFromGit() (string, error) {
	// TODO: change to retrieve from git config directly? Or use go-git?
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	remoteURL := strings.TrimSpace(string(out))
	repoName := strings.TrimSuffix(filepath.Base(remoteURL), ".git")
	return repoName, nil
}

func DrsUUID(repoName string, hash string) string {
	// FIXME: use different UUID method? Used same method as g3t
	hashStr := fmt.Sprintf("%s:%s", repoName, hash)
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(hashStr)).String()
}

func loadDrsMap() (map[string]IndexdRecord, error) {
	// Load the DRSMap json file
	// FIXME: need to load the committed version as opposed to the working directory version
	// see https://github.com/copilot/c/c56f0baa-66d0-4d33-924f-27ca701591e5
	if _, err := os.Stat(drsMapFilePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("%s does not exist at %s", DRS_MAP_FILE_NAME, drsMapFilePath)
	}
	data, err := os.ReadFile(drsMapFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", DRS_MAP_FILE_NAME, err)
	}
	var drsMap map[string]IndexdRecord
	err = json.Unmarshal(data, &drsMap)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling %s: %v", DRS_MAP_FILE_NAME, err)
	}
	return drsMap, nil
}

func DrsInfoFromOid(oid string) (IndexdRecord, error) {
	drsMap, err := loadDrsMap()
	if err != nil {
		return IndexdRecord{}, fmt.Errorf("error loading %s: %v", DRS_MAP_FILE_NAME, err)
	}

	// Check if the oid exists in the drsMap
	if indexdObj, ok := drsMap[oid]; ok {
		return indexdObj, nil
	}
	return IndexdRecord{}, fmt.Errorf("DRS object not found for oid %s in %s", oid, DRS_MAP_FILE_NAME)
}

func GetObjectPath(oid string) (string, error) {
	// check that oid is a valid sha256 hash
	if len(oid) != 64 {
		return "", errors.New(fmt.Sprintf("Error: %s is not a valid sha256 hash", oid))
	}

	return filepath.Join(LFS_OBJS_PATH, oid[:2], oid[2:4], oid), nil
}
