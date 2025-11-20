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
	"slices"
	"strings"
	"time"

	token "github.com/bmeg/grip-graphql/middleware"
	"github.com/calypr/data-client/client/commonUtils"

	"github.com/calypr/data-client/client/g3cmd"
	"github.com/calypr/data-client/client/jwt"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/utils"
)

var conf jwt.Configure
var ProfileConfig jwt.Credential

// AuthHandler is an interface for adding authentication headers
// This allows us to inject different auth implementations for testing vs production
type AuthHandler interface {
	AddAuthHeader(req *http.Request, profile string) error
}

// RealAuthHandler uses actual Gen3 authentication
type RealAuthHandler struct{}

func (r *RealAuthHandler) AddAuthHeader(req *http.Request, profile string) error {
	return addGen3AuthHeader(req, profile)
}

type IndexDClient struct {
	Base        *url.URL
	Profile     string
	ProjectId   string
	BucketName  string
	logger      LoggerInterface
	authHandler AuthHandler // Injected for testing/flexibility
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

	logger.Logf("Loaded config: current server is %s", cfg.CurrentServer)
	if cfg.CurrentServer != config.Gen3ServerType {
		return nil, fmt.Errorf("current server is not gen3, current server: %s. Please use git drs init with the --gen3 flag", cfg.CurrentServer)
	}
	gen3Auth := cfg.Servers.Gen3.Auth

	var clientLogger LoggerInterface
	if logger == nil {
		clientLogger = &NoOpLogger{}
	} else {
		clientLogger = logger
	}

	// get the gen3Profile and endpoint
	profile := gen3Auth.Profile
	if profile == "" {
		return nil, fmt.Errorf("No gen3 profile specified. Please provide a gen3Profile key in your .drs/config")
	}

	// Attempt to parse config defined in .gen3/ directory.
	// In instances where a token and not a file is provided, This function can be ignored.
	if ProfileConfig.AccessToken == "" {
		ProfileConfig, err = conf.ParseConfig(profile)
		if err != nil && !errors.Is(err, jwt.ErrProfileNotFound) {
			return nil, err
		}
	}

	baseUrl, err := url.Parse(ProfileConfig.APIEndpoint)
	if err != nil {
		return nil, fmt.Errorf("error parsing base URL from profile %s: %v", profile, err)
	}

	// get the gen3Project and gen3Bucket from the config
	projectId := gen3Auth.ProjectID
	if projectId == "" {
		return nil, fmt.Errorf("No gen3 project specified. Run 'git drs init', use the '--help' flag for more info")
	}

	bucketName := gen3Auth.Bucket
	if bucketName == "" {
		return nil, fmt.Errorf("No gen3 bucket specified. Run 'git drs init', use the '--help' flag for more info")
	}
	return &IndexDClient{
		Base:        baseUrl,
		Profile:     profile,
		ProjectId:   projectId,
		BucketName:  bucketName,
		logger:      clientLogger,
		authHandler: &RealAuthHandler{}, // Use real auth in production
	}, err
}

// GetDownloadURL implements ObjectStoreClient
func (cl *IndexDClient) GetDownloadURL(oid string) (*drs.AccessURL, error) {

	cl.logger.Logf("Try to get download url for file OID %s", oid)

	// get the DRS object using the OID
	// FIXME: how do we not hardcode sha256 here?
	records, err := cl.GetObjectsByHash(drs.ChecksumTypeSHA256.String(), oid)
	if err != nil {
		cl.logger.Logf("error getting DRS object for OID %s: %s", oid, err)
		return nil, fmt.Errorf("error getting DRS object for OID %s: %v", oid, err)
	}
	if len(records) == 0 {
		cl.logger.Logf("no DRS object found for OID %s", oid)
		return nil, fmt.Errorf("no DRS object found for OID %s", oid)
	}

	// Find a record that matches the client's project ID
	matchingRecord, err := FindMatchingRecord(records, cl.ProjectId)
	if err != nil {
		cl.logger.Logf("error finding matching record for project %s: %s", cl.ProjectId, err)
		return nil, fmt.Errorf("error finding matching record for project %s: %v", cl.ProjectId, err)
	}
	if matchingRecord == nil {
		cl.logger.Logf("no matching record found for project %s", cl.ProjectId)
		return nil, fmt.Errorf("no matching record found for project %s", cl.ProjectId)
	}

	// Get the DRS object for the matching record
	drsObj, err := cl.GetObject(matchingRecord.Did)
	if err != nil {
		cl.logger.Logf("error getting DRS object for matching record %s: %s", matchingRecord.Did, err)
		return nil, fmt.Errorf("error getting DRS object for matching record %s: %v", matchingRecord.Did, err)
	}

	// FIXME: generalize access ID method
	// Check if access methods exist
	if len(drsObj.AccessMethods) == 0 {
		cl.logger.Logf("no access methods available for DRS object %s", drsObj.Id)
		return nil, fmt.Errorf("no access methods available for DRS object %s", drsObj.Id)
	}

	// naively get access ID from splitting first path into :
	accessId := drsObj.AccessMethods[0].AccessID

	// get signed url
	a := *cl.Base
	a.Path = filepath.Join(a.Path, "ga4gh/drs/v1/objects", drsObj.Id, "access", accessId)

	req, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}

	err = cl.authHandler.AddAuthHeader(req, cl.Profile)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error getting signed URL: %v", err)
	}
	defer response.Body.Close()

	accessUrl := drs.AccessURL{}
	if err := json.NewDecoder(response.Body).Decode(&accessUrl); err != nil {
		return nil, fmt.Errorf("unable to decode response into drs.AccessURL: %v", err)
	}

	cl.logger.Log("signed url retrieved: %s", response.Status)

	return &accessUrl, nil
}

// RegisterFile implements ObjectStoreClient.
// This function registers a file with gen3 indexd, writes the file to the bucket,
// and returns the successful DRS object.
// DRS will use any matching indexd record / file that already exists
func (cl *IndexDClient) RegisterFile(oid string, path string) (*drs.DRSObject, error) {
	cl.logger.Logf("register file started for oid: %s", oid)

	// get all existing hashes
	records, err := cl.GetObjectsByHash(string(drs.ChecksumTypeSHA256), oid)
	if err != nil {
		return nil, fmt.Errorf("Error querying indexd server for matches to hash %s: %v", oid, err)
	}

	// use any indexd record from the same project if it exists
	// Use cl.ProjectId if set (for testing), otherwise get from config
	projectId := cl.ProjectId
	if projectId == "" {
		projectId, err = config.GetProjectId()
		if err != nil {
			return nil, fmt.Errorf("Error getting project ID: %v", err)
		}
	}

	// If multiple project-matching records exist for this hash, choose a canonical one
	// based on earliest CreatedDate. This ensures we consistently use the same DID
	// that was first created in indexd when uploading the file.
	canonicalRecord, err := FindEarliestCreatedRecordForProject(records, projectId)
	if err != nil {
		return nil, fmt.Errorf("Error selecting canonical record for project %s: %v", projectId, err)
	}

	var drsObj *drs.DRSObject
	recordAlreadyExisted := false

	if canonicalRecord != nil {
		cl.logger.Logf("found existing canonical indexd record with DID %s for this project (created_date=%s)", canonicalRecord.Did, canonicalRecord.CreatedDate)
		recordAlreadyExisted = true
		drsObj, err = cl.GetObject(canonicalRecord.Did)
		if err != nil {
			return nil, fmt.Errorf("Error getting DRS object for canonical record %s: %v", canonicalRecord.Did, err)
		}
	} else {
		// otherwise, create indexd record
		cl.logger.Log("creating record: no existing indexd record for this project")

		// get indexd object using drs map
		indexdObj, err := DrsInfoFromOid(oid, path)
		if err != nil {
			return nil, fmt.Errorf("error getting indexd object for oid %s: %v", oid, err)
		}

		// register the record
		drsObj, err = cl.RegisterIndexdRecord(indexdObj)
		if err != nil {
			cl.logger.Logf("error registering indexd record: %s", err)
			return nil, fmt.Errorf("error registering indexd record: %v", err)
		}
	}

	// delete indexd record if subsequent file upload code errors out
	// but only if we just created it (don't delete pre-existing records)
	defer func() {
		if err != nil && !recordAlreadyExisted {
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

	// determine if file is downloadable
	isDownloadable := true
	cl.logger.Log("checking if file is downloadable")
	signedUrl, err := cl.GetDownloadURL(oid)
	if err != nil || signedUrl == nil {
		isDownloadable = false
	} else { // signedUrl exists
		err = utils.CanDownloadFile(signedUrl.URL)
		if err != nil {
			isDownloadable = false
		} else {
			cl.logger.Logf("file with oid %s is downloadable", oid)
		}
	}

	// if file is not downloadable, then upload it to bucket
	if !isDownloadable {
		cl.logger.Logf("file with oid %s not downloadable from bucket, proceeding to upload. Reason: %s", oid, err)

		// modified from gen3-client/g3cmd/upload-single.go
		filePath, err := GetObjectPath(config.LFS_OBJS_PATH, oid, "")
		cl.logger.Logf("file path to upload: %s", filePath)
		if err != nil {
			cl.logger.Logf("error getting object path for oid %s: %s", oid, err)
			return nil, fmt.Errorf("error getting object path for oid %s: %v", oid, err)
		}

		// figure out what is being actually called
		cl.logger.Logf("running g3cmd.UploadSingleMultipart with filepath %s, profile %s, bucket %s, DRS ID %s", filePath, cl.Profile, cl.BucketName, drsObj.Id)
		err = g3cmd.UploadSingleMultipart(cl.Profile, filePath, cl.BucketName, drsObj.Id, false)
		if err != nil {
			cl.logger.Logf("error uploading file to bucket: %s", err)
			return nil, fmt.Errorf("error uploading file to bucket: %v", err)
		}
	} else {
		cl.logger.Log("file exists in bucket, skipping upload")
	}

	// if all successful, remove temp DRS object
	drsPath, err := GetObjectPath(config.DRS_OBJS_PATH, oid, path)
	if err == nil {
		_ = os.Remove(drsPath)
	}

	// return drsObject
	return drsObj, nil
}

func (cl *IndexDClient) GetObject(id string) (*drs.DRSObject, error) {

	a := *cl.Base
	a.Path = filepath.Join(a.Path, "ga4gh/drs/v1/objects", id)

	req, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}

	err = cl.authHandler.AddAuthHeader(req, cl.Profile)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	out := drs.DRSObject{}
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (cl *IndexDClient) ListObjects() (chan drs.DRSObjectResult, error) {

	cl.logger.Log("Getting DRS objects from indexd")

	a := *cl.Base
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

			err = cl.authHandler.AddAuthHeader(req, cl.Profile)
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
	var err error
	if ProfileConfig.AccessToken == "" {
		ProfileConfig, err = conf.ParseConfig(profile)
		if err != nil {
			if errors.Is(err, jwt.ErrProfileNotFound) {
				return fmt.Errorf("Profile not in config file. Need to run 'git drs init' for gen3 first, see git drs init --help\n")
			}
			return fmt.Errorf("error parsing gen3 config: %s", err)
		}
	}
	if ProfileConfig.AccessToken == "" {
		return fmt.Errorf("access token not found in profile config")
	}
	expiration, err := token.GetExpiration(ProfileConfig.AccessToken)
	if err != nil {
		return err
	}
	// Update AccessToken if token is old
	if expiration.Before(time.Now()) {
		r := jwt.Request{}
		err = r.RequestNewAccessToken(ProfileConfig.APIEndpoint+commonUtils.FenceAccessTokenEndpoint, &ProfileConfig)
		if err != nil {
			// load config and see if the endpoint is printed
			errStr := fmt.Sprintf("error refreshing access token: %v", err)
			if strings.Contains(errStr, "no such host") {
				errStr += ". If you are accessing an internal website, make sure you are connected to the internal network."
			}

			return errors.New(errStr)
		}
	}

	// Add headers to the request
	authStr := "Bearer " + ProfileConfig.AccessToken
	req.Header.Set("Authorization", authStr)

	return nil
}

// given indexd record, constructs a new indexd record
// implements /index/index POST
func (cl *IndexDClient) RegisterIndexdRecord(indexdObj *IndexdRecord) (*drs.DRSObject, error) {
	indexdObjForm := IndexdRecordForm{
		IndexdRecord: *indexdObj,
		Form:         "object",
	}

	jsonBytes, _ := json.Marshal(indexdObjForm)
	cl.logger.Logf("retrieved IndexdObj: %s", string(jsonBytes))

	// register DRS object via /index POST
	// (setup post request to indexd)
	endpt := *cl.Base
	endpt.Path = filepath.Join(endpt.Path, "index", "index")

	req, err := http.NewRequest("POST", endpt.String(), bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}
	// set Content-Type header for JSON
	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	// add auth token
	err = cl.authHandler.AddAuthHeader(req, cl.Profile)
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
	drsId := indexdObjForm.Did
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("failed to register DRS ID %s: %s", drsId, body)
	}
	cl.logger.Logf("POST successful: %s", response.Status)

	// query and return DRS object
	drsObj, err := cl.GetObject(indexdObjForm.Did)
	if err != nil {
		return nil, fmt.Errorf("error querying DRS ID %s: %v", drsId, err)
	}
	cl.logger.Logf("GET for DRS ID successful: %s", drsObj.Id)
	return drsObj, nil
}

// implements /index{did}?rev={rev} DELETE
func (cl *IndexDClient) DeleteIndexdRecord(did string) error {
	// get the indexd record, can't use GetObject cause the DRS object doesn't contain the rev
	record, err := cl.getIndexdRecordByDID(did)
	if err != nil {
		return fmt.Errorf("could not query index record for did %s: %v", did, err)
	}

	// delete indexd record using did and rev
	url := fmt.Sprintf("%s/index/index/%s?rev=%s", cl.Base.String(), did, record.Rev)
	delReq, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	err = cl.authHandler.AddAuthHeader(delReq, cl.Profile)
	if err != nil {
		return fmt.Errorf("error adding Gen3 auth header to delete record: %v", err)
	}
	// set Content-Type header for JSON
	delReq.Header.Set("accept", "application/json")

	client := &http.Client{}
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
		body, err := io.ReadAll(fileResponse.Body)
		if err != nil {
			return fmt.Errorf("failed to download file using signed URL: %s", fileResponse.Status)
		}
		return fmt.Errorf("failed to download file using signed URL: %s. Full error: %s", fileResponse.Status, string(body))
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
func (cl *IndexDClient) GetObjectsByHash(hashType string, hash string) ([]OutputInfo, error) {
	// setup get request to indexd
	url := fmt.Sprintf("%s/index/index?hash=%s:%s", cl.Base.String(), hashType, hash)
	cl.logger.Logf("Querying indexd at %s", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		cl.logger.Logf("http.NewRequest Error: %s", err)
		return nil, err
	}
	cl.logger.Logf("Looking for files with hash %s:%s", hashType, hash)

	err = cl.authHandler.AddAuthHeader(req, cl.Profile)
	if err != nil {
		return nil, fmt.Errorf("Unable to add authentication when searching for object: %s:%s. More on the error: %v", hashType, hash, err)
	}
	req.Header.Set("accept", "application/json")

	// run request and do checks
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to check if server has files with hash %s:%s: %v", hashType, hash, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to query indexd for %s:%s. Error: %s, %s", hashType, hash, resp.Status, string(body))
	}

	// unmarshal response body
	records := ListRecords{}
	err = json.NewDecoder(resp.Body).Decode(&records)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling (%s:%s): %v", hashType, hash, err)
	}
	// log how many records were found
	cl.logger.Logf("Found %d indexd record(s) matching the hash", len(records.Records))

	// if no records found, return empty slice
	if len(records.Records) == 0 {
		return []OutputInfo{}, nil
	}

	return records.Records, nil
}

// FindMatchingRecord finds a record from the list that matches the given project ID authz
// If no matching record is found return nil
func FindMatchingRecord(records []OutputInfo, projectId string) (*OutputInfo, error) {
	if len(records) == 0 {
		return nil, nil
	}

	// Convert project ID to resource path format for comparison
	expectedAuthz, err := utils.ProjectToResource(projectId)
	if err != nil {
		return nil, fmt.Errorf("error converting project ID to resource format: %v", err)
	}

	// Get the first record with matching authz if exists
	for _, record := range records {
		if slices.Contains(record.Authz, expectedAuthz) {
			return &record, nil
		}
	}

	return nil, nil
}

// FindEarliestCreatedRecordForProject selects a canonical record for a given project
// from a list of OutputInfo objects. If multiple records belong to the same project,
// it returns the one with the earliest CreatedDate timestamp (RFC3339). If no
// matching records are found, it returns nil.
func FindEarliestCreatedRecordForProject(records []OutputInfo, projectId string) (*OutputInfo, error) {
	if len(records) == 0 {
		return nil, nil
	}

	expectedAuthz, err := utils.ProjectToResource(projectId)
	if err != nil {
		return nil, fmt.Errorf("error converting project ID to resource format: %v", err)
	}

	var canonical *OutputInfo
	var canonicalTime time.Time

	for i := range records {
		record := &records[i]
		if !slices.Contains(record.Authz, expectedAuthz) {
			continue
		}

		// If CreatedDate is empty or unparsable, treat it as "infinite" (skip) so that
		// well-formed timestamps always take precedence.
		if record.CreatedDate == "" {
			if canonical == nil {
				canonical = record
			}
			continue
		}

		// Try parsing with multiple formats since indexd may return timestamps
		// without timezone information
		var createdAt time.Time
		formats := []string{
			time.RFC3339,                 // 2006-01-02T15:04:05Z07:00
			time.RFC3339Nano,             // 2006-01-02T15:04:05.999999999Z07:00
			"2006-01-02T15:04:05.999999", // indexd format without timezone
			"2006-01-02T15:04:05",        // without microseconds or timezone
		}

		parsed := false
		for _, format := range formats {
			t, err := time.Parse(format, record.CreatedDate)
			if err == nil {
				createdAt = t
				parsed = true
				break
			}
		}

		if !parsed {
			// Skip records with invalid timestamps
			if canonical == nil {
				canonical = record
			}
			continue
		}

		if canonical == nil {
			canonical = record
			canonicalTime = createdAt
		} else if createdAt.Before(canonicalTime) {
			canonical = record
			canonicalTime = createdAt
		}
	}

	return canonical, nil
}

// implements /index/index?authz={resource_path}&start={start}&limit={limit} GET
func (cl *IndexDClient) ListObjectsByProject(projectId string) (chan ListRecordsResult, error) {
	const PAGESIZE = 50
	pageNum := 0

	cl.logger.Log("Getting DRS objects from indexd")
	resourcePath, err := utils.ProjectToResource(projectId)
	if err != nil {
		return nil, err
	}

	a := *cl.Base
	a.Path = filepath.Join(a.Path, "index/index")

	out := make(chan ListRecordsResult, PAGESIZE)
	go func() {
		defer close(out)
		active := true
		for active {
			// setup request
			req, err := http.NewRequest("GET", a.String(), nil)
			if err != nil {
				cl.logger.Logf("error: %s", err)
				out <- ListRecordsResult{Error: err}
				return
			}

			q := req.URL.Query()
			q.Add("authz", fmt.Sprintf("%s", resourcePath))
			q.Add("limit", fmt.Sprintf("%d", PAGESIZE))
			q.Add("page", fmt.Sprintf("%d", pageNum))
			req.URL.RawQuery = q.Encode()

			err = cl.authHandler.AddAuthHeader(req, cl.Profile)
			if err != nil {
				cl.logger.Logf("error: %s", err)
				out <- ListRecordsResult{Error: err}
				return
			}

			// execute request with error checking
			client := &http.Client{}
			response, err := client.Do(req)
			if err != nil {
				cl.logger.Logf("error: %s", err)
				out <- ListRecordsResult{Error: err}
				return
			}

			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				cl.logger.Logf("error: %s", err)
				out <- ListRecordsResult{Error: err}
				return
			}
			if response.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(response.Body)
				cl.logger.Logf("%d: check that your credentials are valid \nfull message: %s", response.StatusCode, body)
				out <- ListRecordsResult{Error: fmt.Errorf("%d: check your credentials are valid, \nfull message: %s", response.StatusCode, body)}
				return
			}

			// return page of DRS objects
			page := &ListRecords{}
			err = json.Unmarshal(body, &page)
			if err != nil {
				cl.logger.Logf("error: %s", err)
				out <- ListRecordsResult{Error: err}
				return
			}
			for _, elem := range page.Records {
				out <- ListRecordsResult{Record: &elem}
			}
			if len(page.Records) == 0 {
				active = false
			}
			pageNum++
		}

		cl.logger.Logf("total pages retrieved: %d", pageNum)
	}()
	return out, nil
}

// UpdateIndexdRecord updates an existing indexd record by GUID using the PUT /index/index/{guid} endpoint
// WARN: only supports putting new URLs atm
func (cl *IndexDClient) UpdateIndexdRecord(updateInfo *UpdateInputInfo, did string) (*drs.DRSObject, error) {
	// Get current revision from existing record
	record, err := cl.getIndexdRecordByDID(did)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve existing record for DID %s: %v", did, err)
	}

	// create updated update info based on existing record
	updateInfo.URLs = append(record.URLs, updateInfo.URLs...)

	// marshal update info
	jsonBytes, err := json.Marshal(updateInfo)
	if err != nil {
		return nil, fmt.Errorf("error marshaling indexd object form: %v", err)
	}

	cl.logger.Logf("Prepared updated indexd object for DID %s: %s", did, string(jsonBytes))

	// prepare URL
	updateURL := fmt.Sprintf("%s/index/index/%s?rev=%s", cl.Base.String(), did, record.Rev)

	req, err := http.NewRequest("PUT", updateURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("error creating PUT request: %v", err)
	}

	// Set required headers
	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	err = cl.authHandler.AddAuthHeader(req, cl.Profile)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	cl.logger.Logf("PUT request created for indexd update: %s", updateURL)

	// Execute the request
	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing PUT request: %v", err)
	}
	defer response.Body.Close()

	// Check response status
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("failed to update indexd record %s: status %d, body: %s", did, response.StatusCode, string(body))
	}

	cl.logger.Logf("PUT request successful: %s", response.Status)

	// Query and return the updated DRS object
	updatedDrsObj, err := cl.GetObject(did)
	if err != nil {
		return nil, fmt.Errorf("error retrieving updated DRS object: %v", err)
	}

	cl.logger.Logf("Successfully updated and retrieved DRS object: %s", did)
	return updatedDrsObj, nil
}

// Helper function to get indexd record by DID (similar to existing pattern in DeleteIndexdRecord)
func (cl *IndexDClient) getIndexdRecordByDID(did string) (*OutputInfo, error) {
	url := fmt.Sprintf("%s/index/%s", cl.Base.String(), did)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	err = cl.authHandler.AddAuthHeader(req, cl.Profile)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}
	req.Header.Set("accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get record: status %d, body: %s", resp.StatusCode, string(body))
	}

	record := &OutputInfo{}
	if err := json.NewDecoder(resp.Body).Decode(record); err != nil {
		return nil, fmt.Errorf("error decoding response body: %v", err)
	}

	return record, nil
}
