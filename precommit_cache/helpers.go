package precommit_cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/maypok86/otter"
)

const cacheVersionDir = "drs/pre-commit/v1"

type PathEntry struct {
	Path      string `json:"path"`
	LFSOID    string `json:"lfs_oid"`
	UpdatedAt string `json:"updated_at"`
}

type OIDEntry struct {
	LFSOID        string   `json:"lfs_oid"`
	Paths         []string `json:"paths"`
	ExternalURL   string   `json:"external_url,omitempty"`
	UpdatedAt     string   `json:"updated_at"`
	ContentChange bool     `json:"content_changed"`
}

type Cache struct {
	GitDir, RepoRoot, Root, PathsDir, OIDsDir, StatePath string
	pathCache                                            otter.Cache[string, *PathEntry]
	oidCache                                             otter.Cache[string, *OIDEntry]
}

func Open(ctx context.Context) (*Cache, error) {
	gitDir, err := git(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return nil, err
	}
	gd := strings.TrimSpace(string(gitDir))
	top, err := git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	repoRoot := strings.TrimSpace(string(top))

	if !filepath.IsAbs(gd) {
		gd = filepath.Join(repoRoot, gd)
	}

	root := filepath.Join(gd, cacheVersionDir)

	// Initialize strict typed caches
	pc, err := otter.MustBuilder[string, *PathEntry](10000).Build()
	if err != nil {
		return nil, fmt.Errorf("create path cache: %w", err)
	}
	oc, err := otter.MustBuilder[string, *OIDEntry](1000).Build() // OIDs are fewer than paths usually
	if err != nil {
		return nil, fmt.Errorf("create oid cache: %w", err)
	}

	return &Cache{
		GitDir:    gd,
		RepoRoot:  repoRoot,
		Root:      root,
		PathsDir:  filepath.Join(root, "paths"),
		OIDsDir:   filepath.Join(root, "oids"),
		StatePath: filepath.Join(root, "state.json"),
		pathCache: pc,
		oidCache:  oc,
	}, nil
}

// LookupOIDByPath returns the cached LFS OID for a repo-relative path.
func (c *Cache) LookupOIDByPath(path string) (string, bool, error) {
	pe, ok, err := c.ReadPathEntry(path)
	if err != nil || !ok {
		return "", ok, err
	}
	return pe.LFSOID, pe.LFSOID != "", nil
}

// LookupPathsByOID returns paths that recently referenced the given LFS OID.
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
func (c *Cache) LookupExternalURLByOID(oid string) (string, bool, error) {
	oe, ok, err := c.ReadOIDEntry(oid)
	if err != nil || !ok {
		return "", ok, err
	}
	u := strings.TrimSpace(oe.ExternalURL)
	return u, u != "", nil
}

func (c *Cache) ResolveExternalURLByPath(path string) (string, bool, error) {
	oid, ok, err := c.LookupOIDByPath(path)
	if err != nil || !ok {
		return "", false, err
	}
	return c.LookupExternalURLByOID(oid)
}

func (c *Cache) ReadPathEntry(path string) (*PathEntry, bool, error) {
	if val, ok := c.pathCache.Get(path); ok {
		return val, true, nil
	}
	f := c.pathEntryFile(path)
	b, err := os.ReadFile(f)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var v PathEntry
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, false, err
	}
	c.pathCache.Set(path, &v)
	return &v, true, nil
}

func (c *Cache) ReadOIDEntry(oid string) (*OIDEntry, bool, error) {
	if val, ok := c.oidCache.Get(oid); ok {
		return val, true, nil
	}
	f := c.oidEntryFile(oid)
	b, err := os.ReadFile(f)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var v OIDEntry
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, false, err
	}
	c.oidCache.Set(oid, &v)
	return &v, true, nil
}

func (c *Cache) pathEntryFile(path string) string {
	return filepath.Join(c.PathsDir, EncodePath(path)+".json")
}

func (c *Cache) oidEntryFile(oid string) string {
	sum := sha256.Sum256([]byte(oid))
	return filepath.Join(c.OIDsDir, fmt.Sprintf("%x.json", sum[:]))
}

func EncodePath(p string) string { return base64.RawURLEncoding.EncodeToString([]byte(p)) }
func DecodePath(e string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(e)
	return string(b), err
}
func StaleAfter(ua string, age time.Duration) bool {
	t, err := time.Parse(time.RFC3339, ua)
	return err == nil && time.Since(t) > age
}

func CheckExternalURLMismatch(hint, auth string) error {
	h, a := strings.TrimSpace(hint), strings.TrimSpace(auth)
	if h != "" && a != "" && h != a {
		return fmt.Errorf("external URL mismatch: cache=%q auth=%q", h, a)
	}
	return nil
}

func git(ctx context.Context, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %s (stderr: %s)", strings.Join(args, " "), strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
