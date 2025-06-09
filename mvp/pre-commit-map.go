package main

import (
	"encoding/json"
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
	drsMap   = make(map[string]string)
	// drsMapFilePath = filepath.Join(LFS_OBJS_PATH, DRS_MAP_FILE_NAME)
	drsMapFilePath = DRS_MAP_FILE_NAME
)

func main() {
	// Check if path exists and is a directory
	info, err := os.Stat(LFS_OBJS_PATH)
	if err != nil || !info.IsDir() {
		fmt.Println("No LFS objects tracked in this repository.")
		os.Exit(0)
	}

	// Get all LFS file and info using json
	// FIXME: use git-lfs internally instead of exec?
	// eg use git-lfs git.GetTrackedFiles
	// https://github.com/git-lfs/git-lfs/blob/main/git/git.go/#L1515
	cmd := exec.Command("git", "lfs", "ls-files", "--long", "--json")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("error running git lfs ls-files: %v", err)
	}

	err = json.Unmarshal(out, &lfsFiles)
	if err != nil {
		log.Fatalf("error unmarshalling git lfs ls-files output: %v", err)
	}

	// get the name of repository
	repoName, err := getRepoNameFromGit()
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	fmt.Println("Repo Name:", repoName)

	// for each LFS file, calculate the DRS ID using repoName and the oid
	for _, file := range lfsFiles.Files {
		// Example: DRS ID = sha1(repoName + ":" + oid)
		hashStr := fmt.Sprintf("%s:%s", repoName, file.Oid)
		drsId := V5UUID(hashStr).String()

		// If the oid exists in drsMap, check if it matches the calculated uuid
		if existing, ok := drsMap[file.Oid]; ok {
			if existing != drsId {
				fmt.Printf("Warning: OID %s has mismatched UUID. Updating.\n", file.Oid)
				drsMap[file.Oid] = drsId
			}
		} else {
			// Add new mapping
			drsMap[file.Oid] = drsId
		}
	}

	// write drsMap to json at drsMapPath
	drsMapBytes, err := json.Marshal(drsMap)
	if err != nil {
		log.Fatalf("error marshalling drs-map.json: %v", err)
	}

	err = os.WriteFile(drsMapFilePath, drsMapBytes, 0644)
	if err != nil {
		log.Fatalf("error writing drs-map.json: %v", err)
	}

	fmt.Println("Updated drs-map.json with", len(drsMap), "entries.")

	// stage the drsMap file
	// FIXME: should this be in th pre-commit hook as opposed to the Go code?
	cmd = exec.Command("git", "add", drsMapFilePath)
	_, err = cmd.Output()
	if err != nil {
		log.Fatalf("error adding drs-map.json to git: %v", err)
	}
}

func getRepoNameFromGit() (string, error) {
	// FIXME: change to call git config directly?
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	remoteURL := strings.TrimSpace(string(out))
	repoName := strings.TrimSuffix(filepath.Base(remoteURL), ".git")
	return repoName, nil
}

func V5UUID(data string) uuid.UUID {
	// FIXME: use different UUID method? Used same method as g3t
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(data))
}
