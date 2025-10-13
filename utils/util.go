package utils

import (
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
)

func GitTopLevel() (string, error) {
	path, err := SimpleRun([]string{"git", "rev-parse", "--show-toplevel"})
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(path, "\n"), nil
}

func SimpleRun(cmds []string) (string, error) {
	exePath, err := exec.LookPath(cmds[0])
	if err != nil {
		return "", fmt.Errorf("command not found: %s: %w", cmds[0], err)
	}
	cmd := exec.Command(exePath, cmds[1:]...)
	cmdOut, err := cmd.Output()
	return string(cmdOut), err
}

func DrsTopLevel() (string, error) {
	base, err := GitTopLevel()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, DRS_DIR), nil
}

func CanDownloadFile(signedURL string) error {
	// Create an HTTP GET request
	resp, err := http.Get(signedURL)
	if err != nil {
		return fmt.Errorf("Error while sending the request: %v\n", err)
	}
	defer resp.Body.Close()

	// Check if the response status is 200 OK
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	return fmt.Errorf("failed to download file, HTTP Status: %d", resp.StatusCode)
}
