package client

import (
	"bytes"
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
	LFS_OBJS_PATH = ".git/lfs/objects"
	DRS_DIR       = ".drs"
	// FIXME: should this be /lfs/objects or just /objects?
	DRS_OBJS_PATH = DRS_DIR + "/lfs/objects"
)

var (
	lfsFiles LfsLsOutput
)

func UpdateDrsObjects() error {
	// init logger
	logger, err := NewLogger("")
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logger.Close()
	logger.Log("Update to DRS objects started")

	// init indexd client
	indexdClient, err := NewIndexDClient()
	if err != nil {
		return fmt.Errorf("error initializing indexd with credentials: %v", err)
	}

	// get all LFS files' info using json
	// TODO: use git-lfs internally instead of exec? (eg git.GetTrackedFiles)
	cmd := exec.Command("git", "lfs", "ls-files", "--json")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("error running git lfs ls-files: %v", err)
	}
	logger.Log("git lfs ls-files output")

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

	// get list of staged files as a set
	stagedFiles, err := getStagedFiles()
	if err != nil {
		return fmt.Errorf("error getting staged files: %v", err)
	}
	stagedFilesSet := make(map[string]struct{})
	for _, file := range stagedFiles {
		stagedFilesSet[file] = struct{}{}
	}
	logger.Log("Creating DRS objects for staged files: %v", stagedFiles)

	// for each LFS file, calculate the DRS ID using repoName and the oid
	// assumes that the DRS_OBJS_PATH only contains
	// ie that DRS objects is not manually edited, only edited via CLI
	for _, file := range lfsFiles.Files {
		// check if the file is staged
		if _, ok := stagedFilesSet[file.Name]; !ok {
			continue
		}

		// check hash to see if record already exists in indexd (source of truth)
		obj, err := indexdClient.GetObjectByHash(file.OidType, file.Oid)
		if err != nil {
			return fmt.Errorf("error getting object by hash %s: %v", file.Oid, err)
		}
		if obj != nil {
			logger.Log("Skipping staged file %s: OID %s already exists in indexd", file.Name, file.Oid)
			continue
		}

		// check if oid already committed to git
		// TODO: need to determine how to manage indexd file name
		// right now, chooses the path of the first committed copy or
		// if there's multiple copies in one commit, the first occurrence from ls-files
		drsObjPath, err := GetObjectPath(DRS_OBJS_PATH, file.Oid)
		if err != nil {
			return fmt.Errorf("error getting object path for oid %s: %v", file.Oid, err)
		}
		if _, err := os.Stat(drsObjPath); err == nil {
			logger.Log("Skipping staged file %s with OID %s, already exists in DRS objects path %s", file.Name, file.Oid, drsObjPath)
			continue
		}

		// check file exists in the local cache
		if !file.Downloaded {
			return fmt.Errorf("Staged file %s is not cached. Please unstage the file, then git add the file again", file.Name)
		}

		// if file is in cache, hasn't been committted to git or pushed to indexd,
		// create a local DRS object for it
		// TODO: determine git to gen3 project hierarchy mapping (eg repo name to project ID)
		drsId := DrsUUID(repoName, file.Oid) // FIXME: do we want to hash this with the project ID instead of the repoName?
		logger.Log("Processing staged file: %s, OID: %s, DRS ID: %s\n", file.Name, file.Oid, drsId)

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

		// get gen3 config
		cfg, err := LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		// get auth info from config
		if cfg.CurrentServer != GEN3_TYPE {
			return fmt.Errorf("error: current server is not gen3, current server is %s, please git drs init with gen3", cfg.CurrentServer)
		}
		if cfg.Servers.Gen3 == nil {
			return fmt.Errorf("error: gen3 server is not configured, please git drs init with gen3")
		}
		gen3Auth := cfg.Servers.Gen3.Auth

		if gen3Auth.Bucket == "" {
			return fmt.Errorf("error: bucket name is empty in config file")
		}
		fileURL := fmt.Sprintf("s3://%s", filepath.Join(gen3Auth.Bucket, drsId, file.Oid))

		// create authz string from profile
		if !strings.Contains(gen3Auth.ProjectID, "-") {
			return fmt.Errorf("error: invalid project ID %s in config file, ID should look like <program>-<project>", gen3Auth.ProjectID)
		}
		projectIdArr := strings.SplitN(gen3Auth.ProjectID, "-", 2)
		authzStr := "/programs/" + projectIdArr[0] + "/projects/" + projectIdArr[1]

		// create IndexdRecord
		indexdObj := IndexdRecord{
			Did:      drsId,
			FileName: file.Name,
			URLs:     []string{fileURL},
			Hashes:   HashInfo{SHA256: file.Oid},
			Size:     file.Size,
			Authz:    []string{authzStr},
			// ContentCreatedDate: modDate,
			// ContentUpdatedDate: modDate,
		}
		logger.Log("Adding to DRS Objects: %s -> %s", file.Name, indexdObj.Did)

		// write drs objects to DRS_OBJS_PATH
		err = writeDrsObj(indexdObj, file.Oid, drsObjPath)
		if err != nil {
			return fmt.Errorf("error writing DRS object for oid %s: %v", file.Oid, err)
		}
		logger.Log("Created %s for file %s", drsObjPath, file.Name)
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
