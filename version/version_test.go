package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	// Set vars for consistent testing
	GitCommit = "chk123"
	GitBranch = "test-branch"
	GitUpstream = "origin/test"
	BuildDate = "2023-01-01"
	Version = "v0.0.1"

	s := String()
	if !strings.Contains(s, "git commit: chk123") {
		t.Errorf("String() missing commit info")
	}
	if !strings.Contains(s, "git branch: test-branch") {
		t.Errorf("String() missing branch info")
	}
	if !strings.Contains(s, "version: v0.0.1") {
		t.Errorf("String() missing version info")
	}
}

func TestLogFields(t *testing.T) {
	// Ensure vars are set
	GitCommit = "chk123"
	Version = "v0.0.1"

	fields := LogFields()
	if len(fields) != 10 {
		t.Errorf("LogFields returned wrong number of fields: %d", len(fields))
	}

	fieldsMap := make(map[string]interface{})
	for i := 0; i < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok {
			t.Errorf("LogField key at index %d is not a string", i)
			continue
		}
		fieldsMap[key] = fields[i+1]
	}

	if val, ok := fieldsMap["GitCommit"]; !ok || val != "chk123" {
		t.Errorf("LogFields missing correct GitCommit")
	}
	if val, ok := fieldsMap["Version"]; !ok || val != "v0.0.1" {
		t.Errorf("LogFields missing correct Version")
	}
}
