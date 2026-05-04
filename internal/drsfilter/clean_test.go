package drsfilter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/drsobject"
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
	if err := drsobject.WriteObject(common.DRS_OBJS_PATH, &drsapi.DrsObject{
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

	if objPath, err := lfs.ObjectPath(common.DRS_OBJS_PATH, oid); err != nil {
		t.Fatalf("ObjectPath: %v", err)
	} else if _, err := os.Stat(objPath); err != nil {
		t.Fatalf("expected DRS map entry at %s: %v", objPath, err)
	}
	gotObj, err := drsobject.ReadObject(common.DRS_OBJS_PATH, oid)
	if err != nil {
		t.Fatalf("read DRS map entry: %v", err)
	}
	if gotObj.AccessMethods == nil || len(*gotObj.AccessMethods) != 1 || (*gotObj.AccessMethods)[0].AccessUrl.Url != explicitURL {
		t.Fatalf("expected explicit access URL to survive clean, got %+v", gotObj.AccessMethods)
	}

	sum := sha256.Sum256([]byte(pointer))
	contentOID := hex.EncodeToString(sum[:])
	if cachePath, err := lfs.ObjectPath(common.LFS_OBJS_PATH, contentOID); err == nil {
		if _, statErr := os.Stat(cachePath); !os.IsNotExist(statErr) {
			t.Fatalf("did not expect pointer text to be cached as payload at %s", cachePath)
		}
	}
}
