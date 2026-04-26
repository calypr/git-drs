//go:build integration

package dockersyfon

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

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
