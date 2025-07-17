package cache

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bmeg/git-drs/client"
	"github.com/bmeg/git-drs/utils"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "create-cache <manifest.tsv>",
	Short: "create a local version of a file manifest containing DRS URIs",
	Long:  "create a local version of a file manifest containing DRS URIs. Enables LFS to map its file object id (sha256) back to a DRS URI by file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file := args[0]

		// load file
		f, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("failed to open manifest file: %w", err)
		}
		defer f.Close()

		// Use encoding/csv with tab delimiter for TSV
		r := csv.NewReader(f)
		r.Comma = '\t'

		// Read header
		header, err := r.Read()
		if err != nil {
			return fmt.Errorf("failed to read header: %w", err)
		}

		// Map column names to indices
		colIdx := map[string]int{}
		for i, col := range header {
			colIdx[col] = i
		}

		// Check required columns
		shaIdx, shaOk := colIdx["files.sha256"]
		drsIdx, drsOk := colIdx["files.drs_uri"]
		if !shaOk || !drsOk {
			return fmt.Errorf("manifest must contain 'files.sha256' and 'files.drs_uri' columns")
		}

		// Read each row
		for {
			row, err := r.Read()
			if err != nil {
				if err == io.EOF {
					break
				}
				return fmt.Errorf("error reading manifest file: %w", err)
			}
			sha := row[shaIdx]
			drsURI := row[drsIdx]
			fmt.Printf("Indexing DRS URI %s with sha256 %s\n", drsURI, sha)

			// create sha to DRS URI mapping
			objPath, err := client.GetObjectPath(utils.DRS_REF_DIR, sha)
			if err != nil {
				return fmt.Errorf("failed to get object path for %s: %w", sha, err)
			}

			if err := os.MkdirAll(filepath.Dir(objPath), 0755); err != nil {
				return fmt.Errorf("failed to create dir for %s: %w", objPath, err)
			}

			contents := fmt.Sprintf("files.drs_uri\n%s\n", drsURI)
			if err := os.WriteFile(objPath, []byte(contents), 0644); err != nil {
				return fmt.Errorf("failed to write DRS URI for %s: %w", sha, err)
			}

			// Split DRS URI into a custom path and write sha to custom path
			customPath, err := utils.CreateCustomPath(utils.DRS_REF_DIR, drsURI)
			if err != nil {
				return fmt.Errorf("failed to create custom path for %s: %w", drsURI, err)
			}
			if err := os.MkdirAll(filepath.Dir(customPath), 0755); err != nil {
				return fmt.Errorf("failed to create dir for %s: %w", customPath, err)
			}
			if err := os.WriteFile(customPath, []byte(sha), 0644); err != nil {
				return fmt.Errorf("failed to write sha for %s: %w", drsURI, err)
			}
		}

		fmt.Printf("Cache created in %s\n", utils.DRS_REF_DIR)
		return nil
	},
}
