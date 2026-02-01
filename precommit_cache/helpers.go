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
	"time"
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

// OIDEntry represents the per-OID cache file format.
// It lists repository paths that referenced the OID, an optional
// non-authoritative external URL hint, a timestamp and a flag that
// indicates whether content changed for a path update.
type OIDEntry struct {
	LFSOID        string   `json:"lfs_oid"`
	Paths         []string `json:"paths"`
	ExternalURL   string   `json:"external_url,omitempty"` // non-authoritative hint
	UpdatedAt     string   `json:"updated_at"`
	ContentChange bool     `json:"content_changed"`
}

// Cache provides read-only access to the `.git/drs/pre-commit` cache.
// Use Open to construct an instance with correct paths resolved.
type Cache struct {
	GitDir    string
	Root      string
	PathsDir  string
	OIDsDir   string
	StatePath string
}

// Open discovers the repository `.git` directory and returns a Cache
// configured to read the repository's pre-commit cache layout.
// Returns an error if git metadata cannot be resolved.
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

//
// Primary lookup helpers
//

// LookupOIDByPath returns the cached LFS OID for a repo-relative path.
// It returns (oid, true, nil) when present, (\"\", false, nil) when absent,
// and an error if the underlying read failed.
func (c *Cache) LookupOIDByPath(path string) (string, bool, error) {
	pe, ok, err := c.ReadPathEntry(path)
	if err != nil || !ok {
		return "", ok, err
	}
	if pe.LFSOID == "" {
		return "", false, nil
	}
	return pe.LFSOID, true, nil
}

// LookupPathsByOID returns advisory repository-relative paths that recently
// referenced the given LFS OID. Paths are returned sorted. Absent OID yields
// (nil, false, nil).
func (c *Cache) LookupPathsByOID(oid string) ([]string, bool, error) {
	oe, ok, err := c.ReadOIDEntry(oid)
	if err != nil || !ok {
		return nil, ok, err
	}
	paths := append([]string(nil), oe.Paths...)
	sort.Strings(paths)
	return paths, true, nil
}

// LookupExternalURLByOID returns the cached external URL hint for an OID.
// Returns (\"\", false, nil) if the entry is missing or the hint is empty.
func (c *Cache) LookupExternalURLByOID(oid string) (string, bool, error) {
	oe, ok, err := c.ReadOIDEntry(oid)
	if err != nil || !ok {
		return "", ok, err
	}
	u := strings.TrimSpace(oe.ExternalURL)
	if u == "" {
		return "", false, nil
	}
	return u, true, nil
}

// ResolveExternalURLByPath resolves a path -> oid -> external_url (hint).
// Returns the external URL hint when available. Missing data yields
// (\"\", false, nil).
func (c *Cache) ResolveExternalURLByPath(path string) (string, bool, error) {
	oid, ok, err := c.LookupOIDByPath(path)
	if err != nil || !ok {
		return "", false, err
	}
	return c.LookupExternalURLByOID(oid)
}

//
// Lower-level file access
//

// ReadPathEntry reads and parses the JSON path entry for a repository-relative
// path. Returns (entry, true, nil) on success, (nil, false, nil) if the file
// does not exist, or an error on I/O/parse failure.
func (c *Cache) ReadPathEntry(path string) (*PathEntry, bool, error) {
	f := c.pathEntryFile(path)
	b, err := os.ReadFile(f)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read path entry %q: %w", f, err)
	}
	var pe PathEntry
	if err := json.Unmarshal(b, &pe); err != nil {
		return nil, false, fmt.Errorf("parse path entry %q: %w", f, err)
	}
	return &pe, true, nil
}

// ReadOIDEntry reads and parses the JSON OID entry for an LFS OID string.
// Returns (entry, true, nil) on success, (nil, false, nil) if missing,
// or an error on I/O/parse failure.
func (c *Cache) ReadOIDEntry(oid string) (*OIDEntry, bool, error) {
	f := c.oidEntryFile(oid)
	b, err := os.ReadFile(f)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read oid entry %q: %w", f, err)
	}
	var oe OIDEntry
	if err := json.Unmarshal(b, &oe); err != nil {
		return nil, false, fmt.Errorf("parse oid entry %q: %w", f, err)
	}
	return &oe, true, nil
}

//
// Validation helpers (optional)
//

// CheckExternalURLMismatch compares a cached external URL hint against an
// authoritative URL. If either value is empty this is a no-op. When both are
// non-empty and differ an error describing the mismatch is returned.
func CheckExternalURLMismatch(localHint, authoritative string) error {
	l := strings.TrimSpace(localHint)
	a := strings.TrimSpace(authoritative)
	if l == "" || a == "" {
		return nil
	}
	if l != a {
		return fmt.Errorf(
			"external URL mismatch: cache=%q authoritative=%q",
			l, a,
		)
	}
	return nil
}

// StaleAfter reports whether a JSON entry with the given updatedAt RFC3339
// timestamp is older than maxAge. Returns false if the timestamp cannot be parsed.
func StaleAfter(updatedAt string, maxAge time.Duration) bool {
	t, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return false
	}
	return time.Since(t) > maxAge
}

//
// Filename / encoding helpers
//

// pathEntryFile returns the filesystem path to the JSON file for the given
// repository-relative path within the Cache.PathsDir.
func (c *Cache) pathEntryFile(path string) string {
	return filepath.Join(c.PathsDir, EncodePath(path)+".json")
}

// oidEntryFile returns the filesystem path to the JSON file for the given
// LFS OID. Files are named by sha256(oid) to avoid filesystem restrictions.
func (c *Cache) oidEntryFile(oid string) string {
	sum := sha256.Sum256([]byte(oid))
	return filepath.Join(c.OIDsDir, fmt.Sprintf("%x.json", sum[:]))
}

// EncodePath returns a filesystem-safe base64 raw-URL encoding for a path.
// The encoding is reversible by DecodePath.
func EncodePath(path string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(path))
}

// DecodePath decodes a value produced by EncodePath back to the original path.
// Returns an error if the input is not valid base64 raw-URL.
func DecodePath(encoded string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

//
// Git helpers
//

// gitRevParseGitDir runs `git rev-parse --git-dir` (and `--show-toplevel` if
// necessary) to return an absolute path to the repository `.git` directory.
// Returns an error when git fails or the result cannot be resolved.
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

// git executes a git command and returns combined output. If git exits with
// a non-zero status the returned error includes the command and stderr/stdout.
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
