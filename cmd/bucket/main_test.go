package bucket

import "testing"

func TestBucketFromStoragePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{
			name: "s3",
			path: "s3://data-bucket/program/project",
			want: "data-bucket",
		},
		{
			name: "gcs",
			path: "gs://data-bucket/program/project",
			want: "data-bucket",
		},
		{
			name: "azure",
			path: "azblob://data-bucket/program/project",
			want: "data-bucket",
		},
		{
			name:    "missing scheme",
			path:    "data-bucket/program/project",
			wantErr: true,
		},
		{
			name:    "missing bucket",
			path:    "s3:///program/project",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bucketFromStoragePath(tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("bucketFromStoragePath(%q) returned nil error", tc.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("bucketFromStoragePath(%q) returned error: %v", tc.path, err)
			}
			if got != tc.want {
				t.Fatalf("bucketFromStoragePath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestNormalizeStoragePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		bucket  string
		want    string
		wantErr bool
	}{
		{
			name:   "root bucket path",
			path:   "s3://data-bucket",
			bucket: "data-bucket",
			want:   "",
		},
		{
			name:   "program path",
			path:   "s3://data-bucket/program-root",
			bucket: "data-bucket",
			want:   "program-root",
		},
		{
			name:   "project path",
			path:   "s3://data-bucket/program-root/project-subpath",
			bucket: "data-bucket",
			want:   "program-root/project-subpath",
		},
		{
			name:   "gcs path",
			path:   "gs://data-bucket/program-root/project-subpath",
			bucket: "data-bucket",
			want:   "program-root/project-subpath",
		},
		{
			name:   "azure path",
			path:   "azblob://data-bucket/program-root/project-subpath",
			bucket: "data-bucket",
			want:   "program-root/project-subpath",
		},
		{
			name:    "bucket mismatch",
			path:    "s3://other-bucket/program-root",
			bucket:  "data-bucket",
			wantErr: true,
		},
		{
			name:    "unsupported scheme",
			path:    "https://data-bucket/program-root",
			bucket:  "data-bucket",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeStoragePath(tc.path, tc.bucket)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeStoragePath(%q, %q) returned nil error", tc.path, tc.bucket)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeStoragePath(%q, %q) returned error: %v", tc.path, tc.bucket, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeStoragePath(%q, %q) = %q, want %q", tc.path, tc.bucket, got, tc.want)
			}
		})
	}
}
