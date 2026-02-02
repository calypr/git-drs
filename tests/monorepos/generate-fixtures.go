// Creates directories from stdin lines, each with 1–6 `sub-directory-N` subfolders, each containing 100–1000 files of 1 KiB whose contents are the relative file path. Save as `generate-fixtures.go`.

package main

import (
	"bufio"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	minSubDirs = 1
	maxSubDirs = 6

	minFilesPerSub = 100
	maxFilesPerSub = 1000

	fileSizeBytes = 1024
)

// main reads directory names from stdin (one per line) and creates a set of
// "fixture" directories and files for each input name. For each top-level
// directory it creates between minSubDirs and maxSubDirs subdirectories
// named "sub-directory-N". Each subdirectory receives between minFilesPerSub
// and maxFilesPerSub files. File contents are written as the relative path
// bytes. The program prints progress and errors to stderr and exits with a
// non-zero code on read errors or when no input is provided.
func main() {
	rand.Seed(time.Now().UnixNano())

	// Flags: if >0 they override randomness
	numSubdirsFlag := flag.Int("number-of-subdirectories", 0, "fixed number of subdirectories per top-level directory (overrides random)")
	numFilesFlag := flag.Int("number-of-files", 0, "fixed number of files per subdirectory (overrides random)")
	flag.Parse()

	scanner := bufio.NewScanner(os.Stdin)
	entries := []string{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		entries = append(entries, line)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "reading stdin: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "no input lines; provide one directory name per line on stdin")
		os.Exit(1)
	}

	// Determine digits for file name padding based on configured or default max
	maxFilesConsidered := maxFilesPerSub
	if *numFilesFlag > 0 && *numFilesFlag > maxFilesConsidered {
		maxFilesConsidered = *numFilesFlag
	}
	maxFilesDigits := len(strconv.Itoa(maxFilesConsidered))

	for _, name := range entries {
		// Clean the path and disallow absolute paths for safety
		clean := filepath.Clean(name)
		if filepath.IsAbs(clean) {
			fmt.Fprintf(os.Stderr, "skipping absolute path: %s\n", name)
			continue
		}
		if strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
			fmt.Fprintf(os.Stderr, "skipping path outside current tree: %s\n", name)
			continue
		}

		if err := os.MkdirAll(clean, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", clean, err)
			continue
		}

		// Choose number of subdirectories
		var nSub int
		if *numSubdirsFlag > 0 {
			if *numSubdirsFlag < 1 {
				fmt.Fprintf(os.Stderr, "invalid --number-of-subdirectories: %d (must be >= 1)\n", *numSubdirsFlag)
				continue
			}
			nSub = *numSubdirsFlag
		} else {
			nSub = rand.Intn(maxSubDirs-minSubDirs+1) + minSubDirs
		}

		for si := 1; si <= nSub; si++ {
			subdir := filepath.Join(clean, fmt.Sprintf("sub-directory-%d", si))
			if err := os.MkdirAll(subdir, 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", subdir, err)
				continue
			}

			// Choose number of files per subdirectory
			var nFiles int
			if *numFilesFlag > 0 {
				if *numFilesFlag < 1 {
					fmt.Fprintf(os.Stderr, "invalid --number-of-files: %d (must be >= 1)\n", *numFilesFlag)
					continue
				}
				nFiles = *numFilesFlag
			} else {
				nFiles = rand.Intn(maxFilesPerSub-minFilesPerSub+1) + minFilesPerSub
			}

			largeFileNumberOfLines := 480006 // approx 20 MiB
			for fi := 1; fi <= nFiles; fi++ {
				filename := fmt.Sprintf("file-%0*d.dat", maxFilesDigits, fi)
				path := filepath.Join(subdir, filename)
				// if fi is odd just write the path; if even, write the path LARGE_FILE_NUMBER_OF_LINES
				// if fi is odd just write the path; if even, write the path LARGE_FILE_NUMBER_OF_LINES
				var content []byte
				if fi%2 == 1 {
					content = []byte(path)
				} else {
					var b strings.Builder
					// Pre-allocate roughly to avoid too many allocations (estimate)
					b.Grow(len(path)*largeFileNumberOfLines + largeFileNumberOfLines)
					for i := 0; i < largeFileNumberOfLines; i++ {
						b.WriteString(path)
						b.WriteByte('\n')
					}
					content = []byte(b.String())
				}
				if err := os.WriteFile(path, content, 0o644); err != nil {
					fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
				}
			}
			fmt.Fprintf(os.Stderr, "created %d files in %s\n", nFiles, subdir)
		}
		fmt.Fprintf(os.Stderr, "done: %s (%d subdirs)\n", clean, nSub)
	}
}
