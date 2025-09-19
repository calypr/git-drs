package lfs

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type releaseInfo struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// getLatestVersion fetches the latest tag (e.g. v3.7.0)
func getLatestVersion() (string, error) {
	resp, err := http.Get("https://api.github.com/repos/git-lfs/git-lfs/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var rel releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", errors.New("no tag_name found in release info")
	}
	return rel.TagName, nil
}

// getAssetURLs returns the tarball and checksums asset URLs for the latest release and platform
func getAssetURLs(version string) (tarURL, tarName, checksumsURL, checksumsName string, err error) {
	arch := runtime.GOARCH
	osName := runtime.GOOS

	// Map Go arch/os to git-lfs asset naming
	var archStr, osStr string
	switch arch {
	case "amd64":
		archStr = "amd64"
	case "arm64":
		archStr = "arm64"
	default:
		return "", "", "", "", fmt.Errorf("unsupported arch: %s", arch)
	}

	switch osName {
	case "linux":
		osStr = "linux"
	case "darwin":
		osStr = "darwin"
	default:
		return "", "", "", "", fmt.Errorf("unsupported OS: %s", osName)
	}

	ver := strings.TrimPrefix(version, "v")
	tarName = fmt.Sprintf("git-lfs-%s-%s-v%s.tar.gz", osStr, archStr, ver)
	checksumsName = fmt.Sprintf("git-lfs-v%s-checksums.txt", ver)
	baseURL := fmt.Sprintf("https://github.com/git-lfs/git-lfs/releases/download/%s", version)
	tarURL = fmt.Sprintf("%s/%s", baseURL, tarName)
	checksumsURL = fmt.Sprintf("%s/%s", baseURL, checksumsName)
	return
}

// downloadFile downloads a file from url to dest
func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

// verifyChecksum checks the downloaded tarball against the checksums file
func verifyChecksum(tarPath, checksumsPath, tarName string) error {
	// Read the expected checksum from the checksums file
	var expected string
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, tarName) {
			expected = strings.Fields(line)[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("could not find checksum for %s in %s", tarName, checksumsPath)
	}
	// Calculate actual checksum
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

// extractTarGz extracts the git-lfs binary from the tarball
func extractTarGz(tarGzPath, destDir string) (string, error) {
	f, err := os.Open(tarGzPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	var lfsBinary string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		// The binary is named "git-lfs"
		if filepath.Base(hdr.Name) == "git-lfs" && hdr.Typeflag == tar.TypeReg {
			lfsBinary = filepath.Join(destDir, "git-lfs")
			out, err := os.Create(lfsBinary)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return "", err
			}
			out.Close()
			if err := os.Chmod(lfsBinary, 0755); err != nil {
				return "", err
			}
		}
	}
	if lfsBinary == "" {
		return "", errors.New("git-lfs binary not found in tarball")
	}
	return lfsBinary, nil
}

// InstallLatest downloads, verifies, and installs the latest git-lfs to the given directory
func InstallLatest(targetDir string) error {
	version, err := getLatestVersion()
	if err != nil {
		return err
	}
	tarURL, tarName, checksumsURL, checksumsName, err := getAssetURLs(version)
	if err != nil {
		return err
	}

	// Download files
	tmpTar := filepath.Join(os.TempDir(), tarName)
	tmpChecksums := filepath.Join(os.TempDir(), checksumsName)
	defer os.Remove(tmpTar)
	defer os.Remove(tmpChecksums)
	fmt.Println("Downloading:", tarURL)
	if err := downloadFile(tarURL, tmpTar); err != nil {
		return err
	}
	fmt.Println("Downloading:", checksumsURL)
	if err := downloadFile(checksumsURL, tmpChecksums); err != nil {
		return err
	}
	// Verify checksum
	fmt.Println("Verifying checksum...")
	if err := verifyChecksum(tmpTar, tmpChecksums, tarName); err != nil {
		return err
	}
	// Extract binary
	fmt.Println("Extracting to:", targetDir)
	binary, err := extractTarGz(tmpTar, targetDir)
	if err != nil {
		return err
	}
	fmt.Println("git-lfs installed at:", binary)
	return nil
}

func RunGitLFSInstall() error {
	cmd := exec.Command("git-lfs", "install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
