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

func TestShortOIDShowsDRSObjectPortion(t *testing.T) {
	for _, test := range []struct {
		name string
		oid  string
		want string
	}{
		{name: "legacy DRS pointer", oid: "//drs.anv0:v2_e68887be-c583", want: "drs.anv0:v2_e68887be"},
		{name: "canonical DRS URI", oid: "drs://drs.anv0:v2_91ab42cd-1234", want: "drs.anv0:v2_91ab42cd"},
		{name: "path DRS pointer", oid: "//example.org/object-123456789", want: "example.org/object-1234"},
		{name: "sha256", oid: "0123456789abcdef", want: "0123456789"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shortOID(test.oid); got != test.want {
				t.Fatalf("shortOID(%q) = %q, want %q", test.oid, got, test.want)
			}
		})
	}
}
