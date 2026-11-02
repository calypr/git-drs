package lfs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// PointerInventoryForObjects returns every LFS/DRS pointer blob reachable from
// targets but not reachable from exclusions. The subtraction is performed on
// Git objects, rather than paths, so pointers introduced and later deleted
// inside the pending history are still discovered.
func PointerInventoryForObjects(ctx context.Context, targets, exclusions []string) (map[string]LfsFileInfo, error) {
	if len(targets) == 0 {
		return map[string]LfsFileInfo{}, nil
	}

	args := []string{"rev-list", "--objects", "--no-object-names"}
	args = append(args, targets...)
	if len(exclusions) > 0 {
		args = append(args, "--not")
		args = append(args, exclusions...)
	}
	out, err := runHistoryGit(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("git rev-list for LFS history: %w", err)
	}

	var objectIDs []string
	seen := make(map[string]struct{})
	for _, record := range bytes.Split(out, []byte{'\n'}) {
		if len(record) == 0 {
			continue
		}
		oid := string(record)
		if _, ok := seen[oid]; ok {
			continue
		}
		seen[oid] = struct{}{}
		objectIDs = append(objectIDs, oid)
	}
	if len(objectIDs) == 0 {
		return map[string]LfsFileInfo{}, nil
	}

	candidates, err := batchCheckSmallBlobs(ctx, objectIDs)
	if err != nil {
		return nil, err
	}
	pointers, err := batchReadPointers(ctx, candidates)
	if err != nil {
		return nil, err
	}
	pathsByOID, err := pathsForHistoricalPointers(ctx, targets, exclusions, pointers)
	if err != nil {
		return nil, err
	}
	files := make(map[string]LfsFileInfo, len(pointers))
	for _, pointer := range pointers {
		path := pathsByOID[pointer.blobOID]
		if path == "" {
			path = pointer.oid
		}
		if _, exists := files[path]; exists {
			path = pointer.oid
		}
		files[path] = LfsFileInfo{
			Name:        path,
			Size:        pointer.size,
			IsPointer:   true,
			OidType:     pointer.oidType,
			Oid:         pointer.oid,
			Placeholder: pointer.placeholder,
		}
	}
	return files, nil
}

func pathsForHistoricalPointers(ctx context.Context, targets, exclusions []string, pointers []pointerBlob) (map[string]string, error) {
	paths := make(map[string]string, len(pointers))
	if len(pointers) == 0 {
		return paths, nil
	}
	wanted := make(map[string]struct{}, len(pointers))
	for _, pointer := range pointers {
		wanted[pointer.blobOID] = struct{}{}
	}
	args := []string{"log", "--raw", "-z", "--no-abbrev", "--root", "--no-renames", "-m", "--format="}
	args = append(args, targets...)
	if len(exclusions) > 0 {
		args = append(args, "--not")
		args = append(args, exclusions...)
	}
	out, err := runHistoryGit(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("git log for LFS paths: %w", err)
	}
	records := bytes.Split(out, []byte{0})
	for i := 0; i+1 < len(records); i += 2 {
		metadata := strings.TrimSpace(string(records[i]))
		if !strings.HasPrefix(metadata, ":") {
			continue
		}
		fields := strings.Fields(metadata[1:])
		if len(fields) < 5 {
			continue
		}
		path := string(records[i+1])
		for _, oid := range []string{fields[2], fields[3]} {
			if _, ok := wanted[oid]; ok && paths[oid] == "" {
				paths[oid] = path
			}
		}
	}
	return paths, nil
}

type pointerBlobCandidate struct {
	blobOID string
	path    string
}

type pointerBlob struct {
	blobOID, oidType, oid string
	size                  int64
	placeholder           bool
}

const maxPointerBlobSize = 4096

func batchCheckSmallBlobs(ctx context.Context, objectIDs []string) ([]pointerBlobCandidate, error) {
	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch-check")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start git cat-file --batch-check: %w", err)
	}
	go func() {
		defer stdin.Close()
		for _, oid := range objectIDs {
			_, _ = io.WriteString(stdin, oid+"\n")
		}
	}()

	var candidates []pointerBlobCandidate
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 || size > maxPointerBlobSize {
			continue
		}
		candidates = append(candidates, pointerBlobCandidate{blobOID: fields[0]})
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git cat-file --batch-check: %w", err)
	}
	return candidates, nil
}

func batchReadPointers(ctx context.Context, candidates []pointerBlobCandidate) ([]pointerBlob, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start git cat-file --batch: %w", err)
	}
	go func() {
		defer stdin.Close()
		for _, candidate := range candidates {
			_, _ = io.WriteString(stdin, candidate.blobOID+"\n")
		}
	}()

	reader := bufio.NewReader(stdout)
	var pointers []pointerBlob
	for _, candidate := range candidates {
		header, err := reader.ReadString('\n')
		if err != nil {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("read git cat-file header: %w", err)
		}
		fields := strings.Fields(header)
		if len(fields) < 3 || fields[1] != "blob" {
			continue
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || size < 0 || size > maxPointerBlobSize {
			continue
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, fmt.Errorf("read git blob %s: %w", candidate.blobOID, err)
		}
		if _, err := reader.ReadByte(); err != nil {
			return nil, fmt.Errorf("read git blob separator %s: %w", candidate.blobOID, err)
		}
		pointer, ok := parseLFSPointer(string(payload))
		if !ok {
			continue
		}
		pointers = append(pointers, pointerBlob{blobOID: candidate.blobOID, oidType: pointer.OidType, oid: pointer.Oid, size: pointer.Size, placeholder: pointer.Placeholder})
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git cat-file --batch: %w", err)
	}
	return pointers, nil
}

func runHistoryGit(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}
