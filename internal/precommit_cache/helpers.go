package precommit_cache

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// cacheVersionDir is the repository-relative directory under `.git`
	// containing the pre-commit cache layout (paths and oids).
	cacheVersionDir = "drs/pre-commit/v1"
)

// PathEntry represents the per-path cache file format.
// It maps a repository-relative path to the last recorded LFS OID and
// a timestamp when the entry was updated.
type PathEntry struct {
	Path      string `json:"path"`
	LFSOID    string `json:"lfs_oid"`
	UpdatedAt string `json:"updated_at"`
}

// OIDEntry represents the canonical per-OID cache file format.
// The cache historically used `s3_url`; we still read that for compatibility,
// but new writes use `external_url`.
type OIDEntry struct {
	LFSOID        string   `json:"lfs_oid"`
	Paths         []string `json:"paths"`
	ExternalURL   string   `json:"external_url,omitempty"`
	UpdatedAt     string   `json:"updated_at"`
	ContentChange bool     `json:"content_changed"`
}

func (e OIDEntry) MarshalJSON() ([]byte, error) {
	type wireOIDEntry struct {
		LFSOID        string   `json:"lfs_oid"`
		Paths         []string `json:"paths"`
		ExternalURL   string   `json:"external_url,omitempty"`
		UpdatedAt     string   `json:"updated_at"`
		ContentChange bool     `json:"content_changed"`
	}
	return json.Marshal(wireOIDEntry{
		LFSOID:        e.LFSOID,
		Paths:         e.Paths,
		ExternalURL:   strings.TrimSpace(e.ExternalURL),
		UpdatedAt:     e.UpdatedAt,
		ContentChange: e.ContentChange,
	})
}

func (e *OIDEntry) UnmarshalJSON(data []byte) error {
	type wireOIDEntry struct {
		LFSOID        string   `json:"lfs_oid"`
		Paths         []string `json:"paths"`
		ExternalURL   string   `json:"external_url,omitempty"`
		S3URL         string   `json:"s3_url,omitempty"`
		UpdatedAt     string   `json:"updated_at"`
		ContentChange bool     `json:"content_changed"`
	}
	var wire wireOIDEntry
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	e.LFSOID = wire.LFSOID
	e.Paths = wire.Paths
	e.ExternalURL = firstNonEmpty(wire.ExternalURL, wire.S3URL)
	e.UpdatedAt = wire.UpdatedAt
	e.ContentChange = wire.ContentChange
	return nil
}

// Cache describes the on-disk layout for the local pre-commit cache.
// The cache is shared local bookkeeping for precommit/add-url, not an
// authoritative metadata store.
type Cache struct {
	GitDir    string
	Root      string
	PathsDir  string
	OIDsDir   string
	StatePath string
}

// Open discovers the repository `.git` directory and returns a Cache with the
// current pre-commit cache layout resolved.
func Open(ctx context.Context) (*Cache, error) {
	gitDir, err := gitRevParseGitDir(ctx)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(gitDir, cacheVersionDir)
	return &Cache{
		GitDir:    gitDir,
		Root:      root,
		PathsDir:  filepath.Join(root, "paths"),
		OIDsDir:   filepath.Join(root, "oids"),
		StatePath: filepath.Join(root, "state.json"),
	}, nil
}

func EnsureLayout(cache *Cache) error {
	if cache == nil {
		return errors.New("cache is nil")
	}
	for _, dir := range []string{cache.Root, cache.PathsDir, cache.OIDsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// EncodePath returns a filesystem-safe base64 raw-URL encoding for a path.
func EncodePath(path string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(path))
}

func PathEntryPath(cache *Cache, path string) string {
	return filepath.Join(cache.PathsDir, EncodePath(path)+".json")
}

func OIDEntryPath(cache *Cache, oid string) string {
	sum := sha256.Sum256([]byte(oid))
	return filepath.Join(cache.OIDsDir, fmt.Sprintf("%x.json", sum[:]))
}

func ReadPathEntry(cache *Cache, path string) (*PathEntry, bool, error) {
	file := PathEntryPath(cache, path)
	data, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var entry PathEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false, err
	}
	return &entry, true, nil
}

func WritePathEntry(cache *Cache, entry PathEntry) error {
	return writeJSONAtomic(PathEntryPath(cache, entry.Path), entry)
}

func ReadOIDEntry(cache *Cache, oid string, now string) (*OIDEntry, error) {
	file := OIDEntryPath(cache, oid)
	entry := &OIDEntry{
		LFSOID:    oid,
		Paths:     []string{},
		UpdatedAt: now,
	}
	data, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return entry, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, entry); err != nil {
		return nil, err
	}
	entry.LFSOID = oid
	return entry, nil
}

func UpsertOIDPath(cache *Cache, oid, oldPath, newPath, externalURL, now string, contentChanged bool) error {
	entry, err := ReadOIDEntry(cache, oid, now)
	if err != nil {
		return err
	}

	paths := make(map[string]struct{}, len(entry.Paths)+1)
	for _, existing := range entry.Paths {
		existing = strings.TrimSpace(existing)
		if existing == "" {
			continue
		}
		paths[existing] = struct{}{}
	}
	if oldPath != "" {
		delete(paths, oldPath)
	}
	if newPath != "" {
		paths[newPath] = struct{}{}
	}

	entry.Paths = sortedKeys(paths)
	entry.UpdatedAt = now
	entry.ContentChange = entry.ContentChange || contentChanged
	entry.ExternalURL = firstNonEmpty(externalURL, entry.ExternalURL)

	return writeJSONAtomic(OIDEntryPath(cache, oid), entry)
}

func RemoveOIDPath(cache *Cache, oid, path, now string) error {
	entry, err := ReadOIDEntry(cache, oid, now)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	paths := make(map[string]struct{}, len(entry.Paths))
	for _, existing := range entry.Paths {
		existing = strings.TrimSpace(existing)
		if existing == "" || existing == path {
			continue
		}
		paths[existing] = struct{}{}
	}
	entry.Paths = sortedKeys(paths)
	entry.UpdatedAt = now
	return writeJSONAtomic(OIDEntryPath(cache, oid), entry)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func writeJSONAtomic(path string, v any) (retErr error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) && retErr == nil {
			retErr = closeErr
		}
	}()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		if rmErr := removeIfExists(tmp); rmErr != nil {
			return errors.Join(err, rmErr)
		}
		return err
	}
	if err := f.Sync(); err != nil {
		if rmErr := removeIfExists(tmp); rmErr != nil {
			return errors.Join(err, rmErr)
		}
		return err
	}
	if err := f.Close(); err != nil {
		if rmErr := removeIfExists(tmp); rmErr != nil {
			return errors.Join(err, rmErr)
		}
		return err
	}
	return os.Rename(tmp, path)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// gitRevParseGitDir runs `git rev-parse --git-dir` (and `--show-toplevel` if
// necessary) to return an absolute path to the repository `.git` directory.
func gitRevParseGitDir(ctx context.Context) (string, error) {
	out, err := git(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	gitDir := strings.TrimSpace(string(out))
	if gitDir == "" {
		return "", errors.New("could not determine .git dir")
	}
	if !filepath.IsAbs(gitDir) {
		rootOut, err := git(ctx, "rev-parse", "--show-toplevel")
		if err != nil {
			return "", err
		}
		root := strings.TrimSpace(string(rootOut))
		gitDir = filepath.Join(root, gitDir)
	}
	return gitDir, nil
}

func git(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(
			"git %s: %s",
			strings.Join(args, " "),
			strings.TrimSpace(string(out)),
		)
	}
	return out, nil
}
