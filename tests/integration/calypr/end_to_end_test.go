package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	dcJWT "github.com/calypr/data-client/client/jwt"
	"github.com/calypr/git-drs/projectdir"
	"github.com/calypr/git-drs/utils"
)

const DEFAULT_BUCKET string = "cbds"

func TestEndToEndGitDRSWorkflow(t *testing.T) {
	// GitHub Enterprise Server details
	host := "https://source.ohsu.edu"
	owner := "CBDS"
	project := generateRandomString(8)
	repoName := "test-" + project
	token := os.Getenv("GH_PAT")
	if token == "" {
		t.Fatal("GH_PAT environment variable not set")
	}

	profile := os.Getenv("GIT_DRS_PROFILE")
	if profile == "" {
		t.Fatal("GIT_DRS_PROFILE environment variable not set")
	}

	var err error
	// Create a temporary directory for the test repository
	tmpDir, err := os.MkdirTemp("", "git-drs-e2e-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	// Print the temporary directory path for debugging
	t.Logf("Temporary directory: %s", tmpDir)

	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("Failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	// Create remote repo via API
	if err = createRemoteRepo(host, owner, repoName, token); err != nil {
		t.Fatalf("Failed to create remote repo: %v", err)
	}

	defer func() {
		if err := deleteRemoteRepo(host, owner, repoName, token); err != nil {
			t.Errorf("Failed to delete host repo %s/%s: %v", owner, repoName, err)
		}
		if repoExists, _ := checkRepoExists(host, owner, repoName, token); repoExists {
			t.Errorf("Remote repository %s/%s was not deleted", owner, repoName)
		}
	}()

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current dir: %v", err)
	}
	defer func() {
		if err = os.Chdir(oldDir); err != nil {
			t.Errorf("Failed to change back to original dir: %v", err)
		}
	}()
	if err = os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change dir %s: %v", tmpDir, err)
	}

	repoDir := filepath.Join(tmpDir, repoName)
	if err = os.Mkdir(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir %s: %v", repoDir, err)
	}
	if err = os.Chdir(repoDir); err != nil {
		t.Fatalf("Failed to change to repo dir %s: %v", repoDir, err)
	}

	cmd := exec.Command("git", "init")
	if err = cmd.Run(); err != nil {
		t.Fatalf("Failed to git init in %s: %v", repoDir, err)
	}

	cof := dcJWT.Configure{}
	cred, err := cof.ParseConfig(profile)
	if err != nil {
		t.Fatalf("Parse config: %v", err)
	}

	email, err := utils.ParseEmailFromToken(cred.AccessToken)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Running CMD: ", "calypr_admin",
		"collaborators",
		"add",
		email,
		"/programs/test/projects/"+project,
		"--project_id",
		repoName,
		"--profile",
		profile,
		"-w",
		"-a")

	cmd = exec.Command(
		"calypr_admin",
		"collaborators",
		"add",
		email,
		"/programs/test/projects/"+project,
		"--project_id",
		repoName,
		"--profile",
		profile,
		"-w",
		"-a",
	)

	var out []byte
	if out, err = cmd.Output(); err != nil {
		t.Fatalf("Failed to calypr_admin collaborators add %s: %s", repoName, out)
	}
	t.Logf("calypr_admin collaborators add: %s", string(out))

	cmd = exec.Command("git", "lfs", "install", "--skip-smudge")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git lfs install in %s: %v", repoDir, err)
	}

	cmd = exec.Command("git-drs", "init")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("git-drs init output: %s", output)
		t.Fatalf("Failed to git-drs init in %s: %v", repoDir, err)
	}
	t.Logf("git-drs add remote output: %s", output)

	cmd = exec.Command("git-drs", "remote", "add", "gen3", profile, "--project", repoName, "--bucket", DEFAULT_BUCKET)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Logf("git-drs add remote output: %s", output)
		t.Fatalf("Failed to git-drs add remote %s: %v", repoName, err)
	}
	t.Logf("git-drs add remote output: %s", output)

	// Verify .drs/config.yaml exists
	configPath := filepath.Join(".drs", "config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			t.Fatalf(".drs/config.yaml not created in %s", repoDir)
		}
		t.Fatalf("Failed to stat .drs/config.yaml in %s: %v", repoDir, err)
	}
	// Log config.yaml contents for debugging
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read .drs/config.yaml: %v", err)
	} else {
		t.Logf(".drs/config.yaml contents: %s", configContent)
	}

	/* Hooks not getting added right now. Uncomment this out in the future if we go back to this setup
	* // Verify git hooks are installed (pre-commit and pre-push)
	  hooksDir := ".git/hooks"
	  preCommitHook := filepath.Join(hooksDir, "pre-commit")
	  if _, err := os.Stat(preCommitHook); err != nil {
	          if os.IsNotExist(err) {
	                  t.Fatalf("pre-commit hook not installed in %s", hooksDir)
	          }
	          t.Fatalf("Failed to stat pre-commit hook in %s: %v", hooksDir, err)
	  }
	  prePushHook := filepath.Join(hooksDir, "pre-push")
	  if _, err := os.Stat(prePushHook); err != nil {
	          if os.IsNotExist(err) {
	                  t.Fatalf("pre-push hook not installed in %s", hooksDir)
	          }
	          t.Fatalf("Failed to stat pre-push hook in %s: %v", hooksDir, err)
	  }
	*/

	cmd = exec.Command("git", "lfs", "track", "*.txt")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git lfs track in %s: %v", repoDir, err)
	}

	cmd = exec.Command("git", "add", ".gitattributes")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git add .gitattributes in %s: %v", repoDir, err)
	}

	// Create a dummy data file
	dataFile := "data.txt"
	// Make the string random so that each new indexd record that is created only exists for this specific integration test
	if err := os.WriteFile(dataFile, []byte("Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."+generateRandomString(4)), 0644); err != nil {
		t.Fatalf("Failed to create data file %s: %v", dataFile, err)
	}

	// Configure Git to use PAT for HTTPS push
	cmd = exec.Command("git", "config", "credential.helper", fmt.Sprintf("!f() { echo username=x-oauth-basic; echo password=%s; }; f", token))
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to configure git credential helper in %s: %v", repoDir, err)
	}

	cmd = exec.Command("git", "branch", "-M", "main")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to add main branch: %v", err)
	}

	remoteURL := fmt.Sprintf("%s/%s/%s.git", host, owner, repoName)
	// Add remote
	t.Log("Remote URL: ", remoteURL)
	cmd = exec.Command("git", "remote", "add", "origin", remoteURL)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to add remote %s: %v", remoteURL, err)
	}

	cmd = exec.Command("git", "add", dataFile)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git add data file %s: %v", dataFile, err)
	}

	cmd = exec.Command("git", "commit", "-m", "Add test file")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Logf("Commit output: %s", output)
		t.Fatalf("Failed to git commit in %s: %v", repoDir, err)
	}

	// Verify LFS files are listed
	cmd = exec.Command("git", "lfs", "ls-files", "--json")
	output, err = cmd.Output()
	if err != nil {
		t.Fatalf("Failed to git lfs ls-files in %s: %v", repoDir, err)
	}
	if string(output) == "" {
		t.Fatalf("No LFS files listed after commit in %s", repoDir)
	}

	type File struct {
		Name       string `json:"name"`
		Size       int64  `json:"size"`
		Checkout   bool   `json:"checkout"`
		Downloaded bool   `json:"downloaded"`
		OIDType    string `json:"oid_type"`
		OID        string `json:"oid"`
		Version    string `json:"version"`
	}
	type FileContainer struct {
		Files []File `json:"files"`
	}

	t.Log("OUTPUT: ", string(output))
	var fileMap FileContainer
	err = json.Unmarshal(output, &fileMap)

	/*
		 * precommit hooks don't exist for now, so this isn't done anymore
			* TODO: discussion on wether it is necessary to add this back in.

		path, err := drsmap.GetObjectPath(projectdir.DRS_OBJS_PATH, fileMap.Files[0].OID)
		if err != nil {
			t.Fatalf("Failed to get object path %s: %v", path, err)
		}
		if path == "" {
			t.Fatalf("Expecting path but got %s instead", path)
		}
		t.Logf("Path: %s", path)

		_, err = os.Stat(path)

		if os.IsNotExist(err) {
			t.Fatalf("File or directory not found at path: %s", path)
		}
		if err != nil {
			t.Fatalf("Error checking path existence %s: %v", path, err)
			}
	*/

	// Verify pre-commit hook was called by checking .drs/ logs
	files, err := fs.ReadDir(os.DirFS(projectdir.DRS_DIR), ".")
	if err != nil {
		t.Fatalf("Failed to read .drs dir %s: %v", projectdir.DRS_DIR, err)
	}
	logFound := false
	for _, file := range files {
		if !file.IsDir() && len(file.Name()) > 0 && file.Name() != "config.yaml" {
			logFound = true
			logPath := filepath.Join(projectdir.DRS_DIR, file.Name())
			logContent, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("Failed to read log file %s: %v", logPath, err)
			} else {
				t.Logf("Log file %s contents: %s", logPath, logContent)
			}
		}
	}
	if !logFound {
		t.Fatalf("No logs found in .drs/ after commit in %s; pre-commit hook may not have run", projectdir.DRS_DIR)
	}

	cmd = exec.Command("git", "push", "--set-upstream", "origin", "main")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Expected push failure with dummy DRS server: %v\nOutput: %s", err, output)
	} else {
		t.Log("Push succeeded with dummy DRS server")
	}
	t.Log("OUTPUT: ", string(output))

	// Clean up the initial repository
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change back to tmp dir %s: %v", tmpDir, err)
	}
	if err := os.RemoveAll(repoDir); err != nil {
		t.Errorf("Failed to remove initial repo dir %s: %v", repoDir, err)
	}

	cloneDir, err := os.MkdirTemp("", "git-drs-clone-")
	if err != nil {
		t.Fatalf("Failed to create clone dir: %v", err)
	}
	t.Logf("Clone directory: %s", cloneDir)
	defer func() {
		if err := os.RemoveAll(cloneDir); err != nil {
			t.Errorf("Failed to remove clone dir %s: %v", cloneDir, err)
		}
	}()

	if err := os.Chdir(cloneDir); err != nil {
		t.Fatalf("Failed to change to clone dir %s: %v", cloneDir, err)
	}

	// Clone the repository
	cmd = exec.Command("git", "clone", remoteURL, "cloned-repo")
	cmd.Env = append(os.Environ(), fmt.Sprintf("GIT_ASKPASS=echo %s", token))
	if cmdOut, err := cmd.Output(); err != nil {
		t.Fatalf("Failed to git clone %s: %s", remoteURL, cmdOut)
	}

	// Change to cloned repo
	cloneRepoDir := filepath.Join(cloneDir, "cloned-repo")
	if err := os.Chdir(cloneRepoDir); err != nil {
		t.Fatalf("Failed to change to cloned repo dir %s: %v", cloneRepoDir, err)
	}

	cmd = exec.Command("git-drs", "init")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Logf("git-drs init (clone) output: %s", output)
		t.Fatalf("Failed to git-drs init in %s: %v", cloneRepoDir, err)
	}
	t.Logf("git-drs init (clone) output: %s", output)

	cmd = exec.Command("git-drs", "remote", "add", "gen3", profile, "--project", repoName, "--bucket", DEFAULT_BUCKET)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Logf("git-drs add remote output: %s", output)
		t.Fatalf("Failed to git-drs add remote %s: %v", repoName, err)
	}
	t.Logf("git-drs add remote output: %s", output)

	cmd = exec.Command("git", "lfs", "pull", "-I", dataFile)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Expected pull failure with dummy DRS server: %v\nOutput: %s", err, output)
	} else {
		t.Log("Pull succeeded with dummy DRS server")
	}

	// Verify data.txt exists (even if content fetch fails, pointer file should exist)
	if _, err := os.Stat(dataFile); err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("data.txt not found after git lfs pull in %s", cloneRepoDir)
		}
		t.Fatalf("Failed to stat data.txt in %s: %v", cloneRepoDir, err)
	}

	cmd = exec.Command("git", "lfs", "ls-files", "--json")
	output, err = cmd.Output()
	if err != nil {
		t.Fatalf("Failed to git lfs ls-files in %s: %v", cloneRepoDir, err)
	}

	err = json.Unmarshal(output, &fileMap)

	cmd = exec.Command("git-drs", "delete", "sha256", fileMap.Files[0].OID)
	output, err = cmd.Output()
	if err != nil {
		t.Fatalf("Failed to delete indexd record %s: %v", fileMap.Files[0].OID, err)
	}

	// Verify .gitattributes exists and contains the txt pattern
	gitAttributes, err := os.ReadFile(".gitattributes")
	if err != nil {
		t.Fatalf("Failed to read .gitattributes in %s: %v", cloneRepoDir, err)
	}
	if string(gitAttributes) != "*.txt filter=lfs diff=lfs merge=lfs -text\n" {
		t.Fatalf("Unexpected .gitattributes content in %s: %s", cloneRepoDir, gitAttributes)
	}

	if err := os.Chdir(cloneDir); err != nil {
		t.Fatalf("Failed to change back to clone dir %s: %v", cloneDir, err)
	}
	if err := os.RemoveAll(cloneRepoDir); err != nil {
		t.Errorf("Failed to remove cloned repo dir %s: %v", cloneRepoDir, err)
	}

}

// createRemoteRepo creates a new repo on GHE via API
func createRemoteRepo(host, owner, repoName, token string) error {
	url := fmt.Sprintf("%s/api/v3/orgs/%s/repos", host, owner)
	body := map[string]any{
		"name":        repoName,
		"description": "Test repo for git-drs e2e test",
		"private":     true,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal create repo request: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create repo HTTP request: %v", err)
	}
	req.Header.Set("Authorization", "token "+token)
	//req.Header.Set("Accept", "application/vnd.github+json")
	//req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send create repo request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create repo: %s %d %s", repoName, resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// deleteRemoteRepo deletes the repo on GHE via API
func deleteRemoteRepo(host, owner, repoName, token string) error {
	url := fmt.Sprintf("%s/api/v3/repos/%s/%s", host, owner, repoName)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send delete repo request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete repo: %d %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// checkRepoExists checks if the repository exists via API
func checkRepoExists(host, owner, repoName, token string) (bool, error) {
	url := fmt.Sprintf("%s/api/v3/repos/%s/%s", host, owner, repoName)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create HTTP request: %v", err)
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to send check repo request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	return false, fmt.Errorf("unexpected status code checking repo: %d %s", resp.StatusCode, string(bodyBytes))
}

// generateRandomString generates a truly random string for unique repo names
func generateRandomString(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	source := rand.NewSource(time.Now().UnixNano())
	r := rand.New(source)
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[r.Intn(len(chars))]
	}
	return string(result)
}
