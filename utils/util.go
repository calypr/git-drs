package utils

import (
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/golang-jwt/jwt/v4"
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
func ParseEmailFromToken(tokenString string) (string, error) {
	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(tokenString, &claims)
	if err != nil {
		return "", fmt.Errorf("failed to decode token: %w", err)
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
		return "", fmt.Errorf("failed to decode token: %w", err)
	}
	issUrl, ok := claims["iss"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'context' claim structure")
	}
	parsedURL, err := url.Parse(issUrl)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host), nil
}
