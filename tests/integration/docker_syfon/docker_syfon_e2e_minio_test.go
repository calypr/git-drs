//go:build integration

package dockersyfon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func startMinIOContainer(ctx context.Context) (*minioContainer, error) {
	containerName := fmt.Sprintf("git-drs-minio-e2e-%d", time.Now().UnixNano())
	fmt.Fprintf(os.Stderr, "starting MinIO container %s\n", containerName)
	runArgs := []string{
		"run", "-d", "--rm",
		"--name", containerName,
		"-e", "MINIO_ROOT_USER=" + dockerE2EMinioAccessKey,
		"-e", "MINIO_ROOT_PASSWORD=" + dockerE2EMinioSecretKey,
		"-p", "127.0.0.1::9000",
		dockerE2EMinioImage,
		"server", "/data", "--address", ":9000",
	}
	runCmd := exec.CommandContext(ctx, "docker", runArgs...)
	out, err := runCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker run minio: %w\n%s", err, string(out))
	}
	containerID := strings.TrimSpace(string(out))
	if containerID == "" {
		return nil, fmt.Errorf("docker run minio returned empty container id")
	}
	fmt.Fprintf(os.Stderr, "MinIO container id %s\n", containerID)

	portCmd := exec.CommandContext(ctx, "docker", "port", containerID, "9000/tcp")
	portOut, err := portCmd.CombinedOutput()
	if err != nil {
		_ = stopDockerContainer(context.Background(), containerID)
		return nil, fmt.Errorf("docker port minio: %w\n%s\nlogs:\n%s", err, string(portOut), dockerContainerLogs(context.Background(), containerID))
	}
	endpoint, err := dockerPortEndpoint(string(portOut))
	if err != nil {
		_ = stopDockerContainer(context.Background(), containerID)
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "MinIO endpoint %s\n", endpoint)
	if err := waitForHTTPReady(ctx, endpoint+"/minio/health/ready", 2*time.Minute); err != nil {
		logs := dockerContainerLogs(context.Background(), containerID)
		_ = stopDockerContainer(context.Background(), containerID)
		return nil, fmt.Errorf("wait for MinIO ready at %s: %w\nlogs:\n%s", endpoint, err, logs)
	}
	s3Client, err := newMinIOClient(ctx, endpoint)
	if err != nil {
		_ = stopDockerContainer(context.Background(), containerID)
		return nil, err
	}
	if err := ensureMinIOBucket(ctx, s3Client, dockerE2EMinioBucket); err != nil {
		_ = stopDockerContainer(context.Background(), containerID)
		return nil, err
	}

	return &minioContainer{
		containerID: containerID,
		endpoint:    endpoint,
		bucket:      dockerE2EMinioBucket,
		region:      dockerE2EMinioRegion,
		accessKey:   dockerE2EMinioAccessKey,
		secretKey:   dockerE2EMinioSecretKey,
		s3Client:    s3Client,
	}, nil
}

func newMinIOClient(ctx context.Context, endpoint string) (*s3.Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(dockerE2EMinioRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(dockerE2EMinioAccessKey, dockerE2EMinioSecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	cfg.BaseEndpoint = aws.String(endpoint)
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
	}), nil
}

func ensureMinIOBucket(ctx context.Context, client *s3.Client, bucket string) error {
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "bucketalready") {
		return fmt.Errorf("create bucket %s: %w", bucket, err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait for bucket %s: %w", bucket, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func seedMinIOObject(t *testing.T, client *s3.Client, bucket, key string, body []byte) {
	t.Helper()
	_, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(body),
	})
	if err != nil {
		t.Fatalf("put object %s/%s: %v", bucket, key, err)
	}
}

func temporarilyRemoveMinIOObject(t *testing.T, client *s3.Client, bucket, oidOrKey string, expectedSize int64) func() {
	t.Helper()

	key, err := resolveMinIOObjectKey(context.Background(), client, bucket, oidOrKey, expectedSize)
	if err != nil {
		t.Fatalf("resolve object key for %s/%s: %v", bucket, oidOrKey, err)
	}

	resp, err := client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("get object %s/%s: %v", bucket, key, err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read object %s/%s: %v", bucket, key, err)
	}
	if _, err := client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}); err != nil {
		t.Fatalf("delete object %s/%s: %v", bucket, key, err)
	}

	return func() {
		t.Helper()
		seedMinIOObject(t, client, bucket, key, body)
	}
}

func resolveMinIOObjectKey(ctx context.Context, client *s3.Client, bucket, oidOrKey string, expectedSize int64) (string, error) {
	candidate := strings.TrimSpace(oidOrKey)
	if candidate == "" {
		return "", fmt.Errorf("object id is required")
	}

	prefix := strings.TrimLeft(strings.TrimSpace(candidate), "/")
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	})
	var keyMatches []string
	var sizeMatches []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return "", err
		}
		for _, obj := range page.Contents {
			key := strings.TrimSpace(aws.ToString(obj.Key))
			if key == "" {
				continue
			}
			if key == candidate || key == prefix || strings.HasSuffix(key, "/"+candidate) || strings.HasSuffix(key, candidate) {
				keyMatches = append(keyMatches, key)
				continue
			}
			if expectedSize > 0 && *obj.Size == expectedSize {
				sizeMatches = append(sizeMatches, key)
			}
		}
	}
	if len(keyMatches) == 1 {
		return keyMatches[0], nil
	}
	if len(keyMatches) > 1 {
		return "", fmt.Errorf("multiple object keys matching %q found in bucket %q: %v", candidate, bucket, keyMatches)
	}
	if len(sizeMatches) == 1 {
		return sizeMatches[0], nil
	}
	if len(sizeMatches) > 1 {
		return "", fmt.Errorf("no key matching %q found in bucket %q; multiple objects matched size %d: %v", candidate, bucket, expectedSize, sizeMatches)
	}
	return "", fmt.Errorf("no object key matching %q or size %d found in bucket %q", candidate, expectedSize, bucket)
}
