package confirm

import (
	"strings"
	"testing"
)

func TestPromptCaseMatching(t *testing.T) {
	tests := []struct {
		name          string
		response      string
		expected      string
		caseSensitive bool
		wantErr       bool
	}{
		{name: "case insensitive", response: "YeS\n", expected: "yes"},
		{name: "case sensitive match", response: "project-id\n", expected: "project-id", caseSensitive: true},
		{name: "case sensitive mismatch", response: "PROJECT-ID\n", expected: "project-id", caseSensitive: true, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output strings.Builder
			err := Prompt(&output, strings.NewReader(test.response), "Confirm", test.expected, test.caseSensitive)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected confirmation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Prompt returned error: %v", err)
			}
			if got, want := output.String(), "Confirm: "; got != want {
				t.Fatalf("unexpected prompt output %q, want %q", got, want)
			}
		})
	}
}

func TestDisplayHelpers(t *testing.T) {
	var output strings.Builder
	if err := WarningHeader(&output, "DELETE a record"); err != nil {
		t.Fatalf("WarningHeader returned error: %v", err)
	}
	if err := Field(&output, "Project", "project-id"); err != nil {
		t.Fatalf("Field returned error: %v", err)
	}
	if err := Footer(&output); err != nil {
		t.Fatalf("Footer returned error: %v", err)
	}

	const want = "\nWARNING: You are about to DELETE a record\n\nProject:    project-id\n\nThis action CANNOT be undone.\n\n"
	if got := output.String(); got != want {
		t.Fatalf("unexpected display output %q, want %q", got, want)
	}
}
