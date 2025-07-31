package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/data-client/data-client/g3cmd"
	"github.com/calypr/data-client/data-client/jwt"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
)

var conf jwt.Configure
var profileConfig jwt.Credential

type IndexDClient struct {
	base       *url.URL
	profile    string
	projectId  string
	bucketName string
	logger     LoggerInterface
}

////////////////////
// CLIENT METHODS //
////////////////////

// load repo-level config and return a new IndexDClient
func NewIndexDClient(logger LoggerInterface) (ObjectStoreClient, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}

	var clientLogger LoggerInterface
	if logger == nil {
		clientLogger = &NoOpLogger{}
	} else {
		clientLogger = logger
	}

	// get the gen3Profile and endpoint
	profile := cfg.Gen3Profile
	if profile == "" {
		return nil, fmt.Errorf("No gen3 profile specified. Please provide a gen3Profile key in your .drs/config")
	}

	profileConfig, err := conf.ParseConfig(profile)
	if err != nil {
		if errors.Is(err, jwt.ErrProfileNotFound) {
			return nil, fmt.Errorf("Profile not in config file. Need to run 'git drs init --profile=<profile-name> --cred=<path-to-credential/cred.json> --apiendpoint=<api_endpoint_url>' first\n")
		}
		return nil, err
	}

	baseUrl, err := url.Parse(profileConfig.APIEndpoint)
	if err != nil {
		return nil, fmt.Errorf("error parsing base URL from profile %s: %v", profile, err)
	}

	// get the gen3Project and gen3Bucket from the config
	projectId := cfg.Gen3Project
	if projectId == "" {
		return nil, fmt.Errorf("No gen3 project specified. Please provide a gen3Project key in your .drs/config")
	}

	bucketName := cfg.Gen3Bucket
	if bucketName == "" {
		return nil, fmt.Errorf("No gen3 bucket specified. Please provide a gen3Bucket key in your .drs/config")
	}
	return &IndexDClient{baseUrl, profile, projectId, bucketName, clientLogger}, err
}

// GetDownloadURL implements ObjectStoreClient
func (cl *IndexDClient) GetDownloadURL(oid string) (*drs.AccessURL, error) {
	// setup logging

	cl.logger.Logf("requested download of file oid %s", oid)

	// get the DRS object using the OID
	// FIXME: how do we not hardcode sha256 here?
	drsObj, err := cl.GetObjectByHash("sha256", oid)
	if err != nil {
		cl.logger.Logf("error getting DRS object for oid %s: %s", oid, err)
		return nil, fmt.Errorf("error getting DRS object for oid %s: %v", oid, err)
	}
	if drsObj == nil {
		cl.logger.Logf("no DRS object found for oid %s", oid)
		return nil, fmt.Errorf("no DRS object found for oid %s", oid)
	}

	// download file using the DRS object
	cl.logger.Logf("Downloading file for OID %s from DRS object: %+v", oid, drsObj)

	// FIXME: generalize access ID method
	// naively get access ID from splitting first path into :
	accessId := drsObj.AccessMethods[0].AccessID
	cl.logger.Log(fmt.Sprintf("Downloading file with oid %s, access ID: %s, file name: %s", oid, accessId, drsObj.Name))

	// get signed url
	a := *cl.base
	a.Path = filepath.Join(a.Path, "ga4gh/drs/v1/objects", drsObj.Id, "access", accessId)

	cl.logger.Logf("using endpoint: %s\n", a.String())
	req, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}

	err = addGen3AuthHeader(req, cl.profile)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	cl.logger.Log("added auth header")

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	cl.logger.Log("got a response")

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	accessUrl := drs.AccessURL{}
	err = json.Unmarshal(body, &accessUrl)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal response into drs.AccessURL, response looks like: %s", body)
	}

	cl.logger.Log("unmarshaled response into DRS AccessURL")

	return &accessUrl, nil
}

// RegisterFile implements ObjectStoreClient.
// This function registers a file with gen3 indexd, writes the file to the bucket,
// and returns the successful DRS object.
// This is done atomically, so a failed upload will not leave a record in indexd.
func (cl *IndexDClient) RegisterFile(oid string) (*drs.DRSObject, error) {
	cl.logger.Logf("register file started for oid: %s", oid)

	// create indexd record

	drsObj, err := cl.RegisterIndexdRecord(oid)
	if err != nil {
		cl.logger.Logf("error registering indexd record: %s", err)
		return nil, fmt.Errorf("error registering indexd record: %v", err)
	}

	// if upload unsuccessful (panic or error), delete record from indexd
	defer func() {
		// delete indexd record if panic
		if r := recover(); r != nil {
			// TODO: this panic isn't getting triggered
			cl.logger.Logf("panic occurred, cleaning up indexd record for oid %s", oid)
			// Handle panic
			cl.DeleteIndexdRecord(drsObj.Id)
			if err != nil {
				cl.logger.Logf("error cleaning up indexd record on failed registration for oid %s: %s", oid, err)
				cl.logger.Logf("please delete the indexd record manually if needed for DRS ID: %s", drsObj.Id)
				cl.logger.Logf("see https://uc-cdis.github.io/gen3sdk-python/_build/html/indexing.html")
				panic(r)
			}
			cl.logger.Logf("cleaned up indexd record for oid %s", oid)
			cl.logger.Logf("exiting: %v", r)
			panic(r) // re-throw if you want the CLI to still terminate
		}

		// delete indexd record if error thrown
		if err != nil {
			cl.logger.Logf("registration incomplete, cleaning up indexd record for oid %s", oid)
			err = cl.DeleteIndexdRecord(drsObj.Id)
			if err != nil {
				cl.logger.Logf("error cleaning up indexd record on failed registration for oid %s: %s", oid, err)
				cl.logger.Logf("please delete the indexd record manually if needed for DRS ID: %s", drsObj.Id)
				cl.logger.Logf("see https://uc-cdis.github.io/gen3sdk-python/_build/html/indexing.html")
				return
			}
			cl.logger.Logf("cleaned up indexd record for oid %s", oid)
		}
	}()

	// upload file to bucket using gen3-client code
	// modified from gen3-client/g3cmd/upload-single.go
	filePath, err := GetObjectPath(config.LFS_OBJS_PATH, oid)
	if err != nil {
		cl.logger.Logf("error getting object path for oid %s: %s", oid, err)
		return nil, fmt.Errorf("error getting object path for oid %s: %v", oid, err)
	}
	err = g3cmd.UploadSingle(cl.profile, drsObj.Id, filePath, cl.bucketName)
	if err != nil {
		cl.logger.Logf("error uploading file to bucket: %s", err)
		return nil, fmt.Errorf("error uploading file to bucket: %v", err)
	}

	// if all successful, remove temp DRS object
	drsPath, err := GetObjectPath(config.DRS_OBJS_PATH, oid)
	if err == nil {
		_ = os.Remove(drsPath)
	}

	// return
	return drsObj, nil
}

func (cl *IndexDClient) GetObject(id string) (*drs.DRSObject, error) {

	a := *cl.base
	a.Path = filepath.Join(a.Path, "ga4gh/drs/v1/objects", id)

	req, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}

	err = addGen3AuthHeader(req, cl.profile)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
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

func (cl *IndexDClient) ListObjects() (chan drs.DRSObjectResult, error) {

	cl.logger.Log("Getting DRS objects from indexd")

	a := *cl.base
	a.Path = filepath.Join(a.Path, "ga4gh/drs/v1/objects")

	out := make(chan drs.DRSObjectResult, 10)

	LIMIT := 50
	pageNum := 0

	go func() {
		defer close(out)
		active := true
		for active {
			// setup request
			req, err := http.NewRequest("GET", a.String(), nil)
			if err != nil {
				cl.logger.Logf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}

			q := req.URL.Query()
			q.Add("limit", fmt.Sprintf("%d", LIMIT))
			q.Add("page", fmt.Sprintf("%d", pageNum))
			req.URL.RawQuery = q.Encode()

			err = addGen3AuthHeader(req, cl.profile)
			if err != nil {
				cl.logger.Logf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}

			// execute request with error checking
			client := &http.Client{}
			response, err := client.Do(req)
			if err != nil {
				cl.logger.Logf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}

			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				cl.logger.Logf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}
			if response.StatusCode != http.StatusOK {
				cl.logger.Logf("%d: check that your credentials are valid \nfull message: %s", response.StatusCode, body)
				out <- drs.DRSObjectResult{Error: fmt.Errorf("%d: check your credentials are valid, \nfull message: %s", response.StatusCode, body)}
				return
			}

			// return page of DRS objects
			page := &drs.DRSPage{}
			err = json.Unmarshal(body, &page)
			if err != nil {
				cl.logger.Logf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}
			for _, elem := range page.DRSObjects {
				out <- drs.DRSObjectResult{Object: &elem}
			}
			if len(page.DRSObjects) == 0 {
				active = false
			}
			pageNum++
		}

		cl.logger.Logf("total pages retrieved: %d", pageNum)
	}()
	return out, nil
}

/////////////
// HELPERS //
/////////////

func addGen3AuthHeader(req *http.Request, profile string) error {
	// extract accessToken from gen3 profile and insert into header of request
	profileConfig, err := conf.ParseConfig(profile)
	if err != nil {
		if errors.Is(err, jwt.ErrProfileNotFound) {
			return fmt.Errorf("Profile not in config file. Need to run 'git drs init --profile=<profile-name> --cred=<path-to-credential/cred.json> --apiendpoint=<api_endpoint_url>' first\n")
		}
		return fmt.Errorf("error parsing gen3 config: %s", err)
	}
	if profileConfig.AccessToken == "" {
		return fmt.Errorf("access token not found in profile config")
	}

	// Add headers to the request
	authStr := "Bearer " + profileConfig.AccessToken
	req.Header.Set("Authorization", authStr)

	return nil
}

// given oid, uses saved indexd object
// and implements /index/index POST
func (cl *IndexDClient) RegisterIndexdRecord(oid string) (*drs.DRSObject, error) {
	// (get indexd object using drs map)

	indexdObj, err := DrsInfoFromOid(oid)
	if err != nil {
		return nil, fmt.Errorf("error getting indexd object for oid %s: %v", oid, err)
	}

	// create indexd object the long way
	var data map[string]any
	var tempIndexdObj, _ = json.Marshal(indexdObj)
	json.Unmarshal(tempIndexdObj, &data)
	data["form"] = "object"

	// parse project ID to form authz string
	projectId := strings.Split(cl.projectId, "-")
	authz := fmt.Sprintf("/programs/%s/projects/%s", projectId[0], projectId[1])
	data["authz"] = []string{authz}

	jsonBytes, _ := json.Marshal(data)
	cl.logger.Logf("retrieved IndexdObj: %s", string(jsonBytes))

	// register DRS object via /index POST
	// (setup post request to indexd)
	endpt := *cl.base
	endpt.Path = filepath.Join(endpt.Path, "index", "index")

	req, err := http.NewRequest("POST", endpt.String(), bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}
	// set Content-Type header for JSON
	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	// add auth token
	// FIXME: token expires earlier than expected, error looks like
	// [401] - request to arborist failed: error decoding token: expired at time: 1749844905
	err = addGen3AuthHeader(req, cl.profile)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	cl.logger.Logf("POST request created for indexd: %s", endpt.String())

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
	cl.logger.Logf("POST successful: %s", response.Status)

	// query and return DRS object
	drsObj, err := cl.GetObject(indexdObj.Did)
	if err != nil {
		return nil, fmt.Errorf("error querying DRS ID %s: %v", drsId, err)
	}
	cl.logger.Logf("GET for DRS ID successful: %s", drsObj.Id)
	return drsObj, nil
}

// implements /index{did}?rev={rev} DELETE
func (cl *IndexDClient) DeleteIndexdRecord(did string) error {
	// get the indexd record, can't use GetObject cause the DRS object doesn't contain the rev
	a := *cl.base
	a.Path = filepath.Join(a.Path, "index", did)

	getReq, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return err
	}

	err = addGen3AuthHeader(getReq, cl.profile)
	if err != nil {
		return fmt.Errorf("error adding Gen3 auth header: %v", err)
	}
	getReq.Header.Set("accept", "application/json")

	client := &http.Client{}
	getResp, err := client.Do(getReq)
	if err != nil {
		return err
	}
	defer getResp.Body.Close()

	body, err := io.ReadAll(getResp.Body)
	if err != nil {
		return err
	}

	record := OutputInfo{}
	err = json.Unmarshal(body, &record)
	if err != nil {
		return fmt.Errorf("could not query index record for did %s: %v", did, err)
	}

	// delete indexd record using did and rev
	url := fmt.Sprintf("%s/index/index/%s?rev=%s", cl.base.String(), did, record.Rev)
	delReq, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	err = addGen3AuthHeader(delReq, cl.profile)
	if err != nil {
		return fmt.Errorf("error adding Gen3 auth header to delete record: %v", err)
	}
	// set Content-Type header for JSON
	delReq.Header.Set("accept", "application/json")

	delResp, err := client.Do(delReq)
	if err != nil {
		return err
	}
	defer delResp.Body.Close()

	if delResp.StatusCode >= 400 {
		bodyBytes, readErr := io.ReadAll(delResp.Body)
		if readErr != nil {
			return fmt.Errorf("delete failed with status %s: could not read response body: %v", delResp.Status, readErr)
		}
		bodyString := string(bodyBytes)
		return fmt.Errorf("delete failed with status %s. Response body: %s", delResp.Status, bodyString)
	}
	return nil
}

// downloads a file to a specified path using a signed URL
func DownloadSignedUrl(signedURL string, dstPath string) error {
	// Download the file using the signed URL
	fileResponse, err := http.Get(signedURL)
	if err != nil {
		return err
	}
	defer fileResponse.Body.Close()

	// Check if the response status is OK
	if fileResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file using signed URL: %s", fileResponse.Status)
	}

	// Create the destination directory if it doesn't exist
	err = os.MkdirAll(filepath.Dir(dstPath), os.ModePerm)
	if err != nil {
		return err
	}

	// Create the destination file
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Write the file content to the destination file
	_, err = io.Copy(dstFile, fileResponse.Body)
	if err != nil {
		return err
	}

	return nil
}

// implements /index/index?hash={hashType}:{hash} GET
func (cl *IndexDClient) GetObjectByHash(hashType string, hash string) (*drs.DRSObject, error) {

	// search via hash https://calypr-dev.ohsu.edu/index/index?hash=sha256:52d9baed146de4895a5c9c829e7765ad349c4124ba43ae93855dbfe20a7dd3f0

	// setup get request to indexd
	url := fmt.Sprintf("%s/index/index?hash=%s:%s", cl.base.String(), hashType, hash)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		cl.logger.Logf("http.NewRequest Error: %s", err)
		return nil, err
	}
	cl.logger.Logf("GET request created for indexd: %s", url)

	err = addGen3AuthHeader(req, cl.profile)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}
	req.Header.Set("accept", "application/json")

	// run request and do checks
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error querying index for hash (%s:%s): %v, %s", hashType, hash, err, url)
	}
	defer resp.Body.Close()

	// unmarshal response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body for (%s:%s): %v", hashType, hash, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to query indexd for %s:%s. Error: %s, %s", hashType, hash, resp.Status, string(body))
	}

	records := ListRecords{}
	err = json.Unmarshal(body, &records)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling (%s:%s): %v", hashType, hash, err)
	}
	cl.logger.Logf("records: %+v", records)

	// if no records found, return nil to handle in caller
	if len(records.Records) == 0 {
		return nil, nil
	}

	// if more than one record found, write it to log
	if len(records.Records) > 1 {
		myLogger.Log("INFO: found more than 1 record for OID %s:%s, got %d records", hashType, hash, len(records.Records))
	}

	drsId := records.Records[0].Did
	myLogger.Log("Using the first matching record (%s): %s", drsId, records.Records[0].FileName)

	drsObj, err := cl.GetObject(drsId)

	return drsObj, nil
}
