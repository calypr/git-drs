//go:build integration

package pull

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func rewriteTDRSignedURLForEmulator(response *http.Response, emulatorEndpoint string) error {
	if response == nil || response.Body == nil {
		return fmt.Errorf("TDR response has no body")
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read TDR response: %w", err)
	}
	_ = response.Body.Close()
	if !bytes.Contains(body, []byte("X-Goog-Signature=")) {
		return fmt.Errorf("TDR access response is missing a V4 signed URL")
	}
	const sourceAuthority = "http://127.0.0.1/"
	if !bytes.Contains(body, []byte(sourceAuthority)) {
		return fmt.Errorf("TDR signed URL does not use the expected local storage host")
	}
	if !strings.HasPrefix(emulatorEndpoint, "http://127.0.0.1:") {
		return fmt.Errorf("invalid local GCS emulator endpoint %q", emulatorEndpoint)
	}
	body = bytes.Replace(body, []byte(sourceAuthority), []byte(strings.TrimRight(emulatorEndpoint, "/")+"/"), 1)
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.Header.Set("Content-Length", fmt.Sprint(len(body)))
	return nil
}
