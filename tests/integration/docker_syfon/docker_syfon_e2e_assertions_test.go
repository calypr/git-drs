//go:build integration

package dockersyfon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

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
		"bucket":       minioEnv.bucket,
		"provider":     "s3",
		"region":       minioEnv.region,
		"access_key":   minioEnv.accessKey,
		"secret_key":   minioEnv.secretKey,
		"endpoint":     minioEnv.endpoint,
		"organization": org,
		"project_id":   project,
		"path":         path,
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
