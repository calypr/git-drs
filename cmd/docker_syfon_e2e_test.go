//go:build integration

package cmd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	dockerE2EEnvVar            = "SYFON_E2E_DOCKER"
	dockerE2EMinioImage        = "minio/minio:RELEASE.2025-03-12T18-04-18Z"
	dockerE2EMinioBucket       = "syfon-e2e-bucket"
	dockerE2EMinioRegion       = "us-east-1"
	dockerE2EMinioAccessKey    = "minioadmin"
	dockerE2EMinioSecretKey    = "minioadmin123"
	dockerE2ECredentialKey     = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	dockerE2EServerReadyWait   = 20 * time.Second
	dockerE2ELocalUser         = "drs-user"
	dockerE2ELocalPassword     = "drs-pass"
	dockerE2EOrganization      = "programs"
	dockerE2EProjectID         = "e2e"
	dockerE2EMultipartMB       = 1
	dockerE2EResumeAfter       = 2 * 1024 * 1024
	dockerE2EGogsImage         = "gogs/gogs"
	dockerE2EGogsAdminUser     = "git-drs-e2e"
	dockerE2EGogsAdminPassword = "git-drs-e2e-pass"
	dockerE2EGogsAdminEmail    = "git-drs-e2e@example.local"
	dockerE2EGogsRepoName      = "git-drs-e2e"
)

var gitDrsBinDir string

type minioContainer struct {
	containerID string
	endpoint    string
	bucket      string
	region      string
	accessKey   string
	secretKey   string
	s3Client    *s3.Client
}

type gogsContainer struct {
	containerID     string
	endpoint        string
	hostPort        string
	adminUser       string
	adminPassword   string
	adminEmail      string
	repoName        string
	repoOwner       string
	repoCloneURL    string
	apiToken        string
	credentialStore string
}

type syfonServerProcess struct {
	url       string
	cmd       *exec.Cmd
	waitErrCh <-chan error
	stdout    *bytes.Buffer
	stderr    *bytes.Buffer
}

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Stderr.WriteString("skipping docker-backed integration tests in -short mode\n")
		os.Exit(0)
	}

	wd, err := os.Getwd()
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("could not get working directory: %v\n", err))
		os.Exit(2)
	}
	root := filepath.Dir(wd)

	gitDrsBinDir, err = os.MkdirTemp("", "git-drs-docker-e2e-bin-")
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("could not create temp binary dir: %v\n", err))
		os.Exit(2)
	}
	os.Stderr.WriteString(fmt.Sprintf("building git-drs integration binary into %s\n", gitDrsBinDir))

	binPath := filepath.Join(gitDrsBinDir, "git-drs")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = root
	os.Stderr.WriteString(fmt.Sprintf("building git-drs from %s\n", root))
	if out, err := build.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		os.Stderr.WriteString(fmt.Sprintf("build error: %v\n", err))
		_ = os.RemoveAll(gitDrsBinDir)
		os.Exit(2)
	}

	code := m.Run()
	_ = os.RemoveAll(gitDrsBinDir)
	os.Exit(code)
}

func TestGitDrsDockerMinIOE2E(t *testing.T) {
	if strings.TrimSpace(os.Getenv(dockerE2EEnvVar)) != "1" {
		t.Skipf("set %s=1 to run the Docker-backed MinIO integration test", dockerE2EEnvVar)
	}

	ctx := context.Background()
	t.Logf("STEP 1: Starting MinIO Docker container...")
	minioEnv, err := startMinIOContainer(ctx)
	if err != nil {
		if isDockerUnavailable(err) {
			t.Skipf("Docker is unavailable for %s: %v", dockerE2EEnvVar, err)
		}
		t.Fatalf("failed to start MinIO container: %v", err)
	}
	t.Logf("MinIO started at %s (bucket: %s)", minioEnv.endpoint, minioEnv.bucket)
	t.Logf("MinIO credentials: region=%s access_key=%s secret_key=%s", minioEnv.region, minioEnv.accessKey, "<redacted>")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := stopDockerContainer(cleanupCtx, minioEnv.containerID); err != nil {
			t.Logf("warning: failed to terminate MinIO container: %v", err)
		}
	})

	t.Logf("STEP 2: Starting Syfon server process...")
	server := startSyfonServerProcess(t, minioEnv)
	t.Logf("Syfon server listening at %s", server.url)
	t.Cleanup(func() {
		stopSyfonServerProcess(t, server)
	})
	t.Logf("Syfon server ready: healthz=%s/healthz", server.url)

	t.Logf("STEP 3: Starting Gogs Git server process...")
	gogsEnv := startGogsContainer(t, ctx)
	t.Logf("Gogs server listening at %s (repo clone URL: %s)", gogsEnv.endpoint, gogsEnv.repoCloneURL)
	t.Logf("Gogs admin user=%s repo=%s token=%t", gogsEnv.adminUser, gogsEnv.repoName, gogsEnv.apiToken != "")

	workDir := t.TempDir()
	repoDir := filepath.Join(workDir, "repo")
	cloneDir := filepath.Join(workDir, "clone")
	t.Logf("Working directories: workDir=%s repoDir=%s cloneDir=%s", workDir, repoDir, cloneDir)
	t.Logf("Git credential store: %s", gogsEnv.credentialStore)

	t.Logf("STEP 4: Creating git repository and configuring git-drs remote...")
	runCommand(t, workDir, nil, "git", "init", "-b", "main", repoDir)
	configureLocalRepo(t, repoDir, gogsEnv.credentialStore)
	runCommand(t, repoDir, nil, "git", "remote", "add", "origin", gogsEnv.repoCloneURL)
	runCommand(t, repoDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, repoDir, server.url, minioEnv)
	logRepoSnapshot(t, repoDir, "post-init")

	t.Logf("STEP 5: Uploading tracked files through git-drs push...")
	smallPath := filepath.Join(repoDir, "data", "source.txt")
	smallData := []byte("git-drs docker minio e2e payload")
	if err := os.MkdirAll(filepath.Dir(smallPath), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(smallPath, smallData, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	largePath := filepath.Join(repoDir, "data", "multipart.bin")
	largeData := bytes.Repeat([]byte("syfon-git-drs-multipart-e2e-"), 7*1024*1024/len("syfon-git-drs-multipart-e2e-")+1)
	largeData = largeData[:7*1024*1024]
	if err := os.WriteFile(largePath, largeData, 0o644); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	runCommand(t, repoDir, nil, "git", "lfs", "track", "*.txt", "*.bin")
	runCommand(t, repoDir, nil, "git", "config", "--local", "drs.multipart-threshold", fmt.Sprintf("%d", dockerE2EMultipartMB))
	runCommand(t, repoDir, nil, "git", "add", ".gitattributes", "data/source.txt", "data/multipart.bin")
	runCommand(t, repoDir, nil, "git", "commit", "-m", "docker e2e upload")
	smallPointer := runCommand(t, repoDir, nil, "git", "show", "HEAD:data/source.txt")
	smallDid := parsePointerOID(t, []byte(smallPointer))
	t.Logf("source.txt pointer DID=%s", smallDid)
	largePointer := runCommand(t, repoDir, nil, "git", "show", "HEAD:data/multipart.bin")
	largeDid := parsePointerOID(t, []byte(largePointer))
	t.Logf("multipart.bin pointer DID=%s", largeDid)
	logRepoSnapshot(t, repoDir, "pre-push")
	if err := writeGitCredentialStore(gogsEnv.credentialStore, gogsEnv.repoCloneURL, dockerE2EGogsAdminUser, "wrong-password"); err != nil {
		t.Fatalf("write transient bad git credential store: %v", err)
	}
	if out, err := runCommandOutput(t, repoDir, nil, "git", "drs", "push", "origin"); err == nil {
		t.Fatalf("expected first multipart push to fail, but it succeeded:\n%s", out)
	} else {
		t.Logf("expected first multipart push failure captured for retry path")
	}
	if err := writeGitCredentialStore(gogsEnv.credentialStore, gogsEnv.repoCloneURL, dockerE2EGogsAdminUser, dockerE2EGogsAdminPassword); err != nil {
		t.Fatalf("restore git credential store: %v", err)
	}
	runCommand(t, repoDir, nil, "git", "drs", "push", "origin")
	logRepoSnapshot(t, repoDir, "post-push")
	querySmall := runCommand(t, repoDir, nil, "git", "drs", "query", "--remote", "origin", "--pretty", smallDid)
	if !strings.Contains(querySmall, smallDid) {
		t.Fatalf("query output missing small DID %s:\n%s", smallDid, querySmall)
	}
	t.Logf("query verified source.txt DID %s", smallDid)
	queryLarge := runCommand(t, repoDir, nil, "git", "drs", "query", "--remote", "origin", "--pretty", largeDid)
	if !strings.Contains(queryLarge, largeDid) {
		t.Fatalf("query output missing multipart DID %s:\n%s", largeDid, queryLarge)
	}
	t.Logf("query verified multipart.bin DID %s", largeDid)

	t.Logf("STEP 6: Cloning and pulling the files back from Syfon through Gogs...")
	runCommand(t, workDir, []string{"GIT_LFS_SKIP_SMUDGE=1"}, "git", "-c", "credential.helper=store --file "+gogsEnv.credentialStore, "clone", "--branch", "main", gogsEnv.repoCloneURL, cloneDir)
	configureLocalRepo(t, cloneDir, gogsEnv.credentialStore)
	runCommand(t, cloneDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, cloneDir, server.url, minioEnv)
	runCommand(t, cloneDir, nil, "git", "config", "--local", "drs.multipart-threshold", fmt.Sprintf("%d", dockerE2EMultipartMB))
	logRepoSnapshot(t, cloneDir, "pre-pull")

	largeObjectKey := dockerE2EObjectKey(largeDid)
	restoreLargeObject := temporarilyRemoveMinIOObject(t, minioEnv.s3Client, minioEnv.bucket, largeObjectKey)
	if out, err := runCommandOutput(t, cloneDir, nil, "git", "drs", "pull", "origin"); err == nil {
		t.Fatalf("expected first multipart pull to fail, but it succeeded:\n%s", out)
	} else {
		t.Logf("expected first multipart pull failure captured for retry path")
	}
	restoreLargeObject()
	runCommand(t, cloneDir, nil, "git", "drs", "pull", "origin")
	logRepoSnapshot(t, cloneDir, "post-pull")

	got, err := os.ReadFile(filepath.Join(cloneDir, "data", "source.txt"))
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if !bytes.Equal(got, smallData) {
		t.Fatalf("pulled bytes mismatch: got %q want %q", string(got), string(smallData))
	}
	gotLarge, err := os.ReadFile(filepath.Join(cloneDir, "data", "multipart.bin"))
	if err != nil {
		t.Fatalf("read pulled multipart file: %v", err)
	}
	if !bytes.Equal(gotLarge, largeData) {
		t.Fatalf("pulled multipart bytes mismatch: got %d bytes want %d bytes", len(gotLarge), len(largeData))
	}

	smallSum := sha256.Sum256(smallData)
	smallSumHex := hex.EncodeToString(smallSum[:])
	smallHashOut := runCommand(t, cloneDir, nil, "git", "drs", "query", "--remote", "origin", "--checksum", "--pretty", smallSumHex)
	if !strings.Contains(smallHashOut, smallDid) || !strings.Contains(smallHashOut, smallSumHex) {
		t.Fatalf("small file checksum lookup mismatch: expected DID %s and hash %s in output %q", smallDid, smallSumHex, smallHashOut)
	}

	largeSum := sha256.Sum256(largeData)
	largeSumHex := hex.EncodeToString(largeSum[:])
	largeHashOut := runCommand(t, cloneDir, nil, "git", "drs", "query", "--remote", "origin", "--checksum", "--pretty", largeSumHex)
	if !strings.Contains(largeHashOut, largeDid) || !strings.Contains(largeHashOut, largeSumHex) {
		t.Fatalf("multipart file checksum lookup mismatch: expected DID %s and hash %s in output %q", largeDid, largeSumHex, largeHashOut)
	}
	t.Logf("hash verification complete for source.txt=%s multipart.bin=%s", smallSumHex, largeSumHex)
}

func TestGitDrsDockerAddURLE2E(t *testing.T) {
	if strings.TrimSpace(os.Getenv(dockerE2EEnvVar)) != "1" {
		t.Skipf("set %s=1 to run the Docker-backed MinIO integration test", dockerE2EEnvVar)
	}

	ctx := context.Background()
	t.Logf("STEP 1: Starting MinIO Docker container...")
	minioEnv, err := startMinIOContainer(ctx)
	if err != nil {
		if isDockerUnavailable(err) {
			t.Skipf("Docker is unavailable for %s: %v", dockerE2EEnvVar, err)
		}
		t.Fatalf("failed to start MinIO container: %v", err)
	}
	t.Logf("MinIO started at %s (bucket: %s)", minioEnv.endpoint, minioEnv.bucket)
	t.Logf("MinIO credentials: region=%s access_key=%s secret_key=%s", minioEnv.region, minioEnv.accessKey, "<redacted>")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := stopDockerContainer(cleanupCtx, minioEnv.containerID); err != nil {
			t.Logf("warning: failed to terminate MinIO container: %v", err)
		}
	})

	t.Logf("STEP 2: Starting Syfon server process...")
	server := startSyfonServerProcess(t, minioEnv)
	t.Logf("Syfon server listening at %s", server.url)
	t.Cleanup(func() {
		stopSyfonServerProcess(t, server)
	})
	t.Logf("Syfon server ready: healthz=%s/healthz", server.url)

	t.Logf("STEP 3: Starting Gogs Git server process...")
	gogsEnv := startGogsContainer(t, ctx)
	t.Logf("Gogs server listening at %s (repo clone URL: %s)", gogsEnv.endpoint, gogsEnv.repoCloneURL)
	t.Logf("Gogs admin user=%s repo=%s token=%t", gogsEnv.adminUser, gogsEnv.repoName, gogsEnv.apiToken != "")

	workDir := t.TempDir()
	repoDir := filepath.Join(workDir, "repo")
	cloneDir := filepath.Join(workDir, "clone")
	t.Logf("Working directories: workDir=%s repoDir=%s cloneDir=%s", workDir, repoDir, cloneDir)
	t.Logf("Git credential store: %s", gogsEnv.credentialStore)

	t.Logf("STEP 4: Creating git repository and configuring git-drs remote...")
	runCommand(t, workDir, nil, "git", "init", "-b", "main", repoDir)
	configureLocalRepo(t, repoDir, gogsEnv.credentialStore)
	runCommand(t, repoDir, nil, "git", "remote", "add", "origin", gogsEnv.repoCloneURL)
	runCommand(t, repoDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, repoDir, server.url, minioEnv)
	logRepoSnapshot(t, repoDir, "post-init")

	t.Logf("STEP 5: Seeding objects directly into MinIO...")
	knownData := []byte("known add-url payload")
	knownHash := sha256.Sum256(knownData)
	knownOID := hex.EncodeToString(knownHash[:])
	knownKey := fmt.Sprintf("%s/%s/addurl/%s", dockerE2EOrganization, dockerE2EProjectID, knownOID)
	t.Logf("known add-url object: key=%s oid=%s size=%d", knownKey, knownOID, len(knownData))
	seedMinIOObject(t, minioEnv.s3Client, minioEnv.bucket, knownKey, knownData)

	unknownData := []byte("unknown add-url payload")
	unknownHash := sha256.Sum256(unknownData)
	unknownOID := hex.EncodeToString(unknownHash[:])
	unknownKey := fmt.Sprintf("%s/%s/addurl/%s-unknown", dockerE2EOrganization, dockerE2EProjectID, unknownOID)
	t.Logf("unknown add-url object: key=%s oid=%s size=%d", unknownKey, unknownOID, len(unknownData))
	seedMinIOObject(t, minioEnv.s3Client, minioEnv.bucket, unknownKey, unknownData)

	t.Logf("STEP 6: Registering the objects via git-drs add-url...")
	knownPath := filepath.Join("data", "from-bucket.bin")
	knownOut := runCommandMust(t, repoDir, []string{
		"TEST_BUCKET_REGION=" + minioEnv.region,
		"TEST_BUCKET_ENDPOINT=" + minioEnv.endpoint,
		"TEST_BUCKET_ACCESS_KEY=" + minioEnv.accessKey,
		"TEST_BUCKET_SECRET_KEY=" + minioEnv.secretKey,
	}, "git", "drs", "add-url", "s3://"+minioEnv.bucket+"/"+knownKey, knownPath, "--sha256", knownOID)
	if strings.TrimSpace(knownOut) == "" {
		t.Log("known add-url completed")
	}
	t.Logf("known add-url output:\n%s", knownOut)

	unknownPath := filepath.Join("data", "from-bucket-unknown.bin")
	unknownOut := runCommandMust(t, repoDir, []string{
		"TEST_BUCKET_REGION=" + minioEnv.region,
		"TEST_BUCKET_ENDPOINT=" + minioEnv.endpoint,
		"TEST_BUCKET_ACCESS_KEY=" + minioEnv.accessKey,
		"TEST_BUCKET_SECRET_KEY=" + minioEnv.secretKey,
	}, "git", "drs", "add-url", "s3://"+minioEnv.bucket+"/"+unknownKey, unknownPath)
	if strings.TrimSpace(unknownOut) == "" {
		t.Log("unknown add-url completed")
	}
	t.Logf("unknown add-url output:\n%s", unknownOut)

	knownPointer := mustReadFile(t, repoDir, knownPath)
	knownPointerOID := parsePointerOID(t, knownPointer)
	if knownPointerOID != knownOID {
		t.Fatalf("known add-url pointer oid mismatch: got %s want %s", knownPointerOID, knownOID)
	}

	unknownPointer := mustReadFile(t, repoDir, unknownPath)
	unknownPointerOID := parsePointerOID(t, unknownPointer)
	if unknownPointerOID == "" {
		t.Fatalf("unknown add-url pointer oid is empty")
	}
	if unknownPointerOID == unknownOID {
		t.Fatalf("unknown add-url unexpectedly used the real sha256")
	}
	t.Logf("add-url pointer checks complete: known=%s unknown=%s", knownPointerOID, unknownPointerOID)

	runCommand(t, repoDir, nil, "git", "add", ".gitattributes", knownPath, unknownPath)
	runCommand(t, repoDir, nil, "git", "commit", "-m", "docker e2e add-url")
	runCommand(t, repoDir, nil, "git", "drs", "push", "origin")
	logRepoSnapshot(t, repoDir, "post-add-url-push")

	t.Logf("STEP 7: Cloning and pulling the add-url content back through Gogs...")
	runCommand(t, workDir, []string{"GIT_LFS_SKIP_SMUDGE=1"}, "git", "-c", "credential.helper=store --file "+gogsEnv.credentialStore, "clone", "--branch", "main", gogsEnv.repoCloneURL, cloneDir)
	configureLocalRepo(t, cloneDir, gogsEnv.credentialStore)
	runCommand(t, cloneDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, cloneDir, server.url, minioEnv)
	runCommand(t, cloneDir, nil, "git", "drs", "pull", "origin")
	logRepoSnapshot(t, cloneDir, "post-add-url-pull")

	gotKnown := mustReadFile(t, cloneDir, knownPath)
	if !bytes.Equal(gotKnown, knownData) {
		t.Fatalf("known add-url file mismatch: got %q want %q", string(gotKnown), string(knownData))
	}
	gotUnknown := mustReadFile(t, cloneDir, unknownPath)
	if !bytes.Equal(gotUnknown, unknownData) {
		t.Fatalf("unknown add-url file mismatch: got %q want %q", string(gotUnknown), string(unknownData))
	}
	t.Logf("add-url round-trip verification complete")
}

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

func startGogsContainer(t *testing.T, ctx context.Context) *gogsContainer {
	t.Helper()

	hostPort := reserveTCPPort(t)
	hostDir := t.TempDir()
	customConfDir := filepath.Join(hostDir, "gogs", "custom", "conf")
	if err := os.MkdirAll(customConfDir, 0o755); err != nil {
		t.Fatalf("mkdir gogs config dir: %v", err)
	}
	configPath := filepath.Join(customConfDir, "app.ini")
	gogsURL := fmt.Sprintf("http://127.0.0.1:%d", hostPort)
	gogsAPIBase := gogsURL + "/api/v1"
	t.Logf("starting gogs container on %s with data dir %s", gogsURL, hostDir)
	writeGogsAppConfig(t, configPath, hostPort)

	containerName := fmt.Sprintf("git-drs-gogs-e2e-%d", time.Now().UnixNano())
	runArgs := []string{
		"run", "-d", "--rm",
		"--name", containerName,
		"-e", "GOGS_WORK_DIR=/data/gogs",
		"-e", "GOGS_CUSTOM=/data/gogs/custom",
		"-p", fmt.Sprintf("127.0.0.1:%d:3000", hostPort),
		"-v", hostDir + ":/data",
		dockerE2EGogsImage,
	}
	runCmd := exec.CommandContext(ctx, "docker", runArgs...)
	out, err := runCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker run gogs: %v\n%s", err, string(out))
	}
	containerID := extractDockerContainerID(string(out))
	if containerID == "" {
		t.Fatalf("docker run gogs returned empty container id:\n%s", string(out))
	}
	t.Logf("gogs container id %s", containerID)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := stopDockerContainer(cleanupCtx, containerID); err != nil {
			t.Logf("warning: failed to terminate Gogs container: %v", err)
		}
	})
	go streamDockerContainerLogs(t, containerID)

	if err := waitForHTTPAvailable(ctx, gogsURL, 2*time.Minute); err != nil {
		logs := dockerContainerLogs(context.Background(), containerID)
		t.Fatalf("wait for gogs ready at %s: %v\nlogs:\n%s", gogsURL, err, logs)
	}

	adminCreateArgs := []string{
		"exec", "-u", "git", containerID,
		"/app/gogs/gogs", "admin", "create-user",
		"--name", dockerE2EGogsAdminUser,
		"--password", dockerE2EGogsAdminPassword,
		"--email", dockerE2EGogsAdminEmail,
		"--admin",
		"--config", "/data/gogs/custom/conf/app.ini",
	}
	t.Logf("creating gogs admin user in container %s as %s", containerID, dockerE2EGogsAdminUser)
	adminCreateCmd := exec.CommandContext(ctx, "docker", adminCreateArgs...)
	adminOut, err := adminCreateCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker exec gogs admin create-user: %v\n%s", err, string(adminOut))
	}
	t.Logf("gogs admin create-user output:\n%s", string(adminOut))

	token, err := createGogsAccessToken(ctx, gogsAPIBase, dockerE2EGogsAdminUser, dockerE2EGogsAdminPassword, "git-drs-e2e-token")
	if err != nil {
		t.Logf("warning: token creation failed, falling back to basic-auth repo bootstrap: %v", err)
	}

	repo, err := createGogsRepo(ctx, gogsAPIBase, token, dockerE2EGogsRepoName)
	if err != nil {
		if token != "" {
			t.Logf("warning: token-auth repo creation failed, falling back to basic auth: %v", err)
		}
		repo, err = createGogsRepoWithBasicAuth(ctx, gogsAPIBase, dockerE2EGogsAdminUser, dockerE2EGogsAdminPassword, dockerE2EGogsRepoName)
		if err != nil {
			t.Fatalf("create gogs repo: %v", err)
		}
	}
	if repo == nil || repo.CloneURL == "" {
		t.Fatalf("gogs repo creation returned empty clone_url")
	}
	t.Logf("gogs repo created: html_url=%s clone_url=%s", repo.HTMLURL, repo.CloneURL)

	credentialStore := filepath.Join(hostDir, ".git-credentials")
	if err := writeGitCredentialStore(credentialStore, repo.CloneURL, dockerE2EGogsAdminUser, dockerE2EGogsAdminPassword); err != nil {
		t.Fatalf("write git credential store: %v", err)
	}
	t.Logf("git credential store written to %s", credentialStore)

	return &gogsContainer{
		containerID:     containerID,
		endpoint:        gogsURL,
		hostPort:        fmt.Sprintf("%d", hostPort),
		adminUser:       dockerE2EGogsAdminUser,
		adminPassword:   dockerE2EGogsAdminPassword,
		adminEmail:      dockerE2EGogsAdminEmail,
		repoName:        dockerE2EGogsRepoName,
		repoOwner:       dockerE2EGogsAdminUser,
		repoCloneURL:    repo.CloneURL,
		apiToken:        token,
		credentialStore: credentialStore,
	}
}

func writeGogsAppConfig(t *testing.T, configPath string, hostPort int) {
	t.Helper()

	content := fmt.Sprintf(`RUN_MODE = prod

[server]
PROTOCOL = http
HTTP_ADDR = 0.0.0.0
HTTP_PORT = 3000
DOMAIN = 127.0.0.1
EXTERNAL_URL = http://127.0.0.1:%d/
APP_DATA_PATH = /data/gogs/data

[database]
TYPE = sqlite3
PATH = /data/gogs/data/gogs.db

[repository]
ROOT = /home/git/gogs-repositories

[security]
INSTALL_LOCK = true

[lfs]
STORAGE = local
OBJECTS_PATH = /data/gogs/data/lfs-objects
`, hostPort)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write gogs config file: %v", err)
	}
	t.Logf("wrote gogs app.ini to %s", configPath)
}

type gogsAccessToken struct {
	Name string `json:"name"`
	Sha1 string `json:"sha1"`
}

type gogsRepository struct {
	HTMLURL  string `json:"html_url"`
	CloneURL string `json:"clone_url"`
}

func createGogsAccessToken(ctx context.Context, apiBase, username, password, tokenName string) (string, error) {
	reqBody := map[string]string{"name": tokenName}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/users/"+username+"/tokens", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("post token request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("create access token status %d: %s", resp.StatusCode, string(respBody))
	}

	var token gogsAccessToken
	if err := json.Unmarshal(respBody, &token); err != nil {
		return "", fmt.Errorf("decode access token response: %w: %s", err, string(respBody))
	}
	if token.Sha1 == "" {
		return "", fmt.Errorf("create access token returned empty sha1: %s", string(respBody))
	}
	return token.Sha1, nil
}

func createGogsRepo(ctx context.Context, apiBase, token, repoName string) (*gogsRepository, error) {
	reqBody := map[string]any{
		"name":      repoName,
		"auto_init": false,
		"private":   false,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal repo request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/user/repos", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create repo request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	return decodeGogsRepoResponse(req)
}

func createGogsRepoWithBasicAuth(ctx context.Context, apiBase, username, password, repoName string) (*gogsRepository, error) {
	reqBody := map[string]any{
		"name":      repoName,
		"auto_init": false,
		"private":   false,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal repo request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/user/repos", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create repo request: %w", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "application/json")

	return decodeGogsRepoResponse(req)
}

func decodeGogsRepoResponse(req *http.Request) (*gogsRepository, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post repo request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("create repo status %d: %s", resp.StatusCode, string(respBody))
	}

	var repo gogsRepository
	if err := json.Unmarshal(respBody, &repo); err != nil {
		return nil, fmt.Errorf("decode repo response: %w: %s", err, string(respBody))
	}
	return &repo, nil
}

func writeGitCredentialStore(path, remoteURL, username, password string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir credential dir: %w", err)
	}
	entryURL, err := credentialStoreURL(remoteURL, username, password)
	if err != nil {
		return err
	}
	entry := entryURL + "\n"
	return os.WriteFile(path, []byte(entry), 0o600)
}

func credentialStoreURL(remoteURL, username, password string) (string, error) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(remoteURL, "/"))
	if trimmed == "" {
		return "", fmt.Errorf("remote URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse remote URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("remote URL must include scheme and host")
	}

	parsed.User = urlUserPassword(username, password)
	return parsed.String(), nil
}

func urlUserPassword(username, password string) *url.Userinfo {
	return url.UserPassword(username, password)
}

func TestCredentialStoreURLIncludesRepoPath(t *testing.T) {
	got, err := credentialStoreURL("http://127.0.0.1:63438/git-drs-e2e/git-drs-e2e.git", "git-drs-e2e", "secret")
	if err != nil {
		t.Fatalf("credentialStoreURL returned error: %v", err)
	}
	want := "http://git-drs-e2e:secret@127.0.0.1:63438/git-drs-e2e/git-drs-e2e.git"
	if got != want {
		t.Fatalf("credentialStoreURL = %q, want %q", got, want)
	}
}

func TestWriteGitCredentialStoreUsesFullRepoURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".git-credentials")

	if err := writeGitCredentialStore(path, "http://127.0.0.1:63438/git-drs-e2e/git-drs-e2e.git", "git-drs-e2e", "secret"); err != nil {
		t.Fatalf("writeGitCredentialStore returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read credential store: %v", err)
	}
	want := "http://git-drs-e2e:secret@127.0.0.1:63438/git-drs-e2e/git-drs-e2e.git\n"
	if string(got) != want {
		t.Fatalf("credential store contents = %q, want %q", string(got), want)
	}
}

func startSyfonServerProcess(t *testing.T, minioEnv *minioContainer) *syfonServerProcess {
	t.Helper()

	rootDir := findSyfonRoot(t)
	binaryPath := buildSyfonBinary(t, rootDir)
	port := reserveTCPPort(t)
	dbPath := filepath.Join(t.TempDir(), "docker-syfon-e2e.db")
	configPath := writeSyfonDockerConfig(t, port, dbPath, minioEnv)
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	t.Logf("starting syfon server: root=%s binary=%s port=%d db=%s config=%s", rootDir, binaryPath, port, dbPath, configPath)

	cmd := exec.Command(binaryPath, "serve", "--config", configPath)
	cmd.Dir = rootDir
	cmd.Env = append(os.Environ(), "DRS_CREDENTIAL_MASTER_KEY="+dockerE2ECredentialKey)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	t.Logf("syfon server command: %s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}

	go streamToTestLog(t, "[SERVER STDOUT]", stdoutPipe, stdoutBuf)
	go streamToTestLog(t, "[SERVER STDERR]", stderrPipe, stderrBuf)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start syfon server: %v", err)
	}

	waitErrCh := make(chan error, 1)
	go func() {
		waitErrCh <- cmd.Wait()
	}()

	if err := waitForServerReady(serverURL, waitErrCh, dockerE2EServerReadyWait); err != nil {
		logServerProcessOutput(t, serverURL, stdoutBuf, stderrBuf)
		stopSyfonServerProcess(t, &syfonServerProcess{cmd: cmd, waitErrCh: waitErrCh, stdout: stdoutBuf, stderr: stderrBuf})
		t.Fatalf("wait for server ready: %v", err)
	}

	return &syfonServerProcess{
		url:       serverURL,
		cmd:       cmd,
		waitErrCh: waitErrCh,
		stdout:    stdoutBuf,
		stderr:    stderrBuf,
	}
}

func stopSyfonServerProcess(t *testing.T, server *syfonServerProcess) {
	t.Helper()
	if server == nil || server.cmd == nil || server.cmd.Process == nil {
		return
	}
	if server.cmd.ProcessState != nil {
		return
	}

	_ = syscall.Kill(-server.cmd.Process.Pid, syscall.SIGINT)
	select {
	case <-server.waitErrCh:
		return
	case <-time.After(5 * time.Second):
	}

	_ = server.cmd.Process.Kill()
	select {
	case <-server.waitErrCh:
	case <-time.After(5 * time.Second):
		logServerProcessOutput(t, server.url, server.stdout, server.stderr)
		t.Fatalf("server process did not exit cleanly")
	}
}

func waitForServerReady(baseURL string, waitErrCh <-chan error, timeout time.Duration) error {
	client := &http.Client{Timeout: 1 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	const requiredConsecutiveSuccesses = 2
	successes := 0
	interval := 100 * time.Millisecond

	for {
		select {
		case err := <-waitErrCh:
			return fmt.Errorf("server exited before ready: %w", err)
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for /healthz after %s", timeout)
		default:
		}

		resp, err := client.Get(baseURL + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				successes++
				if successes >= requiredConsecutiveSuccesses {
					return nil
				}
			} else {
				successes = 0
			}
		} else {
			successes = 0
		}

		timer := time.NewTimer(interval)
		select {
		case err := <-waitErrCh:
			timer.Stop()
			return fmt.Errorf("server exited before ready: %w", err)
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("timed out waiting for /healthz after %s", timeout)
		case <-timer.C:
		}
		if interval < time.Second {
			interval *= 2
			if interval > time.Second {
				interval = time.Second
			}
		}
	}
}

func writeSyfonDockerConfig(t *testing.T, port int, dbPath string, minioEnv *minioContainer) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Logf("writing syfon config to %s for bucket=%s endpoint=%s db=%s port=%d", configPath, minioEnv.bucket, minioEnv.endpoint, dbPath, port)
	content := fmt.Sprintf(`port: %d
auth:
  mode: local
  basic:
    username: %s
    password: %s
routes:
  ga4gh: true
  internal: true
database:
  sqlite:
    file: %q
s3_credentials:
  - bucket: %q
    provider: s3
    region: %q
    access_key: %q
    secret_key: %q
    endpoint: %q
`, port, dockerE2ELocalUser, dockerE2ELocalPassword, dbPath, minioEnv.bucket, minioEnv.region, minioEnv.accessKey, minioEnv.secretKey, minioEnv.endpoint)

	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return configPath
}

func buildSyfonBinary(t *testing.T, rootDir string) string {
	t.Helper()

	binaryPath := filepath.Join(t.TempDir(), "syfon-docker-e2e")
	t.Logf("building syfon binary from %s into %s", rootDir, binaryPath)
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = rootDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build syfon binary: %v\n%s", err, string(out))
	}
	return binaryPath
}

func reserveTCPPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve tcp port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func findSyfonRoot(t *testing.T) string {
	t.Helper()

	if root := strings.TrimSpace(os.Getenv("TEST_SYFON_ROOT")); root != "" {
		return root
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	for {
		candidate := filepath.Join(dir, "..", "syfon")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find syfon checkout from %s", dir)
		}
		dir = parent
	}
}

func logServerProcessOutput(t *testing.T, serverURL string, stdoutBuf, stderrBuf *bytes.Buffer) {
	t.Helper()
	if stdoutBuf != nil {
		t.Logf("server %s stdout:\n%s", serverURL, stdoutBuf.String())
	}
	if stderrBuf != nil {
		t.Logf("server %s stderr:\n%s", serverURL, stderrBuf.String())
	}
}

func streamToTestLog(t *testing.T, prefix string, r io.Reader, buf *bytes.Buffer) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		t.Logf("%s %s", prefix, line)
		if buf != nil {
			buf.WriteString(line + "\n")
		}
	}
	if err := scanner.Err(); err != nil {
		t.Logf("%s stream error: %v", prefix, err)
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

func temporarilyRemoveMinIOObject(t *testing.T, client *s3.Client, bucket, key string) func() {
	t.Helper()

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

func dockerE2EObjectKey(oid string) string {
	return fmt.Sprintf("programs/%s/projects/%s/%s", dockerE2EOrganization, dockerE2EProjectID, strings.TrimSpace(oid))
}

func runCommand(t *testing.T, dir string, extraEnv []string, name string, args ...string) string {
	t.Helper()
	out, err := runCommandOutput(t, dir, extraEnv, name, args...)
	if err != nil {
		t.Fatalf("command failed: %s %v\n%s", name, args, out)
	}
	return out
}

func runCommandMust(t *testing.T, dir string, extraEnv []string, name string, args ...string) string {
	t.Helper()
	out, err := runCommandOutput(t, dir, extraEnv, name, args...)
	if err != nil {
		t.Fatalf("command failed: %s %v\n%s", name, args, out)
	}
	return out
}

func runCommandOutput(t *testing.T, dir string, extraEnv []string, name string, args ...string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = gitDrsCommandEnv(extraEnv)
	t.Logf("RUN dir=%s env=%s cmd=%s %s", dir, summarizeExtraEnv(extraEnv), name, strings.Join(args, " "))
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		t.Logf("FAIL after %s: %s %v\n%s", elapsed, name, args, string(out))
	} else {
		t.Logf("OK after %s: %s %v\n%s", elapsed, name, args, string(out))
	}
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("command timed out: %s %v", name, args)
	}
	return string(out), err
}

func gitDrsCommandEnv(extraEnv []string) []string {
	path := os.Getenv("PATH")
	if gitDrsBinDir != "" {
		path = gitDrsBinDir + string(os.PathListSeparator) + path
	}
	env := append([]string{}, os.Environ()...)
	env = append(env, "PATH="+path)
	env = append(env, extraEnv...)
	return env
}

func summarizeExtraEnv(extraEnv []string) string {
	if len(extraEnv) == 0 {
		return "<none>"
	}
	parts := make([]string, 0, len(extraEnv))
	for _, kv := range extraEnv {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			parts = append(parts, kv)
			continue
		}
		if shouldRedactEnvKey(key) {
			parts = append(parts, key+"=<redacted>")
			continue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ", ")
}

func shouldRedactEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	return strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "PASSWORD") ||
		strings.Contains(upper, "TOKEN") ||
		strings.Contains(upper, "AUTH") ||
		strings.Contains(upper, "KEY")
}

func mustReadFile(t *testing.T, root, relPath string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, relPath))
	if err != nil {
		t.Fatalf("read file %s: %v", relPath, err)
	}
	return data
}

func parsePointerOID(t *testing.T, pointer []byte) string {
	t.Helper()
	for _, line := range strings.Split(string(pointer), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "oid sha256:") {
			return strings.TrimPrefix(line, "oid sha256:")
		}
	}
	t.Fatalf("missing oid in LFS pointer: %q", string(pointer))
	return ""
}

func configureLocalRepo(t *testing.T, dir, credentialStore string) {
	t.Helper()
	runCommand(t, dir, nil, "git", "config", "user.email", "git-drs-e2e@example.local")
	runCommand(t, dir, nil, "git", "config", "user.name", "git-drs-e2e")
	if strings.TrimSpace(credentialStore) != "" {
		runCommand(t, dir, nil, "git", "config", "credential.helper", fmt.Sprintf("store --file %s", credentialStore))
	}
	runCommand(t, dir, nil, "git", "config", "lfs.basictransfersonly", "true")
	runCommand(t, dir, nil, "git", "lfs", "install", "--local")
	runCommand(t, dir, nil, "git", "config", "--local", "push.default", "current")
	logRepoSnapshot(t, dir, "post-local-config")
}

func configureGitDrsRemote(t *testing.T, repoDir, serverURL string, minioEnv *minioContainer) {
	t.Helper()
	t.Logf("configuring git-drs remote: repo=%s server=%s bucket=%s org=%s project=%s", repoDir, serverURL, minioEnv.bucket, dockerE2EOrganization, dockerE2EProjectID)
	runCommand(t, repoDir, nil, "git", "drs", "remote", "add", "local", "origin", serverURL,
		"--bucket", minioEnv.bucket,
		"--organization", dockerE2EOrganization,
		"--project", dockerE2EProjectID,
		"--username", dockerE2ELocalUser,
		"--password", dockerE2ELocalPassword,
	)
	if out, err := runCommandOutput(t, repoDir, nil, "git", "config", "--local", "--get-regexp", "^drs\\.remote\\.origin\\."); err == nil {
		t.Logf("git-drs remote config:\n%s", out)
	} else {
		t.Logf("warning: unable to dump git-drs remote config: %v", err)
	}
}

func dockerPortEndpoint(portOutput string) (string, error) {
	fields := strings.Fields(portOutput)
	if len(fields) == 0 {
		return "", fmt.Errorf("docker port returned empty output")
	}
	last := strings.TrimSpace(fields[len(fields)-1])
	if last == "" {
		return "", fmt.Errorf("docker port returned empty host mapping")
	}
	if !strings.Contains(last, ":") {
		return "", fmt.Errorf("docker port output missing host:port: %q", portOutput)
	}
	return "http://" + last, nil
}

func logRepoSnapshot(t *testing.T, dir, label string) {
	t.Helper()
	t.Logf("REPO SNAPSHOT [%s] dir=%s", label, dir)
	for _, args := range [][]string{
		{"git", "status", "--short"},
		{"git", "remote", "-v"},
		{"git", "config", "--local", "--list"},
	} {
		out, err := runCommandOutput(t, dir, nil, args[0], args[1:]...)
		if err != nil {
			t.Logf("snapshot command failed (continuing): %s %v\n%s", args[0], args[1:], out)
			continue
		}
		t.Logf("snapshot output: %s %v\n%s", args[0], args[1:], out)
	}
	if out, err := runCommandOutput(t, dir, nil, "git", "lfs", "env"); err == nil {
		t.Logf("snapshot output: git lfs env\n%s", out)
	} else {
		t.Logf("snapshot git lfs env unavailable: %v", err)
	}
}

func stopDockerContainer(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm -f %s: %w\n%s", containerID, err, string(out))
	}
	return nil
}

func dockerContainerLogs(ctx context.Context, containerID string) string {
	if strings.TrimSpace(containerID) == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, "docker", "logs", containerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("docker logs failed: %v\n%s", err, string(out))
	}
	return string(out)
}

func streamDockerContainerLogs(t *testing.T, containerID string) {
	t.Helper()
	cmd := exec.Command("docker", "logs", "-f", "--since", "0s", containerID)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Logf("warning: unable to attach docker logs stdout: %v", err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Logf("warning: unable to attach docker logs stderr: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		t.Logf("warning: unable to start docker logs stream: %v", err)
		return
	}
	go streamToTestLog(t, "[GOGS STDOUT]", stdout, nil)
	go streamToTestLog(t, "[GOGS STDERR]", stderr, nil)
	go func() {
		if err := cmd.Wait(); err != nil {
			t.Logf("docker logs stream ended: %v", err)
		}
	}()
}

func extractDockerContainerID(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	re := regexp.MustCompile(`^[0-9a-f]{12,}$`)
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if re.MatchString(line) {
			return line
		}
	}
	return ""
}

func waitForHTTPReady(ctx context.Context, url string, timeout time.Duration) error {
	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %s after %s", url, timeout)
		case <-ticker.C:
		}
	}
}

func waitForHTTPAvailable(ctx context.Context, url string, timeout time.Duration) error {
	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			resp, reqErr := client.Do(req)
			if reqErr == nil {
				_ = resp.Body.Close()
				if resp.StatusCode < 500 {
					return nil
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %s after %s", url, timeout)
		case <-ticker.C:
		}
	}
}

func isDockerUnavailable(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "docker daemon") ||
		strings.Contains(lower, "docker: command not found") ||
		strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "cannot connect") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "rootless docker not found") ||
		strings.Contains(lower, "failed to create docker provider") ||
		strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "failed to create container")
}
