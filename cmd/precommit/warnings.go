package precommit

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type OversizedStagedFile struct {
	Path string
	Size int64
}

func collectOversizedPlainGitStagedFiles(ctx context.Context, changes []Change, thresholdBytes int64) ([]OversizedStagedFile, error) {
	if thresholdBytes <= 0 {
		return nil, nil
	}
	var oversized []OversizedStagedFile
	seen := make(map[string]struct{})
	for _, ch := range changes {
		if ch.Kind != KindAdd && ch.Kind != KindModify && ch.Kind != KindRename {
			continue
		}
		path := ch.NewPath
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}

		_, isLFS, err := stagedLFSOID(ctx, path)
		if err != nil {
			continue
		}
		if isLFS {
			continue
		}

		size, err := stagedBlobSize(ctx, path)
		if err != nil {
			return nil, err
		}
		if size <= thresholdBytes {
			continue
		}
		oversized = append(oversized, OversizedStagedFile{Path: path, Size: size})
	}
	sort.Slice(oversized, func(i, j int) bool { return oversized[i].Path < oversized[j].Path })
	return oversized, nil
}

func promptOversizedDirectGitCommit(files []OversizedStagedFile) (bool, error) {
	if len(files) == 0 {
		return true, nil
	}

	fmt.Fprintf(os.Stderr, "\nWarning: the following staged files are being committed directly to Git and exceed %s:\n\n", humanBytes(directCommitWarningThresholdBytes))
	for _, f := range files {
		fmt.Fprintf(os.Stderr, "  - %s (%s)\n", f.Path, humanBytes(f.Size))
	}
	fmt.Fprintln(os.Stderr, "\nIf these should be managed by git-drs, track them first and re-add them.")
	fmt.Fprint(os.Stderr, "Continue committing these files directly to GitHub? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func humanBytes(n int64) string {
	const unit = int64(1024)
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := unit, 0
	for q := n / unit; q >= unit; q /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
