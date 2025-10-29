package utils

import (
	"bytes"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
)

func GitTopLevel() (string, error) {
	path, err := SimpleRun([]string{"git", "rev-parse", "--show-toplevel"})
	path = strings.TrimSuffix(path, "\n")
	return path, err
}

func SimpleRun(cmds []string) (string, error) {
	exePath, err := exec.LookPath(cmds[0])
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	cmd := exec.Command(exePath, cmds[1:]...)
	cmd.Stdout = buf
	err = cmd.Run()
	return buf.String(), err
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

func ParseS3URL(s3url string) (string, string, error) {
	s3Prefix := "s3://"
	if !strings.HasPrefix(s3url, s3Prefix) {
		return "", "", fmt.Errorf("S3 URL requires prefix 's3://': %s", s3url)
	}
	trimmed := strings.TrimPrefix(s3url, s3Prefix)
	slashIndex := strings.Index(trimmed, "/")
	if slashIndex == -1 || slashIndex == len(trimmed)-1 {
		return "", "", fmt.Errorf("invalid S3 file URL: %s", s3url)
	}
	return trimmed[:slashIndex], trimmed[slashIndex+1:], nil
}
