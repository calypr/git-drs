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
	"time"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/utils"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2/google"
)

var endpoint string

var Cmd = &cobra.Command{
	Use:   "add-ref <drs_uri> <sha256>",
	Short: "Add a reference to an existing DRS object via URI and sha256",
	Long:  "Add a reference to an existing DRS object, eg passing a DRS URI from AnVIL. Pass the sha256 checksum if not in drs object",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		drsUri := args[0]
		shaVal := args[1]

		fmt.Printf("Adding reference to DRS object %s with sha256 %s\n", drsUri, shaVal)
		// setup logging to file for debugging
		logger, err := client.NewLogger(utils.DRS_LOG_FILE, true)
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
		logger.Log(fmt.Sprintf("Fetched DRS object: %+v", drsObj))

		// add the sha from the cache to the drsObj checksums
		sha := drs.Checksum{
			Checksum: shaVal,
			Type:     "sha256",
		}
		drsObj.Checksums = append(drsObj.Checksums, sha)
		fmt.Printf("Added sha to DRS object")

		// create an LFS pointer file at drsObj.Name
		err = CreateLfsPointer(drsObj)
		if err != nil {
			return err
		}
		logger.Log(fmt.Sprintf("Created LFS pointer file at %s", drsObj.Name))

		// add filename for lfs tracking
		err = exec.Command("git", "lfs", "track", drsObj.Name).Run()
		if err != nil {
			return fmt.Errorf("error running git lfs track: %v", err)
		}

		err = exec.Command("git", "add", ".gitattributes").Run()
		if err != nil {
			return fmt.Errorf("error running git add .gitattributes: %v", err)
		}
		logger.Log(fmt.Sprintf("Tracked %s with git lfs", drsObj.Name))

		// git add the pointer file
		err = exec.Command("git", "add", drsObj.Name).Run()
		if err != nil {
			return fmt.Errorf("error running git add: %v", err)
		}
		logger.Log(fmt.Sprintf("Added %s to git", drsObj.Name))

		return nil
	},
}

func init() {
	Cmd.Flags().StringVar(&endpoint, "endpoint", utils.ANVIL_ENDPOINT, "custom DRS endpoint to resolve DRS URIs, defaults to AnVIL")
	Cmd.Flags().Lookup("endpoint").DefValue = ""
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

	req, err := http.NewRequest("POST", utils.ANVIL_ENDPOINT, bytes.NewBuffer(bodyBytes))
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
