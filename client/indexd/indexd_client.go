package indexd_client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/calypr/data-client/client/commonUtils"
	"github.com/calypr/data-client/client/g3cmd"
	"github.com/calypr/data-client/client/jwt"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/projectdir"
	"github.com/calypr/git-drs/s3_utils"
	"github.com/calypr/git-drs/utils"

	gen3Client "github.com/calypr/data-client/client/gen3Client"
)

//var conf jwt.Configure
//var ProfileConfig jwt.Credential

type IndexDClient struct {
	Base        *url.URL
	ProjectId   string
	BucketName  string
	Logger      *log.Logger
	AuthHandler s3_utils.AuthHandler // Injected for testing/flexibility
}

////////////////////
// CLIENT METHODS //
////////////////////

// load repo-level config and return a new IndexDClient
func NewIndexDClient(profileConfig jwt.Credential, remote Gen3Remote, logger *log.Logger) (client.DRSClient, error) {

	baseUrl, err := url.Parse(profileConfig.APIEndpoint)
	// get the gen3Project and gen3Bucket from the config
	projectId := remote.GetProjectId()
	if projectId == "" {
		return nil, fmt.Errorf("no gen3 project specified. Run 'git drs init', use the '--help' flag for more info")
	}

	bucketName := remote.GetBucketName()
	//TODO: Is this really a failure state?
	//if bucketName == "" {
	//	return nil, fmt.Errorf("No gen3 bucket specified. Run 'git drs init', use the '--help' flag for more info")
	//}

	return &IndexDClient{
		Base:        baseUrl,
		ProjectId:   projectId,
		BucketName:  bucketName,
		Logger:      logger,
		AuthHandler: &RealAuthHandler{profileConfig}, // Use real auth in production
	}, err
}

func (cl *IndexDClient) GetProjectId() string {
	return cl.ProjectId
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
	delReq, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	err = cl.AuthHandler.AddAuthHeader(delReq)
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
	// FIXME: how do we not hardcode sha256 here?
	records, err := cl.GetObjectByHash(&hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid})
	if err != nil {
		cl.Logger.Printf("error getting DRS object for OID %s: %s", oid, err)
		return nil, fmt.Errorf("error getting DRS object for OID %s: %v", oid, err)
	}
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

	// Get the DRS object for the matching record
	drsObj, err := cl.GetObject(matchingRecord.Id)
	if err != nil {
		cl.Logger.Printf("error getting DRS object for matching record %s: %s", matchingRecord.Id, err)
		return nil, fmt.Errorf("error getting DRS object for matching record %s: %v", matchingRecord.Id, err)
	}

	// FIXME: generalize access ID method
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

	req, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}

	err = cl.AuthHandler.AddAuthHeader(req)
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

	cl.Logger.Printf("signed url retrieved: %s", response.Status)

	return &accessUrl, nil
}

// RegisterFile implements DRSClient.
// This function registers a file with gen3 indexd, writes the file to the bucket,
// and returns the successful DRS object.
// DRS will use any matching indexd record / file that already exists
func (cl *IndexDClient) RegisterFile(oid string) (*drs.DRSObject, error) {
	cl.Logger.Printf("register file started for oid: %s", oid)

	// get all existing hashes
	records, err := cl.GetObjectByHash(&hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid})
	if err != nil {
		return nil, fmt.Errorf("error querying indexd server for matches to hash %s: %v", oid, err)
	}

	// use any indexd record from the same project if it exists
	//  * addresses edge case where user X registering in project A has access to record in project B
	//  * but still needs create a new record to so user Y reading the file in project A can access it
	//  * even if they don't have access to project B
	var drsObject *drs.DRSObject
	if len(records) > 0 {
		var err error
		drsObject, err = drsmap.FindMatchingRecord(records, cl.ProjectId)
		if err != nil {
			return nil, fmt.Errorf("error finding matching record for project %s: %v", cl.ProjectId, err)
		}
	}

	if drsObject == nil {
		// otherwise, create indexd record
		cl.Logger.Print("creating record: no existing indexd record for this project")

		// get indexd object using drs map
		drsObject, err = drsmap.DrsInfoFromOid(oid)
		if err != nil {
			return nil, fmt.Errorf("error getting indexd object for oid %s: %v", oid, err)
		}

		indexdObj, err := indexdRecordFromDrsObject(drsObject)
		if err != nil {
			return nil, fmt.Errorf("error converting DRS object to indexd record: %v", err)
		}

		// register the record
		drsObject, err = cl.RegisterIndexdRecord(indexdObj)

		if err != nil {
			cl.Logger.Printf("error registering indexd record: %s", err)
			return nil, fmt.Errorf("error registering indexd record: %v", err)
		}
	}

	// delete indexd record if subsequent file upload code errors out
	defer func() {
		if err != nil {
			cl.Logger.Printf("registration incomplete, cleaning up indexd record for oid %s", oid)
			err = cl.DeleteIndexdRecord(drsObject.Id)
			if err != nil {
				cl.Logger.Printf("error cleaning up indexd record on failed registration for oid %s: %s", oid, err)
				cl.Logger.Printf("please delete the indexd record manually if needed for DRS ID: %s", drsObject.Id)
				cl.Logger.Printf("see https://uc-cdis.github.io/gen3sdk-python/_build/html/indexing.html")
				return
			}
			cl.Logger.Printf("cleaned up indexd record for oid %s", oid)
		}
	}()

	// determine if file is downloadable
	isDownloadable := true
	cl.Logger.Print("checking if file is downloadable")
	signedUrl, err := cl.GetDownloadURL(oid)
	if err != nil || signedUrl == nil {
		isDownloadable = false
	} else { // signedUrl exists
		err = utils.CanDownloadFile(signedUrl.URL)
		if err != nil {
			isDownloadable = false
		} else {
			cl.Logger.Printf("file with oid %s is downloadable", oid)
		}
	}

	// if file is not downloadable, then upload it to bucket
	if !isDownloadable {
		cl.Logger.Printf("file with oid %s not downloadable from bucket, proceeding to upload", oid)

		filePath, err := drsmap.GetObjectPath(projectdir.LFS_OBJS_PATH, oid)
		if err != nil {
			cl.Logger.Printf("error getting object path for oid %s: %s", oid, err)
			return nil, fmt.Errorf("error getting object path for oid %s: %v", oid, err)
		}

		profile, err := cl.GetProfile()
		if err != nil {
			return nil, fmt.Errorf("error getting profile for upload: %v", err)
		}

		g3, err := gen3Client.NewGen3InterfaceWithLogger(profile, cl.Logger)
		if err != nil {
			return nil, fmt.Errorf("error creating Gen3 interface: %v", err)
		}
		err = g3cmd.MultipartUpload(
			context.TODO(),
			g3,
			commonUtils.FileUploadRequestObject{
				FilePath:     filePath,
				Filename:     filepath.Base(filePath),
				GUID:         drsObject.Id,
				FileMetadata: commonUtils.FileMetadata{},
			},
			cl.BucketName, false,
		)
		if err != nil {
			cl.Logger.Printf("error uploading file to bucket: %s", err)
			return nil, fmt.Errorf("error uploading file to bucket: %v", err)
		}
	} else {
		cl.Logger.Print("file exists in bucket, skipping upload")
	}

	// if all successful, remove temp DRS object
	drsPath, err := drsmap.GetObjectPath(projectdir.DRS_OBJS_PATH, oid)
	if err == nil {
		_ = os.Remove(drsPath)
	}

	// return drsObject
	return drsObject, nil
}

func (cl *IndexDClient) GetObject(id string) (*drs.DRSObject, error) {

	a := *cl.Base
	a.Path = filepath.Join(a.Path, "ga4gh/drs/v1/objects", id)

	req, err := http.NewRequest("GET", a.String(), nil)
	if err != nil {
		return nil, err
	}

	err = cl.AuthHandler.AddAuthHeader(req)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.Status == "404" {
		return nil, fmt.Errorf("%s not found", id)
	}

	in := drs.OutputObject{}
	if err := json.NewDecoder(response.Body).Decode(&in); err != nil {
		return nil, err
	}
	return drs.ConvertOutputObjectToDRSObject(&in), nil

}

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
			req, err := http.NewRequest("GET", a.String(), nil)
			if err != nil {
				cl.Logger.Printf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}

			q := req.URL.Query()
			q.Add("limit", fmt.Sprintf("%d", LIMIT))
			q.Add("page", fmt.Sprintf("%d", pageNum))
			req.URL.RawQuery = q.Encode()

			err = cl.AuthHandler.AddAuthHeader(req)
			if err != nil {
				cl.Logger.Printf("error contacting %s : %s", req.URL, err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}

			// execute request with error checking
			client := &http.Client{}
			response, err := client.Do(req)
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
			err = json.Unmarshal(body, &page)
			if err != nil {
				cl.Logger.Printf("error: %s", err)
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

// given indexd record, constructs a new indexd record
// implements /index/index POST
func (cl *IndexDClient) RegisterIndexdRecord(indexdObj *IndexdRecord) (*drs.DRSObject, error) {
	indexdObjForm := IndexdRecordForm{
		IndexdRecord: *indexdObj,
		Form:         "object",
	}

	jsonBytes, _ := json.Marshal(indexdObjForm)
	cl.Logger.Printf("retrieved IndexdObj: %s", string(jsonBytes))

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
	err = cl.AuthHandler.AddAuthHeader(req)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	cl.Logger.Printf("POST request created for indexd: %s", endpt.String())

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
	cl.Logger.Printf("POST successful: %s", response.Status)

	// query and return DRS object
	drsObj, err := cl.GetObject(indexdObjForm.Did)
	if err != nil {
		return nil, fmt.Errorf("error querying DRS ID %s: %v", drsId, err)
	}
	cl.Logger.Printf("GET for DRS ID successful: %s", drsObj.Id)
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
	delReq, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	err = cl.AuthHandler.AddAuthHeader(delReq)
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

// implements /index/index?hash={hashType}:{hash} GET
func (cl *IndexDClient) GetObjectByHash(sum *hash.Checksum) ([]drs.DRSObject, error) {
	// setup get request to indexd
	url := fmt.Sprintf("%s/index/index?hash=%s:%s", cl.Base.String(), sum.Type, sum.Checksum)
	cl.Logger.Printf("Querying indexd at %s", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		cl.Logger.Printf("http.NewRequest Error: %s", err)
		return nil, err
	}
	cl.Logger.Printf("Looking for files with hash %s:%s", sum.Type, sum.Checksum)

	err = cl.AuthHandler.AddAuthHeader(req)
	if err != nil {
		return nil, fmt.Errorf("unable to add authentication when searching for object: %s:%s. More on the error: %v", sum.Type, sum.Checksum, err)
	}
	req.Header.Set("accept", "application/json")

	// run request and do checks
	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
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
	err = json.NewDecoder(resp.Body).Decode(&records)
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
	for i := range records.Records {
		out[i] = *indexdRecordToDrsObject(records.Records[i].ToIndexdRecord())
	}
	return out, nil
}

// implements /index/index?authz={resource_path}&start={start}&limit={limit} GET
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
		active := true
		for active {
			// setup request
			req, err := http.NewRequest("GET", a.String(), nil)
			if err != nil {
				cl.Logger.Printf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}

			q := req.URL.Query()
			q.Add("authz", resourcePath)
			q.Add("limit", fmt.Sprintf("%d", PAGESIZE))
			q.Add("page", fmt.Sprintf("%d", pageNum))
			req.URL.RawQuery = q.Encode()

			err = cl.AuthHandler.AddAuthHeader(req)
			if err != nil {
				cl.Logger.Printf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}

			// execute request with error checking
			httpClient := &http.Client{}
			response, err := httpClient.Do(req)
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
				body, _ := io.ReadAll(response.Body)
				cl.Logger.Printf("%d: check that your credentials are valid \nfull message: %s", response.StatusCode, body)
				out <- drs.DRSObjectResult{Error: fmt.Errorf("%d: check your credentials are valid, \nfull message: %s", response.StatusCode, body)}
				return
			}

			// return page of DRS objects
			page := &ListRecords{}
			err = json.Unmarshal(body, &page)
			if err != nil {
				cl.Logger.Printf("error: %s", err)
				out <- drs.DRSObjectResult{Error: err}
				return
			}
			for _, elem := range page.Records {
				out <- drs.DRSObjectResult{Object: elem.ToIndexdRecord().ToDrsObject()}
			}
			if len(page.Records) == 0 {
				active = false
			}
			pageNum++
		}
		//cl.Logger.Printf("total pages retrieved: %d", pageNum)
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
		// Build set of existing URLs for deduplication
		existingURLs := make(map[string]bool)
		for _, url := range updatePayload.URLs {
			existingURLs[url] = true
		}

		// Append only new URLs
		for _, a := range updateInfo.AccessMethods {
			if !existingURLs[a.AccessURL.URL] {
				updatePayload.URLs = append(updatePayload.URLs, a.AccessURL.URL)
				existingURLs[a.AccessURL.URL] = true
			}
		}

		// Append authz from access methods (deduplicated)
		existingAuthz := make(map[string]bool)
		for _, authz := range updatePayload.Authz {
			existingAuthz[authz] = true
		}

		authz := indexdAuthzFromDrsAccessMethods(updateInfo.AccessMethods)
		for _, a := range authz {
			if !existingAuthz[a] {
				updatePayload.Authz = append(updatePayload.Authz, a)
				existingAuthz[a] = true
			}
		}
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

	jsonBytes, err := json.Marshal(updatePayload)
	if err != nil {
		return nil, fmt.Errorf("error marshaling indexd object form: %v", err)
	}

	cl.Logger.Printf("Prepared updated indexd object for DID %s: %s", did, string(jsonBytes))

	// prepare URL
	updateURL := fmt.Sprintf("%s/index/index/%s?rev=%s", cl.Base.String(), did, record.Rev)

	req, err := http.NewRequest("PUT", updateURL, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, fmt.Errorf("error creating PUT request: %v", err)
	}

	// Set required headers
	req.Header.Set("accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	err = cl.AuthHandler.AddAuthHeader(req)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}

	cl.Logger.Printf("PUT request created for indexd update: %s", updateURL)

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

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	err = cl.AuthHandler.AddAuthHeader(req)
	if err != nil {
		return nil, fmt.Errorf("error adding Gen3 auth header: %v", err)
	}
	req.Header.Set("accept", "application/json")

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
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

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	err = cl.AuthHandler.AddAuthHeader(req)
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
