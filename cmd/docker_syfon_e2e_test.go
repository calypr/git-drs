//go:build integration

package cmd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	dockerE2EEnvVar          = "SYFON_E2E_DOCKER"
	dockerE2EMinioImage      = "minio/minio:RELEASE.2025-03-12T18-04-18Z"
	dockerE2EMinioBucket     = "syfon-e2e-bucket"
	dockerE2EMinioRegion     = "us-east-1"
	dockerE2EMinioAccessKey  = "minioadmin"
	dockerE2EMinioSecretKey  = "minioadmin123"
	dockerE2ECredentialKey   = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	dockerE2EServerReadyWait = 20 * time.Second
	dockerE2ELocalUser       = "drs-user"
	dockerE2ELocalPassword   = "drs-pass"
	dockerE2EOrganization    = "programs"
	dockerE2EProjectID       = "e2e"
	dockerE2EMultipartMB     = 1
	dockerE2EResumeAfter     = 2 * 1024 * 1024
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

type syfonServerProcess struct {
	url       string
	cmd       *exec.Cmd
	waitErrCh <-chan error
	stdout    *bytes.Buffer
	stderr    *bytes.Buffer
}

func TestMain(m *testing.M) {
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

	binPath := filepath.Join(gitDrsBinDir, "git-drs")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = root
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

	workDir := t.TempDir()
	remoteDir := filepath.Join(workDir, "remote.git")
	repoDir := filepath.Join(workDir, "repo")

	t.Logf("STEP 3: Creating git repository and configuring git-drs remote...")
	runCommand(t, workDir, nil, "git", "init", "--bare", remoteDir)
	runCommand(t, workDir, nil, "git", "--git-dir", remoteDir, "symbolic-ref", "HEAD", "refs/heads/main")
	runCommand(t, workDir, nil, "git", "init", "-b", "main", repoDir)
	configureLocalRepo(t, repoDir)
	runCommand(t, repoDir, nil, "git", "remote", "add", "origin", remoteDir)
	runCommand(t, repoDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, repoDir, server.url, minioEnv)

	t.Logf("STEP 4: Uploading tracked files through git-drs push...")
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
	largePointer := runCommand(t, repoDir, nil, "git", "show", "HEAD:data/multipart.bin")
	largeDid := parsePointerOID(t, []byte(largePointer))
	if out, err := runCommandOutput(t, repoDir, []string{"DATA_CLIENT_TEST_FAIL_UPLOAD_PART_ONCE=1"}, "git", "drs", "push", "origin"); err == nil {
		t.Fatalf("expected first multipart push to fail, but it succeeded:\n%s", out)
	}
	runCommand(t, repoDir, nil, "git", "drs", "push", "origin")

	t.Logf("STEP 5: Cloning and pulling the files back from Syfon...")
	cloneDir := filepath.Join(workDir, "clone")
	runCommand(t, workDir, []string{"GIT_LFS_SKIP_SMUDGE=1"}, "git", "clone", "--branch", "main", remoteDir, cloneDir)
	configureLocalRepo(t, cloneDir)
	runCommand(t, cloneDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, cloneDir, server.url, minioEnv)
	runCommand(t, cloneDir, nil, "git", "config", "--local", "drs.multipart-threshold", fmt.Sprintf("%d", dockerE2EMultipartMB))
	if out, err := runCommandOutput(t, cloneDir, []string{"DATA_CLIENT_TEST_FAIL_DOWNLOAD_AFTER_BYTES=" + fmt.Sprintf("%d", dockerE2EResumeAfter)}, "git", "drs", "pull", "origin"); err == nil {
		t.Fatalf("expected first multipart pull to fail, but it succeeded:\n%s", out)
	}
	runCommand(t, cloneDir, nil, "git", "drs", "pull", "origin")

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
	smallHashOut := runCommand(t, cloneDir, nil, "git", "drs", "sha256sum", "--did", smallDid)
	if !strings.Contains(smallHashOut, smallSumHex) {
		t.Fatalf("small file hash mismatch: expected %s in output %q", smallSumHex, smallHashOut)
	}

	largeSum := sha256.Sum256(largeData)
	largeSumHex := hex.EncodeToString(largeSum[:])
	largeHashOut := runCommand(t, cloneDir, nil, "git", "drs", "sha256sum", "--did", largeDid)
	if !strings.Contains(largeHashOut, largeSumHex) {
		t.Fatalf("multipart file hash mismatch: expected %s in output %q", largeSumHex, largeHashOut)
	}
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

	workDir := t.TempDir()
	remoteDir := filepath.Join(workDir, "remote.git")
	repoDir := filepath.Join(workDir, "repo")

	t.Logf("STEP 3: Creating git repository and configuring git-drs remote...")
	runCommand(t, workDir, nil, "git", "init", "--bare", remoteDir)
	runCommand(t, workDir, nil, "git", "--git-dir", remoteDir, "symbolic-ref", "HEAD", "refs/heads/main")
	runCommand(t, workDir, nil, "git", "init", "-b", "main", repoDir)
	configureLocalRepo(t, repoDir)
	runCommand(t, repoDir, nil, "git", "remote", "add", "origin", remoteDir)
	runCommand(t, repoDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, repoDir, server.url, minioEnv)

	t.Logf("STEP 4: Seeding objects directly into MinIO...")
	knownData := []byte("known add-url payload")
	knownHash := sha256.Sum256(knownData)
	knownOID := hex.EncodeToString(knownHash[:])
	knownKey := fmt.Sprintf("%s/%s/addurl/%s", dockerE2EOrganization, dockerE2EProjectID, knownOID)
	seedMinIOObject(t, minioEnv.s3Client, minioEnv.bucket, knownKey, knownData)

	unknownData := []byte("unknown add-url payload")
	unknownHash := sha256.Sum256(unknownData)
	unknownOID := hex.EncodeToString(unknownHash[:])
	unknownKey := fmt.Sprintf("%s/%s/addurl/%s-unknown", dockerE2EOrganization, dockerE2EProjectID, unknownOID)
	seedMinIOObject(t, minioEnv.s3Client, minioEnv.bucket, unknownKey, unknownData)

	t.Logf("STEP 5: Registering the objects via git-drs add-url...")
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

	runCommand(t, repoDir, nil, "git", "add", ".gitattributes", knownPath, unknownPath)
	runCommand(t, repoDir, nil, "git", "commit", "-m", "docker e2e add-url")
	runCommand(t, repoDir, nil, "git", "drs", "push", "origin")

	t.Logf("STEP 6: Cloning and pulling the add-url content back...")
	cloneDir := filepath.Join(workDir, "clone")
	runCommand(t, workDir, []string{"GIT_LFS_SKIP_SMUDGE=1"}, "git", "clone", "--branch", "main", remoteDir, cloneDir)
	configureLocalRepo(t, cloneDir)
	runCommand(t, cloneDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, cloneDir, server.url, minioEnv)
	runCommand(t, cloneDir, nil, "git", "drs", "pull", "origin")

	gotKnown := mustReadFile(t, cloneDir, knownPath)
	if !bytes.Equal(gotKnown, knownData) {
		t.Fatalf("known add-url file mismatch: got %q want %q", string(gotKnown), string(knownData))
	}
	gotUnknown := mustReadFile(t, cloneDir, unknownPath)
	if !bytes.Equal(gotUnknown, unknownData) {
		t.Fatalf("unknown add-url file mismatch: got %q want %q", string(gotUnknown), string(unknownData))
	}
}

func startMinIOContainer(ctx context.Context) (*minioContainer, error) {
	containerName := fmt.Sprintf("git-drs-minio-e2e-%d", time.Now().UnixNano())
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

	portCmd := exec.CommandContext(ctx, "docker", "port", containerID, "9000/tcp")
	portOut, err := portCmd.CombinedOutput()
	if err != nil {
		_ = stopDockerContainer(context.Background(), containerID)
		return nil, fmt.Errorf("docker port minio: %w\n%s", err, string(portOut))
	}
	endpoint, err := dockerPortEndpoint(string(portOut))
	if err != nil {
		_ = stopDockerContainer(context.Background(), containerID)
		return nil, err
	}
	if err := waitForHTTPReady(ctx, endpoint+"/minio/health/ready", 2*time.Minute); err != nil {
		_ = stopDockerContainer(context.Background(), containerID)
		return nil, err
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

func startSyfonServerProcess(t *testing.T, minioEnv *minioContainer) *syfonServerProcess {
	t.Helper()

	rootDir := findSyfonRoot(t)
	binaryPath := buildSyfonBinary(t, rootDir)
	port := reserveTCPPort(t)
	dbPath := filepath.Join(t.TempDir(), "docker-syfon-e2e.db")
	configPath := writeSyfonDockerConfig(t, port, dbPath, minioEnv)
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cmd := exec.Command(binaryPath, "serve", "--config", configPath)
	cmd.Dir = rootDir
	cmd.Env = append(os.Environ(), "DRS_CREDENTIAL_MASTER_KEY="+dockerE2ECredentialKey)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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
	out, err := cmd.CombinedOutput()
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

func configureLocalRepo(t *testing.T, dir string) {
	t.Helper()
	runCommand(t, dir, nil, "git", "config", "user.email", "git-drs-e2e@example.local")
	runCommand(t, dir, nil, "git", "config", "user.name", "git-drs-e2e")
	runCommand(t, dir, nil, "git", "config", "credential.helper", "git drs credential-helper")
	runCommand(t, dir, nil, "git", "config", "lfs.basictransfersonly", "true")
	runCommand(t, dir, nil, "git", "lfs", "install", "--local")
	runCommand(t, dir, nil, "git", "config", "--local", "push.default", "current")
}

func configureGitDrsRemote(t *testing.T, repoDir, serverURL string, minioEnv *minioContainer) {
	t.Helper()
	runCommand(t, repoDir, nil, "git", "drs", "remote", "add", "local", "origin", serverURL,
		"--bucket", minioEnv.bucket,
		"--organization", dockerE2EOrganization,
		"--project", dockerE2EProjectID,
		"--username", dockerE2ELocalUser,
		"--password", dockerE2ELocalPassword,
	)
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
