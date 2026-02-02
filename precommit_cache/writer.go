package precommit_cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (c *Cache) UpdatePathEntry(ctx context.Context, log *slog.Logger, path, oid, extURL string) error {
	if log == nil {
		return errors.New("logger is required")
	}
	if err := c.EnsureDirs(log); err != nil {
		return err
	}

	rel, err := c.relPath(path)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	prev, ok, err := c.ReadPathEntry(rel)
	if err != nil {
		return err
	}

	changed := ok && prev.LFSOID != "" && prev.LFSOID != oid
	pe := &PathEntry{Path: rel, LFSOID: oid, UpdatedAt: now}

	if err := writeAtomic(c.pathEntryFile(rel), pe); err != nil {
		return err
	}
	c.pathCache.Set(rel, pe)

	if err := c.updateOID(oid, rel, extURL, now, changed); err != nil {
		return err
	}
	if changed {
		_ = c.removePathFromOID(prev.LFSOID, rel, now)
	}
	return nil
}

func (c *Cache) DeletePathEntry(ctx context.Context, log *slog.Logger, path string) error {
	if log == nil {
		return errors.New("logger is required")
	}
	if err := c.EnsureDirs(log); err != nil {
		return err
	}

	rel, err := c.relPath(path)
	if err != nil {
		return err
	}

	pe, ok, err := c.ReadPathEntry(rel)
	if err != nil || !ok {
		return nil
	}

	if err := os.Remove(c.pathEntryFile(rel)); err != nil && !os.IsNotExist(err) {
		return err
	}
	c.pathCache.Delete(rel)

	if pe.LFSOID != "" {
		_ = c.removePathFromOID(pe.LFSOID, rel, time.Now().UTC().Format(time.RFC3339))
	}
	return nil
}

func (c *Cache) EnsureDirs(log *slog.Logger) error {
	if _, err := os.Stat(c.Root); os.IsNotExist(err) {
		log.Warn("creating missing cache dir", "path", c.Root)
	}
	if err := os.MkdirAll(c.PathsDir, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(c.OIDsDir, 0o755)
}

func (c *Cache) updateOID(oid, path, extURL, now string, changed bool) error {
	return c.mutateOID(oid, func(e *OIDEntry) {
		s := make(map[string]struct{})
		for _, p := range e.Paths {
			s[p] = struct{}{}
		}
		if path != "" {
			s[path] = struct{}{}
		}
		e.Paths = sortedKeys(s)
		e.UpdatedAt = now
		e.ContentChange = e.ContentChange || changed
		if strings.TrimSpace(extURL) != "" {
			e.ExternalURL = extURL
		}
	})
}

func (c *Cache) removePathFromOID(oid, path, now string) error {
	return c.mutateOID(oid, func(e *OIDEntry) {
		s := make(map[string]struct{})
		for _, p := range e.Paths {
			if p != path {
				s[p] = struct{}{}
			}
		}
		e.Paths = sortedKeys(s)
		e.UpdatedAt = now
	})
}

func (c *Cache) mutateOID(oid string, fn func(*OIDEntry)) error {
	e, _, err := c.ReadOIDEntry(oid)
	if err != nil {
		return err
	}
	if e == nil {
		e = &OIDEntry{LFSOID: oid}
	}
	fn(e)
	if err := writeAtomic(c.oidEntryFile(oid), e); err != nil {
		return err
	}
	c.oidCache.Set(oid, e)
	return nil
}

func (c *Cache) relPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	root, err := filepath.EvalSymlinks(c.RepoRoot)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(p)
	if filepath.IsAbs(clean) {
		clean, err = filepath.EvalSymlinks(clean)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, clean)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("path %s is outside repo root %s", clean, root)
		}
		return filepath.ToSlash(rel), nil
	}
	return filepath.ToSlash(clean), nil
}

func writeAtomic(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
