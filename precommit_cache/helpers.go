// helpers for pre-push (reading .git/drs/pre-commit cache)
// ---------------------------------------------------------
// LFS-only local cache readers for pre-push validation.
//
// These helpers expose NON-AUTHORITATIVE hints recorded by pre-commit.
// Pre-push is expected to resolve truth via Indexd / DRS and compare.
//
// Cache layout (per ADR):
//   .git/drs/pre-commit/v1/paths/<encoded-path>.json   (Path -> OID)
//   .git/drs/pre-commit/v1/oids/<oid-hash>.json        (OID -> paths[] + external_url hint)
//
// NOTE:
// OID files are named by sha256(oid string), not the raw oid.
// These helpers reproduce that mapping exactly.

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
	cacheVersionDir = "drs/pre-commit/v1"
)

type PathEntry struct {
	Path      string `json:"path"`
	LFSOID    string `json:"lfs_oid"`
	UpdatedAt string `json:"updated_at"`
}

type OIDEntry struct {
	LFSOID        string   `json:"lfs_oid"`
	Paths         []string `json:"paths"`
	ExternalURL   string   `json:"external_url,omitempty"` // non-authoritative hint
	UpdatedAt     string   `json:"updated_at"`
	ContentChange bool     `json:"content_changed"`
}

// Cache provides read-only access to .git/drs/pre-commit cache.
type Cache struct {
	GitDir    string
	Root      string
	PathsDir  string
	OIDsDir   string
	StatePath string
}

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

// LookupPathsByOID returns advisory paths that recently referenced this OID.
func (c *Cache) LookupPathsByOID(oid string) ([]string, bool, error) {
	oe, ok, err := c.ReadOIDEntry(oid)
	if err != nil || !ok {
		return nil, ok, err
	}
	paths := append([]string(nil), oe.Paths...)
	sort.Strings(paths)
	return paths, true, nil
}

// LookupExternalURLByOID returns the cached external URL hint (if any).
// Returns ("", false, nil) if the OID entry is missing OR the hint is empty.
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

// ResolveExternalURLByPath maps:
//
//	path -> oid -> external_url (hint)
//
// Returns ("", false, nil) if any step is missing.
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

// CheckExternalURLMismatch compares a cached external URL hint against an authoritative URL.
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

// StaleAfter reports whether a cache timestamp is older than maxAge.
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

func (c *Cache) pathEntryFile(path string) string {
	return filepath.Join(c.PathsDir, EncodePath(path)+".json")
}

func (c *Cache) oidEntryFile(oid string) string {
	sum := sha256.Sum256([]byte(oid))
	return filepath.Join(c.OIDsDir, fmt.Sprintf("%x.json", sum[:]))
}

func EncodePath(path string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(path))
}

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
