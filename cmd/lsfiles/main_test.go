package lsfiles

import "testing"

func resetFlagsForTest() {
	gitRemote = ""
	drsRemote = ""
	includePatterns = nil
	showLong = false
	nameOnly = false
	jsonOutput = false
	drsStatus = false
}

func TestValidateOutputFlags(t *testing.T) {
	resetFlagsForTest()

	nameOnly = true
	jsonOutput = true
	if err := validateOutputFlags(); err == nil {
		t.Fatal("expected name-only/json conflict")
	}

	resetFlagsForTest()
	nameOnly = true
	showLong = true
	if err := validateOutputFlags(); err == nil {
		t.Fatal("expected long/name-only conflict")
	}
}
