package drsdelete

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/lfs"
)

func collectLivePathsByOID(refs []RefUpdate, logger *slog.Logger) (map[string][]string, error) {
	targets := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		newSHA := strings.TrimSpace(ref.NewSHA)
		if newSHA == "" || isZeroSHA(newSHA) {
			continue
		}
		if _, ok := seen[newSHA]; ok {
			continue
		}
		seen[newSHA] = struct{}{}
		targets = append(targets, newSHA)
	}
	if len(targets) == 0 {
		return map[string][]string{}, nil
	}

	files, err := lfs.GetLfsFilesForRefs(targets, logger)
	if err != nil {
		return nil, err
	}
	liveByOID := make(map[string][]string)
	for path, info := range files {
		oid := "sha256:" + strings.TrimPrefix(strings.TrimSpace(info.Oid), "sha256:")
		liveByOID[oid] = append(liveByOID[oid], path)
	}
	for oid := range liveByOID {
		sort.Strings(liveByOID[oid])
	}
	return liveByOID, nil
}
