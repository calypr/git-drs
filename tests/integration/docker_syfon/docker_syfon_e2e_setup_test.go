//go:build integration

package dockersyfon

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	dockerE2EOrganization      = "syfon"
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
var gitDrsTestHomeDir string

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

	root, err := findGitDrsRoot()
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("could not find git-drs root: %v\n", err))
		os.Exit(2)
	}

	gitDrsBinDir, err = os.MkdirTemp("", "git-drs-docker-e2e-bin-")
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("could not create temp binary dir: %v\n", err))
		os.Exit(2)
	}
	os.Stderr.WriteString(fmt.Sprintf("building git-drs integration binary into %s\n", gitDrsBinDir))

	gitDrsTestHomeDir, err = os.MkdirTemp("", "git-drs-docker-e2e-home-")
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("could not create temp home dir: %v\n", err))
		os.Exit(2)
	}
	_ = os.MkdirAll(filepath.Join(gitDrsTestHomeDir, ".config"), 0o755)
	_ = os.Setenv("HOME", gitDrsTestHomeDir)
	_ = os.Setenv("XDG_CONFIG_HOME", filepath.Join(gitDrsTestHomeDir, ".config"))
	_ = os.Setenv("GIT_CONFIG_NOSYSTEM", "1")

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
	_ = os.RemoveAll(gitDrsTestHomeDir)
	os.Exit(code)
}

func findGitDrsRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		goMod := filepath.Join(dir, "go.mod")
		data, readErr := os.ReadFile(goMod)
		if readErr == nil && strings.Contains(string(data), "module github.com/calypr/git-drs") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find github.com/calypr/git-drs go.mod from %s", dir)
		}
		dir = parent
	}
}
