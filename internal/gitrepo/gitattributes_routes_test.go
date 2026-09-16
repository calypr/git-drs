package gitrepo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpsertDRSRouteLinesAddNew(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gitattributes")

	changed, err := UpsertDRSRouteLines(p, "rw", []string{"scratch/**"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}

	b, _ := os.ReadFile(p)
	got := string(b)
	want := "scratch/** drs=rw\n"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestUpsertDRSRouteLinesUpdateExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gitattributes")

	if err := os.WriteFile(p, []byte("# hi\nscratch/** drs=ro\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := UpsertDRSRouteLines(p, "rw", []string{"scratch/**"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true")
	}

	b, _ := os.ReadFile(p)
	got := string(b)
	want := "# hi\nscratch/** drs=rw\n"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestUpsertDRSRouteLinesIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".gitattributes")

	if err := os.WriteFile(p, []byte("scratch/** drs=rw\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changed, err := UpsertDRSRouteLines(p, "rw", []string{"scratch/**"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if changed {
		t.Fatalf("expected changed=false")
	}
}

func TestParseRouteLine(t *testing.T) {
	p, m, ok := parseRouteLine("scratch/** drs=rw")
	if !ok || p != "scratch/**" || m != "rw" {
		t.Fatalf("unexpected: ok=%v p=%q m=%q", ok, p, m)
	}
}
