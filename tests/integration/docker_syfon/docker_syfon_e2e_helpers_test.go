//go:build integration

package dockersyfon

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

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
	if out, err := runCommandOutput(t, dir, nil, "git", "config", "--global", "--get-regexp", "^filter\\.drs\\."); err == nil {
		t.Logf("snapshot output: git config --global --get-regexp ^filter\\.drs\\.\n%s", out)
	} else {
		t.Logf("snapshot git drs global filter config unavailable: %v", err)
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
