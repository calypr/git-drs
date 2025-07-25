package addref

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bmeg/git-drs/client"
	"github.com/bmeg/git-drs/drs"
	"github.com/bmeg/git-drs/utils"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2/google"
)

var Cmd = &cobra.Command{
	Use:   "add-ref <drs_uri>",
	Short: "Add a reference to an existing DRS object via URI",
	Long:  "Add a reference to an existing DRS object, eg passing a DRS URI from AnVIL. Requires that the sha256 of the file is already in the cache",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		drsUri := args[0]

		fmt.Printf("Adding reference to DRS object %s\n", drsUri)
		// setup logging to file for debugging
		logger, err := client.NewLogger(utils.DRS_LOG_FILE)
		if err != nil {
			return fmt.Errorf("Failed to open log file: %v", err)
		}
		defer logger.Close()
		logger.Log("~~~~~~~~~~~~~ START: add-ref ~~~~~~~~~~~~~")

		// ping AnVIL for the DRS object using a hardcoded endpoint
		drsObj, err := GetObject(drsUri)
		if err != nil {
			return err
		}
		if drsObj == nil {
			return errors.New("no DRS object found")
		}
		logger.Log("Fetched DRS object: %+v", drsObj)

		// get sha256 for the drs ID from the cache
		shaPath, err := utils.CreateCustomPath(utils.DRS_REF_DIR, drsObj.Id)
		shaFile, err := os.ReadFile(shaPath)
		if err != nil {
			return fmt.Errorf("failed to read sha file at %s: %w", shaPath, err)
		}
		shaVal := strings.TrimSpace(string(shaFile))
		if shaVal == "" {
			return fmt.Errorf("no sha256 found in file at %s", shaPath)
		}

		// add the sha from the cache to the drsObj checksums
		sha := drs.Checksum{
			Checksum: shaVal,
			Type:     "sha256",
		}
		drsObj.Checksums = append(drsObj.Checksums, sha)

		// create an LFS pointer file at drsObj.Name
		err = CreateLfsPointer(drsObj)
		if err != nil {
			return err
		}
		logger.Log("Created LFS pointer file at %s", drsObj.Name)

		// add filename for lfs tracking
		err = exec.Command("git", "lfs", "track", drsObj.Name).Run()
		if err != nil {
			return fmt.Errorf("error running git lfs track: %v", err)
		}

		err = exec.Command("git", "add", ".gitattributes").Run()
		if err != nil {
			return fmt.Errorf("error running git add .gitattributes: %v", err)
		}
		logger.Log("Tracked %s with git lfs", drsObj.Name)

		// git add the pointer file
		err = exec.Command("git", "add", drsObj.Name).Run()
		if err != nil {
			return fmt.Errorf("error running git add: %v", err)
		}
		logger.Log("Added %s to git", drsObj.Name)

		fmt.Printf("Successfully added reference to DRS object %s as file %s\n", drsObj.Id, drsObj.Name)
		return nil
	},
}

func GetObject(objectID string) (*drs.DRSObject, error) {
	// get auth token
	token, err := GetAuthToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get auth token: %w", err)
	}

	reqBody := map[string]interface{}{
		"url":    objectID,
		"fields": []string{"hashes", "size", "fileName"},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// get endpoint from config
	cfg, err := client.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if cfg.Servers.Anvil == nil {
		return nil, fmt.Errorf("Anvil server config not found in config file")
	}
	if cfg.Servers.Anvil.Endpoint == "" {
		return nil, fmt.Errorf("Anvil server endpoint is empty in config file")
	}

	req, err := http.NewRequest("POST", cfg.Servers.Anvil.Endpoint, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode > 399 {
		// Try to extract error message
		var errResp map[string]interface{}
		json.Unmarshal(respBody, &errResp)
		msg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))
		if m, ok := errResp["message"].(string); ok {
			msg = m
		}
		return &drs.DRSObject{}, errors.New(msg)
	}

	// Parse expected response
	var parsed struct {
		Hashes   map[string]string `json:"hashes"`
		Size     int64             `json:"size"`
		FileName string            `json:"fileName"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}

	// convert parsed.Hashes to []drs.Checksum before returning DRSObject
	checksums := []drs.Checksum{}
	for k, v := range parsed.Hashes {
		checksums = append(checksums, drs.Checksum{
			Type:     k,
			Checksum: v,
		})
	}

	return &drs.DRSObject{
		SelfURI:   objectID,
		Id:        objectID,
		Checksums: checksums,
		Size:      parsed.Size,
		Name:      parsed.FileName,
	}, nil
}

// GetAuthToken fetches a Google Cloud authentication token using Application Default Credentials.
// The user must run `gcloud auth application-default login` before using this.
func GetAuthToken() (string, error) {
	ctx := context.Background()
	creds, err := google.FindDefaultCredentials(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get default credentials: %w", err)
	}

	ts := creds.TokenSource
	token, err := ts.Token()
	if err != nil {
		return "", fmt.Errorf("failed to get token: %w", err)
	}

	if !token.Valid() || token.AccessToken == "" {
		return "", fmt.Errorf("no token retrieved")
	}

	return token.AccessToken, nil
}

func CreateLfsPointer(drsObj *drs.DRSObject) error {
	if len(drsObj.Checksums) == 0 {
		return fmt.Errorf("no checksums found for DRS object")
	}

	// find sha256 checksum
	var shaSum string
	for _, cs := range drsObj.Checksums {
		if cs.Type == "sha256" {
			shaSum = cs.Checksum
			break
		}
	}
	if shaSum == "" {
		return fmt.Errorf("no sha256 checksum found for DRS object")
	}

	// create pointer file content
	pointerContent := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\n")
	pointerContent += fmt.Sprintf("oid sha256:%s\n", shaSum)
	pointerContent += fmt.Sprintf("size %d\n", drsObj.Size)

	// write to file
	err := os.WriteFile(drsObj.Name, []byte(pointerContent), 0644)
	if err != nil {
		return fmt.Errorf("failed to write LFS pointer file: %w", err)
	}

	return nil
}
