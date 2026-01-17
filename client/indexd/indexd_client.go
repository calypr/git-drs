package indexd_client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/calypr/data-client/client/common"
	"github.com/calypr/data-client/client/conf"
	"github.com/calypr/data-client/client/logs"
	"github.com/calypr/data-client/client/upload"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/projectdir"
	"github.com/calypr/git-drs/s3_utils"
	"github.com/calypr/git-drs/utils"
	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/go-retryablehttp"

	dataClient "github.com/calypr/data-client/client/client"
)

type IndexDClient struct {
	Base        *url.URL
	ProjectId   string
	BucketName  string
	Logger      *log.Logger
	AuthHandler s3_utils.AuthHandler // Injected for testing/flexibility

	HttpClient *retryablehttp.Client
	SConfig    sonic.API

	ForcePush bool
}

////////////////////
// CLIENT METHODS //
////////////////////

// load repo-level config and return a new IndexDClient
func NewIndexDClient(profileConfig conf.Credential, remote Gen3Remote, logger *log.Logger) (client.DRSClient, error) {

	baseUrl, err := url.Parse(profileConfig.APIEndpoint)
	// get the gen3Project and gen3Bucket from the config
	projectId := remote.GetProjectId()
	if projectId == "" {
		return nil, fmt.Errorf("no gen3 project specified. Run 'git drs init', use the '--help' flag for more info")
	}

	bucketName := remote.GetBucketName()
	if bucketName == "" {
		logger.Println("WARNING: no gen3 bucket specified. To add a bucket, run 'git remote add gen3', use the '--help' flag for more info")
	}

	transport := &http.Transport{
		MaxIdleConns:        100, // Default pool size (across all hosts)
		MaxIdleConnsPerHost: 100, // Important: Pool size per *single host* (your Indexd server)
		IdleConnTimeout:     90 * time.Second,
	}
	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient = httpClient

	retryClient.Logger = logger
	// TODO - make these configurable?
	retryClient.RetryMax = 5
	retryClient.RetryWaitMin = 5 * time.Second
	retryClient.RetryWaitMax = 15 * time.Second

	forcePush, err := getLfsCustomTransferBool("lfs.customtransfer.drs.force-push", false)
	if err != nil {
		return nil, err
	}

	return &IndexDClient{
		Base:        baseUrl,
		ProjectId:   projectId,
		BucketName:  bucketName,
		Logger:      logger,
		AuthHandler: &RealAuthHandler{profileConfig}, // Use real auth in production
		HttpClient:  retryClient,
		SConfig:     sonic.ConfigFastest,
		ForcePush:   forcePush,
	}, nil
}

func (cl *IndexDClient) GetProjectId() string {
	return cl.ProjectId
}

func getLfsCustomTransferBool(key string, defaultValue bool) (bool, error) {
	defaultText := strconv.FormatBool(defaultValue)
	cmd := exec.Command("git", "config", "--get", "--default", defaultText, key)
	output, err := cmd.Output()
	if err != nil {
		return defaultValue, fmt.Errorf("error reading git config %s: %v", key, err)
	}

	value := strings.TrimSpace(string(output))

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue, fmt.Errorf("invalid boolean value for %s: >%q<", key, value)
	}
	return parsed, nil
}

// GetProfile extracts the profile from the auth handler if available
// This is only needed for external APIs like g3cmd that require it
func (cl *IndexDClient) GetProfile() (string, error) {
	if rh, ok := cl.AuthHandler.(*RealAuthHandler); ok {
		return rh.Cred.Profile, nil
	}
	return "", fmt.Errorf("AuthHandler is not RealAuthHandler, cannot extract profile")
}

func (cl *IndexDClient) DeleteRecordsByProject(projectId string) error {
	recs, err := cl.ListObjectsByProject(projectId)
	if err != nil {
		return err
	}
	for rec := range recs {
		for sumType, sum := range hash.ConvertHashInfoToMap(rec.Object.Checksums) {
			if sumType == string(hash.ChecksumTypeSHA256) {
				err := cl.DeleteRecord(sum)
				if err != nil {
					cl.Logger.Println("DeleteRecordsByProject Error: ", err)
					continue
				}
			}
		}
	}
	return nil
}

func (cl *IndexDClient) DeleteRecord(oid string) error {
	// get records by hash
	record, err := cl.GetObjectByHash(&hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid})
	if err != nil {
		return fmt.Errorf("error getting records for OID %s: %v", oid, err)
	}
	if len(record) == 0 {
		return fmt.Errorf("no records found for OID %s", oid)
	}

	// Find a record that matches the project ID
	matchingRecord, err := drsmap.FindMatchingRecord(record, cl.GetProjectId())
	if err != nil {
		return fmt.Errorf("error finding matching record for project %s: %v", cl.GetProjectId(), err)
	}
	if matchingRecord == nil {
		return fmt.Errorf("no matching record found for project %s", cl.GetProjectId())
	}

	// call helper to do the delete for a gen3 GUID
	return cl.deleteIndexdRecord(matchingRecord.Id)
}

func (cl *IndexDClient) deleteIndexdRecord(did string) error {
	// get the indexd record, can't use GetObject cause the DRS object doesn't contain the rev
	record, err := cl.getIndexdRecordByDID(did)
	if err != nil {
		return fmt.Errorf("could not query index record for did %s: %v", did, err)
	}

	// delete indexd record using did and rev
	url := fmt.Sprintf("%s/index/index/%s?rev=%s", cl.Base.String(), did, record.Rev)
	delReq, err := retryablehttp.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	err = cl.AuthHandler.AddAuthHeader(delReq.Request)
	if err != nil {
		return fmt.Errorf("error adding Gen3 auth header to delete record: %v", err)
	}
	// set Content-Type header for JSON
	delReq.Header.Set("accept", "application/json")

	delResp, err := cl.HttpClient.Do(delReq)
	if err != nil {
		return err
	}
	defer delResp.Body.Close()

	// response error handling
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

func (cl *IndexDClient) RegisterRecord(record *drs.DRSObject) (*drs.DRSObject, error) {
	// prolly could do cleanup but use register record
	indexdRecord, err := indexdRecordFromDrsObject(record)
	if err != nil {
		return nil, fmt.Errorf("error converting DRS object to indexd record: %v", err)
	}

	return cl.RegisterIndexdRecord(indexdRecord)
}

// GetDownloadURL implements DRSClient
func (cl *IndexDClient) GetDownloadURL(oid string) (*drs.AccessURL, error) {

	cl.Logger.Printf("Try to get download url for file OID %s", oid)

	// get the DRS object using the OID
	records, err := cl.GetObjectByHash(&hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid})
	if err != nil {
		cl.Logger.Printf("error getting DRS object for OID %s: %s", oid, err)
		return nil, fmt.Errorf("error getting DRS object for OID %s: %v", oid, err)
	}
	return cl.getDownloadURLFromRecords(oid, records)
}

func (cl *IndexDClient) getDownloadURLFromRecords(oid string, records []drs.DRSObject) (*drs.AccessURL, error) {
	if len(records) == 0 {
		cl.Logger.Printf("no DRS object found for OID %s", oid)
		return nil, fmt.Errorf("no DRS object found for OID %s", oid)
	}

	// Find a record that matches the client's project ID
	matchingRecord, err := drsmap.FindMatchingRecord(records, cl.ProjectId)
	if err != nil {
		cl.Logger.Printf("error finding matching record for project %s: %s", cl.ProjectId, err)
		return nil, fmt.Errorf("error finding matching record for project %s: %v", cl.ProjectId, err)
	}
	if matchingRecord == nil {
		cl.Logger.Printf("no matching record found for project %s", cl.ProjectId)
		return nil, fmt.Errorf("no matching record found for project %s", cl.ProjectId)
	}

	cl.Logger.Printf("Matching record: %#v for oid %s", matchingRecord, oid)
	// Get the DRS object for the matching record
	drsObj, err := cl.GetObject(matchingRecord.Id)
	if err != nil {
		cl.Logger.Printf("error getting DRS object for matching record %s: %s", matchingRecord.Id, err)
		return nil, fmt.Errorf("error getting DRS object for matching record %s: %v", matchingRecord.Id, err)
	}

	// Check if access methods exist
	if len(drsObj.AccessMethods) == 0 {
		cl.Logger.Printf("no access methods available for DRS object %s", drsObj.Id)
		return nil, fmt.Errorf("no access methods available for DRS object %s", drsObj.Id)
	}

	// naively get access ID from splitting first path into :
	accessId := drsObj.AccessMethods[0].AccessID

	// get signed url
	a := *cl.Base
	a.Path = filepath.Join(a.Path, "ga4gh/drs/v1/objects", drsObj.Id, "access", accessId)

	req, err := retryablehttp.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}

	err = cl.AuthHandler.AddAuthHeader(req.Request)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	response, err := cl.HttpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error getting signed URL: %v", err)
	}
	defer response.Body.Close()

	accessUrl := drs.AccessURL{}
	if err := cl.SConfig.NewDecoder(response.Body).Decode(&accessUrl); err != nil {
		return nil, fmt.Errorf("unable to decode response into drs.AccessURL: %v", err)
	}

	// check if empty
	if accessUrl.URL == "" {
		return nil, fmt.Errorf("signed url is empty %#v %s", accessUrl, response.Status)
	}

	cl.Logger.Printf("signed url retrieved: %s", response.Status)

	return &accessUrl, nil
}

// RegisterFile implements DRSClient.
// It registers (or reuses) an indexd record for the oid, uploads the object if it
// is not already available in the bucket, and returns the resulting DRS object.
// When registration fails without force push, it retries once with force push
// enabled to reuse existing records and avoid duplicate uploads.
func (cl *IndexDClient) RegisterFile(oid string) (*drs.DRSObject, error) {
	cl.Logger.Printf("register file started for oid: %s", oid)

	originalForcePush := cl.ForcePush
	attempts := 1
	if !originalForcePush {
		attempts = 2
		defer func() {
			cl.ForcePush = originalForcePush
		}()
	}

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt == 1 {
			cl.ForcePush = true
		}
		cl.Logger.Printf("register file attempt %d for oid: %s (force push: %t)", attempt+1, oid, cl.ForcePush)

		var records []drs.DRSObject
		var err error
		var drsObject *drs.DRSObject
		createdRecord := false

		if cl.ForcePush {
			records, err = cl.GetObjectByHash(&hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid})
			if err != nil {
				return nil, fmt.Errorf("error querying indexd server for matches to hash %s: %v", oid, err)
			}
			if len(records) > 0 {
				drsObject, err = drsmap.FindMatchingRecord(records, cl.ProjectId)
				if err != nil {
					return nil, fmt.Errorf("error finding matching record for project %s: %v", cl.ProjectId, err)
				}
			}
		}

		if drsObject == nil {
			drsObject, err = drsmap.DrsInfoFromOid(oid)
			if err != nil {
				return nil, fmt.Errorf("error getting indexd object for oid %s: %v", oid, err)
			}

			indexdObj, err := indexdRecordFromDrsObject(drsObject)
			if err != nil {
				return nil, fmt.Errorf("error converting DRS object to indexd record: %v", err)
			}

			drsObject, err = cl.RegisterIndexdRecord(indexdObj)
			if err != nil {
				if !cl.ForcePush {
					cl.Logger.Printf("error saving indexd record without force push: %s; retrying with force push enabled", err)
					continue
				}
				cl.Logger.Printf("error saving indexd record: %s", err)
				return nil, fmt.Errorf("error saving indexd record: %v", err)
			}
			createdRecord = true
		}

		cleanupOnError := func(cause error) (*drs.DRSObject, error) {
			if createdRecord && drsObject != nil {
				cl.Logger.Printf("registration incomplete, cleaning up indexd record for oid %s", oid)
				cleanupErr := cl.DeleteIndexdRecord(drsObject.Id)
				if cleanupErr != nil {
					cl.Logger.Printf("error cleaning up indexd record on failed registration for oid %s: %s", oid, cleanupErr)
					cl.Logger.Printf("please delete the indexd record manually if needed for DRS ID: %s", drsObject.Id)
					cl.Logger.Printf("see https://uc-cdis.github.io/gen3sdk-python/_build/html/indexing.html")
				} else {
					cl.Logger.Printf("cleaned up indexd record for oid %s", oid)
				}
			}
			return nil, cause
		}

		isDownloadable := false
		if cl.ForcePush {
			// TODO why is this a list of records?
			recordsForDownload := ensureDrsObjectInRecords(records, drsObject)
			// TODO don't we already know this from earlier?
			isDownloadable, err = cl.isFileDownloadable(oid, recordsForDownload)
			if err != nil {
				return cleanupOnError(err)
			}
		}

		if !isDownloadable {
			cl.Logger.Printf("Proceeding to upload %s", oid)

			filePath, err := drsmap.GetObjectPath(projectdir.LFS_OBJS_PATH, oid)
			if err != nil {
				cl.Logger.Printf("error getting object path for oid %s: %s", oid, err)
				return cleanupOnError(fmt.Errorf("error getting object path for oid %s: %v", oid, err))
			}

			profile, err := cl.GetProfile()
			if err != nil {
				return cleanupOnError(fmt.Errorf("error getting profile for upload: %v", err))
			}

			// TODO - should we deprecate this gen3-client style logger in favor of drslog.Logger?
			// TODO - or can we "wrap it" so both work together?
			logger, closer := logs.New(profile, logs.WithBaseLogger(cl.Logger))
			defer closer()

			g3, err := dataClient.NewGen3Interface(profile, logger)
			if err != nil {
				return cleanupOnError(fmt.Errorf("error creating Gen3 interface: %v", err))
			}

			file, err := os.Open(filePath)
			if err != nil {
				return cleanupOnError(fmt.Errorf("error opening file %s: %v", filePath, err))
			}
			defer file.Close()

			stat, err := file.Stat()
			if err != nil {
				return cleanupOnError(fmt.Errorf("error stating file %s: %v", file.Name(), err))
			}

			// TODO - Can we reuse Auth to ensure we are not repeatedly refreshing tokens?
			if stat.Size() < 5*common.GB {
				err := upload.UploadSingle(context.Background(), g3.GetCredential().Profile, drsObject.Id, filePath, cl.BucketName, false)
				if err != nil {
					cl.Logger.Printf("error uploading single file to bucket: %s", err)
					return cleanupOnError(fmt.Errorf("error uploading single file to bucket: %s", err))
				}
			} else {
				err = upload.MultipartUpload(
					context.TODO(),
					g3,
					common.FileUploadRequestObject{
						FilePath:     filePath,
						Filename:     filepath.Base(filePath),
						GUID:         drsObject.Id,
						FileMetadata: common.FileMetadata{},
						Bucket:       cl.BucketName,
					},
					file, false,
				)
				if err != nil {
					cl.Logger.Printf("error uploading file to bucket: %s", err)
					return cleanupOnError(fmt.Errorf("error uploading file to bucket: %v", err))
				}
			}
		} else {
			cl.Logger.Print("file exists in bucket, skipping upload")
		}

		return drsObject, nil
	}

	return nil, fmt.Errorf("indexd registration failed after retry for oid %s", oid)
}

func (cl *IndexDClient) isFileDownloadable(oid string, records []drs.DRSObject) (bool, error) {
	if !cl.ForcePush {
		cl.Logger.Printf("force push disabled; proceeding to upload oid %s", oid)
		return false, nil
	}

	cl.Logger.Printf("checking if %s file is downloadable", oid)
	signedUrl, err := cl.getDownloadURLFromRecords(oid, records)
	if err != nil {
		cl.Logger.Printf("error getting signed URL for file with oid %s: %s", oid, err)
		return false, fmt.Errorf("error getting signed URL for file with oid %s: %s", oid, err)
	}
	if signedUrl == nil {
		return false, nil
	}

	err = utils.CanDownloadFile(signedUrl.URL)
	if err != nil {
		cl.Logger.Printf("file with oid %s does not exist in bucket: %s", oid, err)
		return false, nil
	}
	cl.Logger.Printf("file with oid %s exists in bucket", oid)
	return true, nil
}

func ensureDrsObjectInRecords(records []drs.DRSObject, drsObject *drs.DRSObject) []drs.DRSObject {
	if drsObject == nil {
		return records
	}

	for _, record := range records {
		if record.Id == drsObject.Id {
			return records
		}
	}

	return append(records, *drsObject)
}

func (cl *IndexDClient) GetObject(id string) (*drs.DRSObject, error) {

	a := *cl.Base
	a.Path = filepath.Join(a.Path, "ga4gh/drs/v1/objects", id)

	req, err := retryablehttp.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}

	err = cl.AuthHandler.AddAuthHeader(req.Request)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	response, err := cl.HttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.Status == "404" {
		return nil, fmt.Errorf("%s not found", id)
	}

	in := drs.OutputObject{}
	if err := cl.SConfig.NewDecoder(response.Body).Decode(&in); err != nil {
		return nil, err
	}
	return drs.ConvertOutputObjectToDRSObject(&in), nil

}

func (cl *IndexDClient) ListObjectsByProject(projectId string) (chan drs.DRSObjectResult, error) {
	const PAGESIZE = 50
	pageNum := 0

	cl.Logger.Print("Getting DRS objects from indexd")
	resourcePath, err := utils.ProjectToResource(projectId)
	if err != nil {
		return nil, err
	}

	a := *cl.Base
	a.Path = filepath.Join(a.Path, "index/index")

	out := make(chan drs.DRSObjectResult, PAGESIZE)

	go func() {
		defer close(out)

		// This will hold all errors encountered during the loop
		var resultErrors *multierror.Error
		active := true

		for active {
			req, err := retryablehttp.NewRequest("GET", a.String(), nil)
			if err != nil {
				resultErrors = multierror.Append(resultErrors, fmt.Errorf("request creation: %w", err))
				break
			}

			q := req.URL.Query()
			q.Add("authz", resourcePath)
			q.Add("limit", fmt.Sprintf("%d", PAGESIZE))
			q.Add("page", fmt.Sprintf("%d", pageNum))
			req.URL.RawQuery = q.Encode()

			if err := cl.AuthHandler.AddAuthHeader(req.Request); err != nil {
				resultErrors = multierror.Append(resultErrors, fmt.Errorf("auth: %w", err))
				break
			}

			response, err := cl.HttpClient.Do(req)
			if err != nil {
				resultErrors = multierror.Append(resultErrors, fmt.Errorf("http call: %w", err))
				break
			}

			// Read body and close immediately
			body, err := io.ReadAll(response.Body)
			response.Body.Close()

			if err != nil {
				resultErrors = multierror.Append(resultErrors, fmt.Errorf("read body: %w", err))
				break
			}

			if response.StatusCode != http.StatusOK {
				resultErrors = multierror.Append(resultErrors, fmt.Errorf("api error %d: %s", response.StatusCode, string(body)))
				break
			}

			page := &ListRecords{}
			if err := cl.SConfig.Unmarshal(body, &page); err != nil {
				resultErrors = multierror.Append(resultErrors, fmt.Errorf("unmarshal: %w", err))
				break
			}

			if len(page.Records) == 0 {
				active = false
			}

			for _, elem := range page.Records {
				drsObj, err := elem.ToIndexdRecord().ToDrsObject()
				if err != nil {
					// Append and keep going, or break if this is fatal
					resultErrors = multierror.Append(resultErrors, err)
					continue
				}
				out <- drs.DRSObjectResult{Object: drsObj}
			}
			pageNum++
		}

		// If we accumulated any errors, send the final concatenated result
		if resultErrors != nil {
			out <- drs.DRSObjectResult{Error: resultErrors.ErrorOrNil()}
		}
	}()

	return out, nil
}

// given indexd record, constructs a new indexd record
// implements /index/index POST
func (cl *IndexDClient) RegisterIndexdRecord(indexdObj *IndexdRecord) (*drs.DRSObject, error) {
	indexdObjForm := IndexdRecordForm{
		IndexdRecord: *indexdObj,
		Form:         "object",
	}

	jsonBytes, err := sonic.Marshal(indexdObjForm)
	if err != nil {
		return nil, err
	}

	cl.Logger.Printf("writing IndexdObj: %s", string(jsonBytes))

	// register DRS object via /index POST
	// (setup post request to indexd)
	endpt := *cl.Base
	endpt.Path = filepath.Join(endpt.Path, "index", "index")

	req, err := retryablehttp.NewRequest("POST", endpt.String(), bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}
	// set Content-Type header for JSON
	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	// add auth token
	err = cl.AuthHandler.AddAuthHeader(req.Request)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	cl.Logger.Printf("POST request created for indexd: %s", endpt.String())
	response, err := cl.HttpClient.Do(req)
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
	cl.Logger.Printf("POST successful: %s", response.Status)

	// removed re-query return DRS object (was missing access method authorization anyway)
	drsObj, err := indexdRecordToDrsObject(indexdObj)
	if err != nil {
		return nil, fmt.Errorf("error converting indexd record to DRS object: %w %v", err, indexdObj)
	}
	return drsObj, nil
}

// implements /index{did}?rev={rev} DELETE
func (cl *IndexDClient) DeleteIndexdRecord(did string) error {
	// get the indexd record, can't use GetObject cause the DRS object doesn't contain the rev
	record, err := cl.GetIndexdRecordByDID(did)
	if err != nil {
		return fmt.Errorf("could not query index record for did %s: %v", did, err)
	}

	// delete indexd record using did and rev
	url := fmt.Sprintf("%s/index/index/%s?rev=%s", cl.Base.String(), did, record.Rev)
	delReq, err := retryablehttp.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	err = cl.AuthHandler.AddAuthHeader(delReq.Request)
	if err != nil {
		return fmt.Errorf("error adding Gen3 auth header to delete record: %v", err)
	}
	// set Content-Type header for JSON
	delReq.Header.Set("accept", "application/json")
	delResp, err := cl.HttpClient.Do(delReq)
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

// implements /index/index?hash={hashType}:{hash} GET
func (cl *IndexDClient) GetObjectByHash(sum *hash.Checksum) ([]drs.DRSObject, error) {
	// setup get request to indexd
	url := fmt.Sprintf("%s/index/index?hash=%s:%s", cl.Base.String(), sum.Type, sum.Checksum)
	cl.Logger.Printf("Querying indexd at %s", url)
	req, err := retryablehttp.NewRequest("GET", url, nil)
	if err != nil {
		cl.Logger.Printf("http.NewRequest Error: %s", err)
		return nil, err
	}
	cl.Logger.Printf("Looking for files with hash %s:%s", sum.Type, sum.Checksum)

	err = cl.AuthHandler.AddAuthHeader(req.Request)
	if err != nil {
		return nil, fmt.Errorf("unable to add authentication when searching for object: %s:%s. More on the error: %v", sum.Type, sum.Checksum, err)
	}
	req.Header.Set("accept", "application/json")

	// run request and do checks
	resp, err := cl.HttpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to check if server has files with hash %s:%s: %v", sum.Type, sum.Checksum, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to query indexd for %s:%s. Error: %s, %s", sum.Type, sum.Checksum, resp.Status, string(body))
	}

	// unmarshal response body
	records := ListRecords{}
	err = cl.SConfig.NewDecoder(resp.Body).Decode(&records)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling (%s:%s): %v", sum.Type, sum.Checksum, err)
	}
	// log how many records were found
	cl.Logger.Printf("Found %d indexd record(s) matching the hash %v", len(records.Records), records)

	out := make([]drs.DRSObject, len(records.Records))

	// if no records found, return empty slice
	if len(records.Records) == 0 {
		return out, nil
	}

	resourcePath, _ := utils.ProjectToResource(cl.GetProjectId())

	for _, record := range records.Records {
		// skip records that do not authorize this client/project
		found := false
		for _, a := range record.Authz {
			if a == resourcePath {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		drsObj, err := indexdRecordToDrsObject(record.ToIndexdRecord())
		if err != nil {
			return nil, fmt.Errorf("error converting indexd record to DRS object: %w", err)
		}
		out = append(out, *drsObj)
	}

	return out, nil
}

// GetProjectSample retrieves a sample of DRS objects for a given project (limit: 1 by default)
// Returns up to 'limit' records for preview purposes before destructive operations
func (cl *IndexDClient) GetProjectSample(projectId string, limit int) ([]drs.DRSObject, error) {
	if limit <= 0 {
		limit = 1
	}

	cl.Logger.Printf("Getting sample DRS objects from indexd for project %s (limit: %d)", projectId, limit)

	// Reuse ListObjectsByProject and collect first 'limit' results
	objChan, err := cl.ListObjectsByProject(projectId)
	if err != nil {
		return nil, err
	}

	result := make([]drs.DRSObject, 0, limit)
	for objResult := range objChan {
		if objResult.Error != nil {
			return nil, objResult.Error
		}
		result = append(result, *objResult.Object)

		// Stop after collecting enough samples
		if len(result) >= limit {
			// Drain remaining results to avoid goroutine leak
			go func() {
				for range objChan {
				}
			}()
			break
		}
	}

	cl.Logger.Printf("Retrieved %d sample record(s)", len(result))
	return result, nil
}

// implements /index/index?authz={resource_path}&start={start}&limit={limit} GET
func (cl *IndexDClient) ListObjects() (chan drs.DRSObjectResult, error) {

	cl.Logger.Print("Getting DRS objects from indexd")

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
			req, err := retryablehttp.NewRequest("GET", a.String(), nil)
			if err != nil {
				cl.Logger.Printf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}

			q := req.URL.Query()
			q.Add("limit", fmt.Sprintf("%d", LIMIT))
			q.Add("page", fmt.Sprintf("%d", pageNum))
			req.URL.RawQuery = q.Encode()

			err = cl.AuthHandler.AddAuthHeader(req.Request)
			if err != nil {
				cl.Logger.Printf("error contacting %s : %s", req.URL, err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}

			// execute request with error checking
			response, err := cl.HttpClient.Do(req)

			if err != nil {
				cl.Logger.Printf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}

			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				cl.Logger.Printf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}
			if response.StatusCode != http.StatusOK {
				cl.Logger.Printf("%d: check that your credentials are valid \nfull message: %s", response.StatusCode, body)
				out <- drs.DRSObjectResult{Error: fmt.Errorf("%d: check your credentials are valid, \nfull message: %s", response.StatusCode, body)}
				return
			}

			// return page of DRS objects
			page := &drs.DRSPage{}
			err = cl.SConfig.Unmarshal(body, &page)
			if err != nil {
				cl.Logger.Printf("error: %s (%s)", err, body)
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

		cl.Logger.Printf("total pages retrieved: %d", pageNum)
	}()
	return out, nil
}

// UpdateRecord updates an existing indexd record by GUID using the PUT /index/index/{guid} endpoint
// Supports updating: URLs, name (file_name), description (metadata), version, and authz
func (cl *IndexDClient) UpdateRecord(updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	// Get current revision from existing record
	record, err := cl.GetIndexdRecordByDID(did)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve existing record for DID %s: %v", did, err)
	}

	// Build update payload starting with existing record values
	updatePayload := UpdateInputInfo{
		URLs:     record.URLs,
		FileName: record.FileName,
		Version:  record.Version,
		Authz:    record.Authz,
		ACL:      record.ACL,
		Metadata: record.Metadata,
	}

	// Apply updates from updateInfo
	// Update URLs by appending new access methods (deduplicated)
	if len(updateInfo.AccessMethods) > 0 {
		// Collect new URLs from access methods
		newURLs := make([]string, 0, len(updateInfo.AccessMethods))
		for _, a := range updateInfo.AccessMethods {
			newURLs = append(newURLs, a.AccessURL.URL)
		}
		updatePayload.URLs = utils.AddUnique(updatePayload.URLs, newURLs)

		// Append authz from access methods (deduplicated)
		authz := indexdAuthzFromDrsAccessMethods(updateInfo.AccessMethods)
		updatePayload.Authz = utils.AddUnique(updatePayload.Authz, authz)
	}

	// Update name (maps to file_name in indexd)
	if updateInfo.Name != "" {
		updatePayload.FileName = updateInfo.Name
	}

	// Update version
	if updateInfo.Version != "" {
		updatePayload.Version = updateInfo.Version
	}

	// Update description (stored in metadata)
	if updateInfo.Description != "" {
		if updatePayload.Metadata == nil {
			updatePayload.Metadata = make(map[string]any)
		}
		updatePayload.Metadata["description"] = updateInfo.Description
	}

	jsonBytes, err := cl.SConfig.Marshal(updatePayload)
	if err != nil {
		return nil, fmt.Errorf("error marshaling indexd object form: %v", err)
	}

	cl.Logger.Printf("Prepared updated indexd object for DID %s: %s", did, string(jsonBytes))

	// prepare URL
	updateURL := fmt.Sprintf("%s/index/index/%s?rev=%s", cl.Base.String(), did, record.Rev)

	req, err := retryablehttp.NewRequest("PUT", updateURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("error creating PUT request: %v", err)
	}

	// Set required headers
	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	err = cl.AuthHandler.AddAuthHeader(req.Request)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	cl.Logger.Printf("PUT request created for indexd update: %s", updateURL)

	// Execute the request
	response, err := cl.HttpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing PUT request: %v", err)
	}
	defer response.Body.Close()

	// Check response status
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("failed to update indexd record %s: status %d, body: %s", did, response.StatusCode, string(body))
	}

	cl.Logger.Printf("PUT request successful: %s", response.Status)

	// Query and return the updated DRS object
	updatedDrsObj, err := cl.GetObject(did)
	if err != nil {
		return nil, fmt.Errorf("error retrieving updated DRS object: %v", err)
	}

	cl.Logger.Printf("Successfully updated and retrieved DRS object: %s", did)
	return updatedDrsObj, nil
}

// Helper function to get indexd record by DID (similar to existing pattern in DeleteIndexdRecord)
func (cl *IndexDClient) GetIndexdRecordByDID(did string) (*OutputInfo, error) {
	url := fmt.Sprintf("%s/index/%s", cl.Base.String(), did)

	req, err := retryablehttp.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	err = cl.AuthHandler.AddAuthHeader(req.Request)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}
	req.Request.Header.Set("accept", "application/json")

	resp, err := cl.HttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get record: status %d, body: %s", resp.StatusCode, string(body))
	}

	record := &OutputInfo{}
	if err := cl.SConfig.NewDecoder(resp.Body).Decode(record); err != nil {
		return nil, fmt.Errorf("error decoding response body: %v", err)
	}

	return record, nil
}

func (cl *IndexDClient) BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error) {
	bucket := cl.BucketName
	if bucket == "" {
		return nil, fmt.Errorf("error: bucket name is empty in config file")
	}

	fileURL := fmt.Sprintf("s3://%s", filepath.Join(bucket, drsId, checksum))

	authzStr, err := utils.ProjectToResource(cl.GetProjectId())
	if err != nil {
		return nil, err
	}
	authorizations := drs.Authorizations{
		Value: authzStr,
	}

	// create DrsObj
	DrsObj := drs.DRSObject{
		Id:   drsId,
		Name: fileName,
		// TODO: ensure that we can retrieve the access method during submission (happens in transfer)
		AccessMethods: []drs.AccessMethod{{AccessURL: drs.AccessURL{URL: fileURL}, Authorizations: &authorizations}},
		Checksums:     hash.HashInfo{SHA256: checksum},
		Size:          size,
	}

	return &DrsObj, nil
}

// Helper function to get indexd record by DID (similar to existing pattern in DeleteIndexdRecord)
func (cl *IndexDClient) getIndexdRecordByDID(did string) (*OutputInfo, error) {
	url := fmt.Sprintf("%s/index/%s", cl.Base.String(), did)

	req, err := retryablehttp.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	err = cl.AuthHandler.AddAuthHeader(req.Request)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}
	req.Header.Set("accept", "application/json")

	resp, err := cl.HttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get record: status %d, body: %s", resp.StatusCode, string(body))
	}

	record := &OutputInfo{}
	if err := cl.SConfig.NewDecoder(resp.Body).Decode(record); err != nil {
		return nil, fmt.Errorf("error decoding response body: %v", err)
	}

	return record, nil
}
