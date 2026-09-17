package confirm

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func Prompt(w io.Writer, r io.Reader, prompt, expectedResponse string, caseSensitive bool) error {
	if _, err := fmt.Fprintf(w, "%s: ", prompt); err != nil {
		return err
	}

	response, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		return fmt.Errorf("error reading confirmation: %v", err)
	}

	response = strings.TrimSpace(response)
	if !caseSensitive {
		response = strings.ToLower(response)
		expectedResponse = strings.ToLower(expectedResponse)
	}

	if response != expectedResponse {
		return fmt.Errorf("operation cancelled: confirmation did not match")
	}

	return nil
}

func WarningHeader(w io.Writer, operation string) error {
	_, err := fmt.Fprintf(w, "\nWARNING: You are about to %s\n\n", operation)
	return err
}

func Field(w io.Writer, key, value string) error {
	_, err := fmt.Fprintf(w, "%-11s %s\n", key+":", value)
	return err
}

func Footer(w io.Writer) error {
	_, err := fmt.Fprintf(w, "\nThis action CANNOT be undone.\n\n")
	return err
}
