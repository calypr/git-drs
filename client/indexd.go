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
	base *url.URL
}

func NewIndexDClient(base string) (ObjectStoreClient, error) {
	baseURL, err := url.Parse(base)
	// print baseURL
	if err != nil {
		return nil, err
	}
	fmt.Printf("Base URL: %s\n", baseURL.String())

	return &IndexDClient{baseURL}, err
}

// DownloadFile implements ObjectStoreClient.
func (cl *IndexDClient) DownloadFile(id string, access_id string, profile string, dstPath string) (*drs.AccessURL, error) {

	// get file from indexd
	a := *cl.base
	a.Path = filepath.Join(a.Path, "drs/v1/objects", id, "access", access_id)
	// a.Path = filepath.Join("https://calypr.ohsu.edu/user/data/download/", id)

	fmt.Print("Getting URL: ", a.String(), "\n")

	// unmarshal response
	req, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}
	// extract accessToken from config and insert into header of request
	profileConfig = conf.ParseConfig(profile)
	if profileConfig.AccessToken == "" {
		return nil, fmt.Errorf("access token not found in profile config")
	}

	// Add headers to the request
	authStr := fmt.Sprintf("Bearer %s", profileConfig.AccessToken)
	fmt.Printf("Authorization header: %s\n", authStr)
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

	// print body
	fmt.Printf("Response body: %s\n", string(body))

	out := drs.AccessURL{}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return nil, err
	}

	// Extract the signed URL from the response
	signedURL := out.URL // Assuming `out.url` contains the signed URL
	if signedURL == "" {
		return nil, fmt.Errorf("signed URL not found in response")
	}

	fmt.Print("Signed URL: ", signedURL, "\n")

	// Download the file using the signed URL
	fileResponse, err := http.Get(signedURL)
	if err != nil {
		return nil, err
	}
	defer fileResponse.Body.Close()

	fmt.Printf("File response status: %s\n", fileResponse.Status)

	// Create the destination file
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return nil, err
	}
	defer dstFile.Close()

	// print file response as string
	fmt.Printf("File response contents: %s\n", fileResponse.Body)

	// Write the file content to the destination file
	_, err = io.Copy(dstFile, fileResponse.Body)
	if err != nil {
		return nil, err
	}

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
	//log.Printf("Getting URL %s\n", a.String())
	//fmt.Printf("%s\n", string(body))

	out := drs.DRSObject{}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
