package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/google/uuid"
)

type fixture struct {
	name      string
	https     bool
	globus    bool
	brokenURL bool
}

func main() {
	repo := flag.String("repo", "", "target Git repository")
	fixtures := flag.String("fixtures", "", "standalone fixture directory")
	sourceCollection := flag.String("source-collection", "", "source collection UUID")
	sourcePath := flag.String("source-path", "/standalone", "source collection path")
	httpsBase := flag.String("https-base", "", "public HTTPS base URL")
	flag.Parse()
	if *repo == "" || *fixtures == "" || *sourceCollection == "" || *httpsBase == "" {
		fatalf("repo, fixtures, source-collection, and https-base are required")
	}
	fixtureRoot, err := filepath.Abs(*fixtures)
	if err != nil {
		fatalf("resolve fixture directory: %v", err)
	}
	if err := os.Chdir(*repo); err != nil {
		fatalf("open target repository: %v", err)
	}

	items := []fixture{
		{name: "globus-only.bin", globus: true},
		{name: "https-globus.bin", https: true, globus: true},
		{name: "https-only.bin", https: true},
		{name: "broken-globus.bin", https: true, globus: true, brokenURL: true},
	}
	for _, item := range items {
		if err := materialize(fixtureRoot, *sourceCollection, *sourcePath, *httpsBase, item); err != nil {
			fatalf("%s: %v", item.name, err)
		}
	}
}

func materialize(fixtures, collection, sourceRoot, httpsBase string, item fixture) error {
	data, err := os.ReadFile(filepath.Join(fixtures, item.name))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	oid := hex.EncodeToString(sum[:])
	name := item.name
	methods := []drsapi.AccessMethod{}
	addMethod := func(typ drsapi.AccessMethodType, rawURL string) {
		methods = append(methods, drsapi.AccessMethod{Type: typ, AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: rawURL}})
	}
	if item.https {
		addMethod(drsapi.AccessMethodTypeHttps, strings.TrimRight(httpsBase, "/")+"/"+item.name)
	}
	if item.globus {
		memberPath := path.Join(sourceRoot, item.name)
		if item.brokenURL {
			memberPath = path.Join(sourceRoot, "intentionally-unreadable", item.name)
		}
		addMethod(drsapi.AccessMethodType("globus"), "globus://"+collection+memberPath)
	}
	id := uuid.NewSHA1(drsobject.UUIDNamespace, []byte("globus-integration:"+oid)).String()
	obj := &drsapi.DrsObject{
		Id:            id,
		SelfUri:       "drs://" + id,
		Name:          &name,
		Size:          int64(len(data)),
		Checksums:     []drsapi.Checksum{{Type: "sha256", Checksum: oid}},
		AccessMethods: &methods,
	}
	if err := drsobject.WriteObject(gitrepo.DRSObjectsPath, obj, oid); err != nil {
		return err
	}
	pointer := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n", oid, len(data))
	if err := os.WriteFile(name, []byte(pointer), 0o644); err != nil {
		return err
	}
	if _, err := gitrepo.TrackReadOnly(context.Background(), name); err != nil {
		return err
	}
	fmt.Printf("%s\t%d\t%s\n", name, len(data), oid)
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
