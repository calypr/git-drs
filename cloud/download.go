package cloud

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// downloads a file to a specified path using a signed URL
func DownloadSignedUrl(signedURL string, dstPath string) error {
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

	// Write the file content to the destination file
	_, err = io.Copy(dstFile, fileResponse.Body)
	if err != nil {
		return err
	}

	return nil
}
