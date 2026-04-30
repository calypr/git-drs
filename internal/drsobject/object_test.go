package drsobject

import "testing"

func TestBuilderDoesNotSynthesizeGen3StoragePrefix(t *testing.T) {
	obj, err := BuildWithOptions("file.txt", "abc123", 10, "drs-1", LocationOptions{
		Bucket:       "bucket",
		Organization: "org",
		Project:      "proj",
	})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	if obj.AccessMethods == nil || len(*obj.AccessMethods) != 1 {
		t.Fatalf("expected access method, got %+v", obj.AccessMethods)
	}
	if got := (*obj.AccessMethods)[0].AccessUrl.Url; got != "s3://bucket/abc123" {
		t.Fatalf("unexpected access url: %q", got)
	}

	obj, err = BuildWithOptions("file.txt", "def456", 10, "drs-2", LocationOptions{
		Bucket:        "bucket",
		Organization:  "org",
		Project:       "proj",
		StoragePrefix: "program-root/project-subpath",
	})
	if err != nil {
		t.Fatalf("BuildWithOptions with prefix returned error: %v", err)
	}
	if got := (*obj.AccessMethods)[0].AccessUrl.Url; got != "s3://bucket/program-root/project-subpath/def456" {
		t.Fatalf("unexpected prefixed access url: %q", got)
	}
}
