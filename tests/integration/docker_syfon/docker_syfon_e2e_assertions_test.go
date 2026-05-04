//go:build integration

package dockersyfon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func assertAccessURL(t *testing.T, queryOutput, wantURL string) {
	t.Helper()
	if !strings.Contains(queryOutput, wantURL) {
		t.Fatalf("query output missing access URL %q:\n%s", wantURL, queryOutput)
	}
}

func assertNoGeneratedProgramProjectPath(t *testing.T, queryOutput string) {
	t.Helper()
	if strings.Contains(queryOutput, "/programs/") || strings.Contains(queryOutput, "/projects/") {
		t.Fatalf("query output contains generated program/project storage path:\n%s", queryOutput)
	}
}

func verifyBucketScopeObject(t *testing.T, repoDir string, minioEnv *minioContainer, name, oid, key string) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		wantURL := "s3://" + minioEnv.bucket + "/" + key
		queryOut := runCommand(t, repoDir, nil, "git", "drs", "query", "--remote", "origin", "--checksum", "--pretty", oid)
		assertAccessURL(t, queryOut, wantURL)
		assertNoGeneratedProgramProjectPath(t, queryOut)
		assertMinIOObjectExists(t, minioEnv.s3Client, minioEnv.bucket, key)
	})
}

func addTrackedPayloadCommit(t *testing.T, repoDir, relPath string, data []byte, extraAddPaths ...string) string {
	t.Helper()
	fullPath := filepath.Join(repoDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir payload dir: %v", err)
	}
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		t.Fatalf("write payload %s: %v", relPath, err)
	}
	addPaths := append([]string{"add"}, extraAddPaths...)
	addPaths = append(addPaths, relPath)
	runCommand(t, repoDir, nil, "git", addPaths...)
	runCommand(t, repoDir, nil, "git", "commit", "-m", "add "+strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath)))
	pointer := runCommand(t, repoDir, nil, "git", "show", "HEAD:"+filepath.ToSlash(relPath))
	return parsePointerOID(t, []byte(pointer))
}

func setLocalBucketMapping(t *testing.T, repoDir, org, project, bucket, prefix string) {
	t.Helper()
	orgKey := normalizeDockerBucketMapKeyPart(org)
	if project == "" {
		runCommand(t, repoDir, nil, "git", "config", "--local", "drs.bucketmap.orgs."+orgKey+".bucket", bucket)
		if prefix != "" {
			runCommand(t, repoDir, nil, "git", "config", "--local", "drs.bucketmap.orgs."+orgKey+".prefix", strings.Trim(prefix, "/"))
		}
		return
	}
	projectKey := normalizeDockerBucketMapKeyPart(project)
	runCommand(t, repoDir, nil, "git", "config", "--local", "drs.bucketmap.projects."+orgKey+"."+projectKey+".bucket", bucket)
	if prefix != "" {
		runCommand(t, repoDir, nil, "git", "config", "--local", "drs.bucketmap.projects."+orgKey+"."+projectKey+".prefix", strings.Trim(prefix, "/"))
	}
}

func normalizeDockerBucketMapKeyPart(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, ".", "_")
	re := regexp.MustCompile(`[^a-z0-9_-]+`)
	v = re.ReplaceAllString(v, "_")
	return strings.Trim(v, "_")
}

func upsertSyfonBucketScope(t *testing.T, serverURL string, minioEnv *minioContainer, org, project, path string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"bucket":             minioEnv.bucket,
		"provider":           "s3",
		"region":             minioEnv.region,
		"access_key":         minioEnv.accessKey,
		"secret_key":         minioEnv.secretKey,
		"endpoint":           minioEnv.endpoint,
		"billing_log_bucket": minioEnv.bucket,
		"billing_log_prefix": dockerE2EProviderLogPrefix,
		"organization":       org,
		"project_id":         project,
		"path":               path,
	})
	if err != nil {
		t.Fatalf("marshal bucket scope request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, strings.TrimRight(serverURL, "/")+"/data/buckets", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build bucket scope request: %v", err)
	}
	req.SetBasicAuth(dockerE2ELocalUser, dockerE2ELocalPassword)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put bucket scope org=%s project=%s path=%s: %v", org, project, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("put bucket scope org=%s project=%s path=%s status=%d body=%s", org, project, path, resp.StatusCode, string(respBody))
	}
}

func verifyProviderTransferMetrics(t *testing.T, serverURL string, minioEnv *minioContainer, events []providerTransferLogEvent, wantBytes int64) {
	t.Helper()
	if len(events) == 0 {
		t.Fatalf("provider transfer metrics verification requires at least one event")
	}
	body, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		t.Fatalf("marshal provider transfer events: %v", err)
	}
	from := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339Nano)
	to := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339Nano)
	postJSONBasic(t, strings.TrimRight(serverURL, "/")+"/index/v1/metrics/provider-transfer-events", body, http.StatusCreated)

	summaryURL := fmt.Sprintf("%s/index/v1/metrics/transfers/summary?organization=%s&project=%s&provider=s3&bucket=%s&direction=download&from=%s&to=%s",
		strings.TrimRight(serverURL, "/"),
		dockerE2EOrganization,
		dockerE2EProjectID,
		minioEnv.bucket,
		urlQueryEscape(from),
		urlQueryEscape(to),
	)
	respBody := getBasic(t, summaryURL, http.StatusOK)
	var summary struct {
		EventCount      int64 `json:"event_count"`
		BytesDownloaded int64 `json:"bytes_downloaded"`
		Freshness       struct {
			IsStale             bool       `json:"is_stale"`
			LatestCompletedSync *time.Time `json:"latest_completed_sync"`
			MissingBuckets      []string   `json:"missing_buckets"`
		} `json:"freshness"`
	}
	if err := json.Unmarshal(respBody, &summary); err != nil {
		t.Fatalf("decode transfer summary: %v body=%s", err, string(respBody))
	}
	if summary.EventCount < int64(len(events)) || summary.BytesDownloaded < wantBytes {
		t.Fatalf("expected provider transfer metrics events=%d bytes>=%d, got %+v body=%s", len(events), wantBytes, summary, string(respBody))
	}
	if summary.Freshness.IsStale || len(summary.Freshness.MissingBuckets) != 0 {
		t.Fatalf("expected fresh provider transfer metrics after sync, got %+v body=%s", summary.Freshness, string(respBody))
	}
}

type providerTransferLogEvent struct {
	ProviderEventID   string `json:"provider_event_id"`
	Direction         string `json:"direction"`
	EventTime         string `json:"event_time"`
	ProviderRequestID string `json:"provider_request_id"`
	Organization      string `json:"organization"`
	Project           string `json:"project"`
	Provider          string `json:"provider"`
	Bucket            string `json:"bucket"`
	ObjectKey         string `json:"object_key"`
	StorageURL        string `json:"storage_url"`
	BytesTransferred  int64  `json:"bytes_transferred"`
	HTTPMethod        string `json:"http_method"`
	HTTPStatus        int    `json:"http_status"`
	UserAgent         string `json:"user_agent"`
}

func newProviderDownloadEvent(eventID string, minioEnv *minioContainer, key string, bytesTransferred int64) providerTransferLogEvent {
	return providerTransferLogEvent{
		ProviderEventID:   eventID,
		Direction:         "download",
		EventTime:         time.Now().UTC().Format(time.RFC3339Nano),
		ProviderRequestID: eventID + "-request",
		Organization:      dockerE2EOrganization,
		Project:           dockerE2EProjectID,
		Provider:          "s3",
		Bucket:            minioEnv.bucket,
		ObjectKey:         strings.TrimLeft(key, "/"),
		StorageURL:        "s3://" + minioEnv.bucket + "/" + strings.TrimLeft(key, "/"),
		BytesTransferred:  bytesTransferred,
		HTTPMethod:        "GET",
		HTTPStatus:        http.StatusOK,
		UserAgent:         "git-drs-docker-e2e",
	}
}

func postJSONBasic(t *testing.T, target string, body []byte, wantStatus int) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build POST %s: %v", target, err)
	}
	req.SetBasicAuth(dockerE2ELocalUser, dockerE2ELocalPassword)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST %s status=%d want=%d body=%s", target, resp.StatusCode, wantStatus, string(respBody))
	}
	return respBody
}

func getBasic(t *testing.T, target string, wantStatus int) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", target, err)
	}
	req.SetBasicAuth(dockerE2ELocalUser, dockerE2ELocalPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s status=%d want=%d body=%s", target, resp.StatusCode, wantStatus, string(body))
	}
	return body
}

func urlQueryEscape(v string) string {
	return url.QueryEscape(v)
}

func assertMinIOObjectExists(t *testing.T, client *s3.Client, bucket, key string) {
	t.Helper()
	_, err := client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("expected MinIO object %s/%s to exist: %v", bucket, key, err)
	}
}
