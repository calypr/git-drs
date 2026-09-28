package lsfiles

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
)

type browseListing struct {
	operand   string
	directory bool
	entries   []string
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
			listing.entries = []string{path}
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
			listing.entries = append(listing.entries, entry.Name())
		}
		listings = append(listings, listing)
	}

	return listings, nil
}
