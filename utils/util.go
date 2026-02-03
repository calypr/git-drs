package utils

import (
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

func SimpleRun(cmds []string) (string, error) {
	exePath, err := exec.LookPath(cmds[0])
	if err != nil {
		return "", fmt.Errorf("command not found: %s: %w", cmds[0], err)
	}
	cmd := exec.Command(exePath, cmds[1:]...)
	cmdOut, err := cmd.Output()
	return string(cmdOut), err
}

// CanDownloadFile checks if a file can be downloaded from the given signed URL
// by issuing a ranged GET for a single byte to mimic HEAD behavior.
func CanDownloadFile(signedURL string) error {
	req, err := http.NewRequest("GET", signedURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error while sending the request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPartialContent || resp.StatusCode == http.StatusOK {
		return nil
	}

	return fmt.Errorf("failed to access file, HTTP status: %d", resp.StatusCode)
}

func ParseEmailFromToken(tokenString string) (string, error) {
	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(tokenString, &claims)
	if err != nil {
		return "", fmt.Errorf("failed to decode token in ParseEmailFromToken: '%s': %w", tokenString, err)
	}
	context, ok := claims["context"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'context' claim structure")
	}
	user, ok := context["user"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'context.user' claim structure")
	}
	name, ok := user["name"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'context.user.name' claim")
	}
	return name, nil
}

func ParseAPIEndpointFromToken(tokenString string) (string, error) {
	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(tokenString, &claims)
	if err != nil {
		return "", fmt.Errorf("failed to decode token in ParseAPIEndpointFromToken: '%s': %w", tokenString, err)
	}
	issUrl, ok := claims["iss"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'iss' claim")
	}
	parsedURL, err := url.Parse(issUrl)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host), nil
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

// IsValidSHA256 checks if a string is a valid SHA-256 hash
func IsValidSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
