package lsfiles

import "testing"

func resetFlagsForTest() {
	gitRemote = ""
	drsRemote = ""
	includePatterns = nil
	showLong = false
	showAll = false
	pointers = false
	nameOnly = false
	jsonOutput = false
	drsStatus = false
}

func TestPointerInventorySelectionPreservesExistingModes(t *testing.T) {
	tests := []struct {
		name  string
		setup func()
	}{
		{name: "all tracked pointers", setup: func() { showAll = true }},
		{name: "explicit pointer mode", setup: func() { pointers = true }},
		{name: "DRS lookup", setup: func() { drsStatus = true }},
		{name: "full object IDs", setup: func() { showLong = true }},
		{name: "JSON output", setup: func() { jsonOutput = true }},
		{name: "include filter", setup: func() { includePatterns = []string{"*.bam"} }},
		{name: "name only", setup: func() { nameOnly = true }},
		{name: "Git remote", setup: func() { gitRemote = "origin" }},
		{name: "DRS remote", setup: func() { drsRemote = "production" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resetFlagsForTest()
			test.setup()
			if !pointerInventoryRequested() {
				t.Fatal("expected pointer inventory mode")
			}
		})
	}

	resetFlagsForTest()
	if pointerInventoryRequested() {
		t.Fatal("default invocation should browse the current directory")
	}
}

func TestLSFlagShorthandsKeepHelpAvailable(t *testing.T) {
	if flag := Cmd.Flags().Lookup("long"); flag == nil {
		t.Fatal("--long flag is missing")
	} else if flag.Shorthand != "l" {
		t.Fatalf("--long shorthand = %q, want l", flag.Shorthand)
	}
	if flag := Cmd.Flags().Lookup("all"); flag == nil {
		t.Fatal("--all flag is missing")
	} else if flag.Shorthand != "a" {
		t.Fatalf("--all shorthand = %q, want a", flag.Shorthand)
	}
	Cmd.InitDefaultHelpFlag()
	if flag := Cmd.Flags().Lookup("help"); flag == nil {
		t.Fatal("--help flag is missing")
	} else if flag.Shorthand != "h" {
		t.Fatalf("--help shorthand = %q, want h", flag.Shorthand)
	}
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
