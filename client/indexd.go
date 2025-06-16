package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bmeg/git-drs/drs"
	"github.com/uc-cdis/gen3-client/gen3-client/commonUtils"
	"github.com/uc-cdis/gen3-client/gen3-client/g3cmd"
	"github.com/uc-cdis/gen3-client/gen3-client/jwt"
	"github.com/uc-cdis/gen3-client/gen3-client/logs"
)

var conf jwt.Configure
var profileConfig jwt.Credential

type IndexDClient struct {
	base       *url.URL
	profile    string
	projectId  string
	bucketName string
}

// subset of the OpenAPI spec for the InputInfo object in indexd
// https://github.com/uc-cdis/indexd/blob/master/openapis/swagger.yaml
// TODO: use VersionInputInfo and indexd/<GUID> instead to allow writes to content_created_date
type IndexdRecord struct {
	// Unique identifier for the record (UUID)
	Did string `json:"did"`

	// Human-readable file name
	FileName string `json:"file_name,omitempty"`

	// List of URLs where the file can be accessed
	URLs []string `json:"urls"`

	// Hashes of the file (e.g., md5, sha256)
	Size int64 `json:"size"`

	// List of access control lists (ACLs)
	ACL []string `json:"acl,omitempty"`

	// List of authorization policies
	Authz []string `json:"authz,omitempty"`

	Hashes HashInfo `json:"hashes,omitempty"`

	// Additional metadata as key-value pairs
	Metadata map[string]string `json:"metadata,omitempty"`

	// Version of the record (optional)
	Version string `json:"version,omitempty"`

	// // Created timestamp (RFC3339 format)
	// CreatedDate string `json:"created_date,omitempty"`

	// // Updated timestamp (RFC3339 format)
	// UpdatedDate string `json:"updated_date,omitempty"`
}

// HashInfo represents file hash information as per OpenAPI spec
// Patterns are documented for reference, but not enforced at struct level
// md5:    ^[0-9a-f]{32}$
// sha:    ^[0-9a-f]{40}$
// sha256: ^[0-9a-f]{64}$
// sha512: ^[0-9a-f]{128}$
// crc:    ^[0-9a-f]{8}$
// etag:   ^[0-9a-f]{32}(-\d+)?$
type HashInfo struct {
	MD5    string `json:"md5,omitempty"`
	SHA    string `json:"sha,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	SHA512 string `json:"sha512,omitempty"`
	CRC    string `json:"crc,omitempty"`
	ETag   string `json:"etag,omitempty"`
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

	// get the gen3Profile, gen3Project, and gen3Bucket from the config
	profile := cfg.Gen3Profile
	if profile == "" {
		return nil, fmt.Errorf("No gen3 profile specified. Please provide a gen3Profile key in your .drsconfig")
	}

	projectId := cfg.Gen3Project
	if projectId == "" {
		return nil, fmt.Errorf("No gen3 project specified. Please provide a gen3Project key in your .drsconfig")
	}

	bucketName := cfg.Gen3Bucket
	if bucketName == "" {
		return nil, fmt.Errorf("No gen3 bucket specified. Please provide a gen3Bucket key in your .drsconfig")
	}

	// fmt.Printf("Base URL: %s\n", baseURL.String())
	// fmt.Printf("Profile: %s\n", profile)

	return &IndexDClient{baseURL, profile, projectId, bucketName}, err
}

// DownloadFile implements ObjectStoreClient
func (cl *IndexDClient) DownloadFile(id string, access_id string, dstPath string) (*drs.AccessURL, error) {
	// get file from indexd
	a := *cl.base
	a.Path = filepath.Join(a.Path, "ga4gh/drs/v1/objects", id, "access", access_id)
	// a.Path = filepath.Join("https://calypr.ohsu.edu/user/data/download/", id)

	fmt.Printf("using API: %s\n", a.String())

	// unmarshal response
	req, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}

	err = addGen3AuthHeader(req, cl.profile)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	fmt.Printf("added auth header")

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	fmt.Printf("got a response")

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	out := drs.AccessURL{}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal response into drs.AccessURL, response looks like: %s", body)
	}

	fmt.Printf("unmarshaled response into AccessURL struct")

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

	fmt.Printf("file download response status: %s\n", fileResponse.Status)

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

	// fmt.Printf("File written to %s\n", dstFile.Name())

	return &out, nil
}

// RegisterFile implements ObjectStoreClient.
// This function registers a file with gen3 indexd, writes the file to the bucket,
// and returns the successful DRS object.
// This is done atomically, so a failed upload will not leave a record in indexd.
func (cl *IndexDClient) RegisterFile(oid string) (*drs.DRSObject, error) {
	// setup logging
	myLogger, err := NewLogger("")
	if err != nil {
		// Handle error (e.g., print to stderr and exit)
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer myLogger.Close() // Ensures cleanup
	myLogger.Log("register file started for oid: %s", oid)

	// create indexd record
	drsObj, err := cl.registerIndexdRecord(*myLogger, oid)
	if err != nil {
		myLogger.Log("error registering indexd record: %s", err)
		return nil, fmt.Errorf("error registering indexd record: %v", err)
	}

	// TODO: upload file to bucket using gen3-client code
	// pulled from gen3-client/g3cmd/upload.go
	// https://github.com/uc-cdis/cdis-data-client/blob/df9c0820ab30e25ba8399c2cc6cccbecc2f0407a/gen3-client/g3cmd/upload.go/#L106-L150

	filePath := GetObjectPath(oid)
	g3cmd.UploadSingle(cl.profile, drsObj.Id, filePath, cl.bucketName)

	// filePath := GetObjectPath(oid)

	// // get file
	// file, _ := os.Open(filePath)
	// if fi, _ := file.Stat(); !fi.IsDir() {
	// 	fmt.Println("\t" + filePath)
	// }
	// defer file.Close()

	// myLogger.Log("file path: %s", filePath)

	// // get file info
	// uploadPath := filePath
	// includeSubDirName := true
	// hasMetadata := false

	// fileInfo, err := g3cmd.ProcessFilename(uploadPath, filePath, includeSubDirName, hasMetadata)
	// if err != nil {
	// 	myLogger.Log("error processing filename: %s", err)
	// 	logs.AddToFailedLog(filePath, filepath.Base(filePath), commonUtils.FileMetadata{}, "", 0, false, true)
	// 	log.Println("Process filename error for file: " + err.Error())
	// }

	// // connect up gen3 profile for auth
	// gen3Interface := g3cmd.NewGen3Interface()
	// myLogger.Log("parsing profile: %s", cl.profile)
	// profileConfig = conf.ParseConfig(cl.profile)

	// // if hasMetadata {
	// // 	hasShepherd, err := gen3Interface.CheckForShepherdAPI(&profileConfig)
	// // 	if err != nil {
	// // 		myLogger.Log("WARNING: Error when checking for Shepherd API: %v", err)
	// // 	} else {
	// // 		if !hasShepherd {
	// // 			myLogger.Log("ERROR: Metadata upload (`--metadata`) is not supported in the environment you are uploading to. Double check that you are uploading to the right profile.")
	// // 		}
	// // 	}
	// // }

	// a, b, err := gen3Interface.CheckPrivileges(&profileConfig)

	// myLogger.Log("Privileges: %s ---- %s ----- %s", a, b, err)

	// // get presigned URL for upload
	// bucketName := "cbds"                                                 // TODO: match bucket to program or project (as determined by fence config?)
	// fileInfo.FileMetadata.Authz = []string{"/programs/cbds/projects/qw"} // TODO: determine how to define gen3 project name
	// respURL, guid, err := g3cmd.GeneratePresignedURL(gen3Interface, fileInfo.Filename, fileInfo.FileMetadata, bucketName)
	// if err != nil {
	// 	myLogger.Log("error generating presigned URL: %s", err)
	// 	logs.AddToFailedLog(fileInfo.FilePath, fileInfo.Filename, fileInfo.FileMetadata, guid, 0, false, true)
	// 	log.Println(err.Error())
	// }
	// // update failed log with new guid
	// logs.AddToFailedLog(fileInfo.FilePath, fileInfo.Filename, fileInfo.FileMetadata, guid, 0, false, true)

	// // upload actual file
	// furObject := commonUtils.FileUploadRequestObject{FilePath: drsObj.Name, Filename: drsObj.Name, GUID: drsObj.Id, PresignedURL: respURL}
	// furObject, err = g3cmd.GenerateUploadRequest(gen3Interface, furObject, file)
	// if err != nil {
	// 	myLogger.Log("Error occurred during request generation: %s", err)
	// 	log.Printf("Error occurred during request generation: %s\n", err.Error())
	// }
	// err = uploadFile(furObject, 0)
	// if err != nil {
	// 	log.Println(err.Error())
	// } else {
	// 	logs.IncrementScore(0)
	// }

	// TODO: if upload unsuccessful, delete record from indexd

	// return
	return drsObj, nil
}

func (cl *IndexDClient) QueryID(id string) (*drs.DRSObject, error) {

	a := *cl.base
	a.Path = filepath.Join(a.Path, "ga4gh/drs/v1/objects", id)

	req, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}

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

func addGen3AuthHeader(req *http.Request, profile string) error {
	// extract accessToken from gen3 profile and insert into header of request
	profileConfig = conf.ParseConfig(profile)
	if profileConfig.AccessToken == "" {
		return fmt.Errorf("access token not found in profile config")
	}

	// Add headers to the request
	authStr := "Bearer " + profileConfig.AccessToken
	req.Header.Set("Authorization", authStr)

	return nil
}

func (cl *IndexDClient) registerIndexdRecord(myLogger Logger, oid string) (*drs.DRSObject, error) {
	// (get indexd object using drs map)
	indexdObj, err := DrsInfoFromOid(oid)
	if err != nil {
		return nil, fmt.Errorf("error getting indexd object for oid %s: %v", oid, err)
	}

	// create indexd object the long way
	var data map[string]interface{}
	var tempIndexdObj, _ = json.Marshal(indexdObj)
	json.Unmarshal(tempIndexdObj, &data)
	data["form"] = "object"

	// parse project ID to form authz string
	projectId := strings.Split(cl.projectId, "-")
	authz := fmt.Sprintf("/programs/%s/projects/%s", projectId[0], projectId[1])
	data["authz"] = []string{authz}

	jsonBytes, _ := json.Marshal(data)
	myLogger.Log("retrieved IndexdObj: %s", string(jsonBytes))

	// register DRS object via /index POST
	// (setup post request to indexd)
	a := *cl.base
	a.Path = filepath.Join(a.Path, "index", "index")

	req, err := http.NewRequest("POST", a.String(), bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}
	// set Content-Type header for JSON
	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	// add auth token
	// FIXME: token expires earlier than expected, error looks like
	// [401] - request to arborist failed: error decoding token: expired at time: 1749844905
	addGen3AuthHeader(req, cl.profile)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	myLogger.Log("POST request created for indexd: %s", a.String())

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// check and see if the response status is OK
	drsId := indexdObj.Did
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("failed to register DRS ID %s: %s", drsId, body)
	}
	myLogger.Log("POST successful: %s", response.Status)

	// query and return DRS object
	drsObj, err := cl.QueryID(indexdObj.Did)
	if err != nil {
		return nil, fmt.Errorf("error querying DRS ID %s: %v", drsId, err)
	}
	myLogger.Log("GET for DRS ID successful: %s", drsObj.Id)
	return drsObj, nil
}

// copied from
// https://github.com/uc-cdis/cdis-data-client/blob/master/gen3-client/g3cmd/utils.go#L540
func uploadFile(furObject commonUtils.FileUploadRequestObject, retryCount int) error {
	log.Println("Uploading data ...")
	furObject.Bar.Start()

	client := &http.Client{}
	resp, err := client.Do(furObject.Request)
	if err != nil {
		logs.AddToFailedLog(furObject.FilePath, furObject.Filename, furObject.FileMetadata, furObject.GUID, retryCount, false, true)
		furObject.Bar.Finish()
		return errors.New("Error occurred during upload: " + err.Error())
	}
	if resp.StatusCode != 200 {
		logs.AddToFailedLog(furObject.FilePath, furObject.Filename, furObject.FileMetadata, furObject.GUID, retryCount, false, true)
		furObject.Bar.Finish()
		return errors.New("Upload request got a non-200 response with status code " + strconv.Itoa(resp.StatusCode))
	}
	furObject.Bar.Finish()
	log.Printf("Successfully uploaded file \"%s\" to GUID %s.\n", furObject.FilePath, furObject.GUID)
	logs.DeleteFromFailedLog(furObject.FilePath, true)
	logs.WriteToSucceededLog(furObject.FilePath, furObject.GUID, false)
	return nil
}
