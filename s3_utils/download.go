package s3_utils

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// downloads a file to a specified path using a signed URL
func DownloadSignedUrl(signedURL string, dstPath string) error {
	return DownloadSignedUrlWithProgress(signedURL, dstPath, nil)
}

// DownloadSignedUrlWithProgress downloads a file using a signed URL and reports bytes transferred.
func DownloadSignedUrlWithProgress(signedURL string, dstPath string, reportBytes func(int64)) error {
	// Download the file using the signed URL
	fileResponse, err := http.Get(signedURL)
	if err != nil {
		return err
	}
	defer fileResponse.Body.Close()

	// Check if the response status is OK
	if fileResponse.StatusCode != http.StatusOK {
		body, err := io.ReadAll(fileResponse.Body)
		if err != nil {
			return fmt.Errorf("failed to download file using signed URL: %s", fileResponse.Status)
		}
		return fmt.Errorf("failed to download file using signed URL: %s. Full error: %s", fileResponse.Status, string(body))
	}

	// Create the destination directory if it doesn't exist
	err = os.MkdirAll(filepath.Dir(dstPath), os.ModePerm)
	if err != nil {
		return err
	}

	// Create the destination file
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	buffer := make([]byte, 32*1024)
	for {
		n, readErr := fileResponse.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := dstFile.Write(buffer[:n]); writeErr != nil {
				return writeErr
			}
			if reportBytes != nil {
				reportBytes(int64(n))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	return nil
}
