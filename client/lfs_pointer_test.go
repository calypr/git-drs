package client

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseLFSPointerIsStrict(t *testing.T) {
	valid := fmt.Sprintf("%s\noid sha256:%064x\nsize 17\n", standardLFSPointerVersion, 42)
	file, ok, err := ParseLFSPointer([]byte(valid))
	if err != nil || !ok {
		t.Fatalf("parse valid pointer: ok=%t err=%v", ok, err)
	}
	if file.OID != fmt.Sprintf("sha256:%064x", 42) || file.Size != 17 {
		t.Fatalf("unexpected pointer: %#v", file)
	}
	for _, invalid := range []string{
		strings.Replace(valid, "size 17", "size -1", 1),
		strings.Replace(valid, "size 17", "size 017", 1),
		strings.Replace(valid, "size 17", "size 17\next-0-foo bar", 1),
		strings.Replace(valid, "\n", "\r\n", 1),
		strings.Replace(valid, fmt.Sprintf("%064x", 42), "not-a-hash", 1),
	} {
		if _, _, err := ParseLFSPointer([]byte(invalid)); err == nil {
			t.Fatalf("expected malformed pointer to be rejected: %q", invalid)
		}
	}
	if _, ok, err := ParseLFSPointer([]byte(`{"resourceType":"Patient"}`)); err != nil || ok {
		t.Fatalf("ordinary NDJSON should not be treated as a pointer: ok=%t err=%v", ok, err)
	}
}
