package install

import (
	"reflect"
	"testing"
)

func TestInstallCommandRejectsArgs(t *testing.T) {
	cmd := NewCommand()
	cmd.SetArgs([]string{"extra"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error when passing extra args")
	}
}

func TestInstallGlobalFilterConfig(t *testing.T) {
	var calls [][]string
	runner := func(args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}

	err := installGlobalFilterConfig(runner)
	if err != nil {
		t.Fatalf("installGlobalFilterConfig returned error: %v", err)
	}

	want := [][]string{
		{"config", "--global", "filter.drs.clean", "git-drs clean -- %f"},
		{"config", "--global", "filter.drs.smudge", "git-drs smudge -- %f"},
		{"config", "--global", "filter.drs.process", "git-drs filter-process"},
		{"config", "--global", "filter.drs.required", "true"},
	}

	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected git config calls\nwant: %#v\ngot:  %#v", want, calls)
	}
}

func TestInstallGlobalFilterConfigRunnerError(t *testing.T) {
	runner := func(args ...string) error {
		return assertErr{}
	}

	err := installGlobalFilterConfig(runner)
	if err == nil {
		t.Fatalf("expected error when runner fails")
	}
}

type assertErr struct{}

func (e assertErr) Error() string {
	return "runner failed"
}
