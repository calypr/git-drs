package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/calypr/git-drs/internal/globusauth"
)

const (
	sourceCollection = "6c54cade-bde5-45c1-bdea-f4bd71dba2cc"
	sourceRoot       = "/home/share/godata"
	sourceScope      = "https://auth.globus.org/scopes/6c54cade-bde5-45c1-bdea-f4bd71dba2cc/data_access"
)

func main() {
	destinationCollection := flag.String("destination-collection", "", "destination collection UUID")
	destinationPath := flag.String("destination-path", "", "collection path for staged tutorial files")
	localPath := flag.String("local-path", "", "local path matching destination-path")
	manifest := flag.String("manifest", "", "output TSV manifest")
	timeout := flag.Duration("timeout", 2*time.Minute, "maximum time to wait for the staging transfer")
	flag.Parse()
	if *destinationCollection == "" || *destinationPath == "" || *localPath == "" || *manifest == "" {
		fmt.Fprintln(os.Stderr, "destination-collection, destination-path, local-path, and manifest are required")
		os.Exit(2)
	}
	if err := run(context.Background(), *destinationCollection, *destinationPath, *localPath, *manifest, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, destinationCollection, destinationPath, localPath, manifest string, timeout time.Duration) error {
	client, err := globusauth.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	files, err := client.ListFiles(ctx, sourceCollection, sourceRoot)
	if err != nil {
		if strings.Contains(err.Error(), "ConsentRequired") {
			return fmt.Errorf("tutorial collection consent required; run `git drs auth globus login --scope %s`, then retry: %w", sourceScope, err)
		}
		return fmt.Errorf("list tutorial files: %w", err)
	}
	items := make([]globusauth.TransferItem, len(files))
	for i, file := range files {
		rel := strings.TrimPrefix(strings.TrimPrefix(file.Path, sourceRoot), "/")
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return fmt.Errorf("invalid tutorial path %q", file.Path)
		}
		items[i] = globusauth.TransferItem{SourcePath: file.Path, DestinationPath: path.Join(destinationPath, rel)}
	}
	taskID, err := client.SubmitTransferItems(ctx, sourceCollection, destinationCollection, items, "git-drs tutorial manifest")
	if err != nil {
		if strings.Contains(err.Error(), "PermissionDenied") || strings.Contains(err.Error(), "ConsentRequired") {
			return fmt.Errorf("destination collection authorization failed; verify that collection %s is active and %s is writable and allowed by Globus Connect Personal: %w", destinationCollection, destinationPath, err)
		}
		return err
	}
	fmt.Printf("staging tutorial files with Globus task %s\n", taskID)
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := client.WaitForTask(waitCtx, taskID, 2*time.Second); err != nil {
		return fmt.Errorf("staging task %s did not complete; inspect https://app.globus.org/activity/%s/overview: %w", taskID, taskID, err)
	}
	out, err := os.Create(manifest)
	if err != nil {
		return err
	}
	defer out.Close()
	w := csv.NewWriter(out)
	w.Comma = '\t'
	_ = w.Write([]string{"path", "size", "sha256"})
	for _, file := range files {
		rel := strings.TrimPrefix(strings.TrimPrefix(file.Path, sourceRoot), "/")
		f, err := os.Open(filepath.Join(localPath, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		_ = w.Write([]string{rel, fmt.Sprint(file.Size), fmt.Sprintf("%x", h.Sum(nil))})
	}
	w.Flush()
	return w.Error()
}
