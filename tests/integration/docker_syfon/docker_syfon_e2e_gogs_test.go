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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
