package cloud

import "testing"

func TestValidateInputs(t *testing.T) {
	err := ValidateInputs("s3://bucket/path", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	if err != nil {
		t.Fatalf("ValidateInputs error: %v", err)
	}

	if err := ValidateInputs("http://bucket/path", "bad"); err == nil {
		t.Fatalf("expected error for invalid inputs")
	}
}
