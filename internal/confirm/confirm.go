package confirm

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// PromptForConfirmation displays a prompt and reads user input to confirm an operation.
// Returns nil if the response matches expectedResponse, error otherwise.
// If caseSensitive is false, comparison is case-insensitive.
func PromptForConfirmation(w io.Writer, prompt string, expectedResponse string, caseSensitive bool) error {
	fmt.Fprintf(w, "%s: ", prompt)

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
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

func DisplayWarningHeader(w io.Writer, operation string) {
	fmt.Fprintf(w, "\n⚠️  WARNING: You are about to %s\n\n", operation)
}

func DisplayField(w io.Writer, key, value string) {
	fmt.Fprintf(w, "%-11s %s\n", key+":", value)
}

func DisplayFooter(w io.Writer) {
	fmt.Fprintf(w, "\nThis action CANNOT be undone.\n\n")
}
