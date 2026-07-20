package filter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func TestCleanContentPassesThroughExistingPointer(t *testing.T) {
	repo := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(orig)
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	lfsRoot := filepath.Join(repo, ".git", "lfs")
	oid := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\nsize 21\n"
	explicitURL := "s3://bucket/path/to/file.bin"
	if err := drsobject.WriteObject(gitrepo.DRSObjectsPath, &drsapi.DrsObject{
		Size: 21,
		AccessMethods: &[]drsapi.AccessMethod{{
			Type: drsapi.AccessMethodTypeS3,
			AccessUrl: &struct {
				Headers *[]string `json:"headers,omitempty"`
				Url     string    `json:"url"`
			}{Url: explicitURL},
		}},
	}, oid); err != nil {
		t.Fatalf("seed DRS object: %v", err)
	}

	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := CleanContent(context.Background(), lfsRoot, "data/from-bucket.bin", bytes.NewBufferString(pointer), &out, logger); err != nil {
		t.Fatalf("CleanContent returned error: %v", err)
	}
	if out.String() != pointer {
		t.Fatalf("expected pointer passthrough, got %q", out.String())
	}

	gotObj, err := drsobject.ReadObject(gitrepo.DRSObjectsPath, oid)
	if err != nil {
		t.Fatalf("read DRS map entry: %v", err)
	}
	if gotObj.AccessMethods == nil || len(*gotObj.AccessMethods) != 1 || (*gotObj.AccessMethods)[0].AccessUrl.Url != explicitURL {
		t.Fatalf("expected explicit access URL to survive clean, got %+v", gotObj.AccessMethods)
	}

	sum := sha256.Sum256([]byte(pointer))
	contentOID := hex.EncodeToString(sum[:])
	if cachePath, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, contentOID); err == nil {
		if _, statErr := os.Stat(cachePath); !os.IsNotExist(statErr) {
			t.Fatalf("did not expect pointer text to be cached as payload at %s", cachePath)
		}
	}
}

func TestCleanContentPassesThroughDRSURIWithoutSHA256MapWarning(t *testing.T) {
	repo := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(orig)
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	const pointer = "version https://calypr.github.io/spec/v1\n" +
		"oid drs://drs.anv0:v2_e68887be-c583-375a-a773-48771192c8fa\n" +
		"size 200184\n"
	var out bytes.Buffer
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	if err := CleanContent(context.Background(), filepath.Join(repo, ".git", "lfs"), "population_descriptor.tsv", bytes.NewBufferString(pointer), &out, logger); err != nil {
		t.Fatalf("CleanContent returned error: %v", err)
	}
	if out.String() != pointer {
		t.Fatalf("expected DRS pointer passthrough, got %q", out.String())
	}
	if strings.Contains(logs.String(), "failed to write DRS map entry") {
		t.Fatalf("DRS URI pointer must not be written to the SHA256 map: %s", logs.String())
	}
}
