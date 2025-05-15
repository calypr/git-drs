package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/bmeg/git-drs/drs"
	"github.com/uc-cdis/gen3-client/gen3-client/jwt"
)

var conf jwt.Configure
var profileConfig jwt.Credential

type IndexDClient struct {
	base    *url.URL
	profile string
}

func NewIndexDClient(base string) (ObjectStoreClient, error) {
	baseURL, err := url.Parse(base)
	// print baseURL
	if err != nil {
		return nil, err
	}

	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	// get the gen3Profile from the config
	profile := cfg.Gen3Profile
	if profile == "" {
		return nil, fmt.Errorf("No gen3 profile specified. Please provide a gen3Profile key in your .drsconfig")
	}

	fmt.Printf("Base URL: %s\n", baseURL.String())
	fmt.Printf("Profile: %s\n", profile)

	return &IndexDClient{baseURL, profile}, err
}

// DownloadFile implements ObjectStoreClient
func (cl *IndexDClient) DownloadFile(id string, access_id string, dstPath string) (*drs.AccessURL, error) {
	// get file from indexd
	a := *cl.base
	a.Path = filepath.Join(a.Path, "drs/v1/objects", id, "access", access_id)
	// a.Path = filepath.Join("https://calypr.ohsu.edu/user/data/download/", id)

	// unmarshal response
	req, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}
	// extract accessToken from gen3 profile and insert into header of request
	profileConfig = conf.ParseConfig(cl.profile)
	if profileConfig.AccessToken == "" {
		return nil, fmt.Errorf("access token not found in profile config")
	}

	// Add headers to the request
	authStr := "Bearer " + profileConfig.AccessToken
	req.Header.Set("Authorization", authStr)

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	out := drs.AccessURL{}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return nil, err
	}

	// Extract the signed URL from the response
	signedURL := out.URL
	if signedURL == "" {
		return nil, fmt.Errorf("signed URL not found in response.")
	}

	// Download the file using the signed URL
	fileResponse, err := http.Get(signedURL)
	if err != nil {
		return nil, err
	}
	defer fileResponse.Body.Close()

	// Check if the response status is OK
	if fileResponse.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download file using signed URL: %s", fileResponse.Status)
	}

	// Create the destination directory if it doesn't exist
	err = os.MkdirAll(filepath.Dir(dstPath), os.ModePerm)
	if err != nil {
		return nil, err
	}

	// Create the destination file
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return nil, err
	}
	defer dstFile.Close()

	// Write the file content to the destination file
	_, err = io.Copy(dstFile, fileResponse.Body)
	if err != nil {
		return nil, err
	}

	fmt.Printf("File written to %s\n", dstFile.Name())

	return &out, nil
}

// RegisterFile implements ObjectStoreClient.
func (cl *IndexDClient) RegisterFile(path string, name string) (*drs.DRSObject, error) {
	panic("unimplemented")
}

func (cl *IndexDClient) QueryID(id string) (*drs.DRSObject, error) {

	a := *cl.base
	a.Path = filepath.Join(a.Path, "drs/v1/objects", id)

	req, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}
	// Add headers to the request
	req.Header.Set("Authorization", "Bearer <your-token>")
	req.Header.Set("Custom-Header", "HeaderValue")

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	out := drs.DRSObject{}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
