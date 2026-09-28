package lsfiles

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type browseEntry struct {
	name    string
	mode    fs.FileMode
	size    int64
	modTime time.Time
}

type browseListing struct {
	operand   string
	directory bool
	entries   []browseEntry
}

func collectBrowseListings(paths []string) ([]browseListing, error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}

	listings := make([]browseListing, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			targetInfo, err := os.Stat(path)
			if err == nil && targetInfo.IsDir() {
				info = targetInfo
			}
		}

		listing := browseListing{operand: path, directory: info.IsDir()}
		if !info.IsDir() {
			listing.entries = []browseEntry{{
				name:    path,
				mode:    info.Mode(),
				size:    info.Size(),
				modTime: info.ModTime(),
			}}
			listings = append(listings, listing)
			continue
		}

		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("read directory %s: %w", path, err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			entryInfo, err := entry.Info()
			if err != nil {
				return nil, fmt.Errorf("read file information for %s: %w", filepath.Join(path, entry.Name()), err)
			}
			listing.entries = append(listing.entries, browseEntry{
				name:    entry.Name(),
				mode:    entryInfo.Mode(),
				size:    entryInfo.Size(),
				modTime: entryInfo.ModTime(),
			})
		}
		listings = append(listings, listing)
	}

	return listings, nil
}

func formatBrowseSize(size int64, human bool) string {
	if !human || size < 1024 {
		return fmt.Sprintf("%d", size)
	}
	units := "KMGTPE"
	value := float64(size)
	unit := -1
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f%c", value, units[unit])
}
