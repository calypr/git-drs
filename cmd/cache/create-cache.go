package cache

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/utils"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "create-cache <manifest.tsv>",
	Short: "create a cache mapping a sha256 back to a DRS URI by file",
	Long:  "create a cache mapping a sha256 back to a DRS URI using a list of DRS URIs and their sha256s",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		file := args[0]

		// load file
		f, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("failed to open manifest file: %w", err)
		}
		defer f.Close()

		// for each pair of <drs_uri> <sha256> in the file, create a file named by sha256
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Printf("Processing line: %s\n", line)
			// Expecting tab-separated: <sha256>\t<drs_uri>
			parts := strings.Fields(line)
			if len(parts) != 2 {
				fmt.Printf("Skipping malformed line (only %d parts): %s\n", len(parts), line)
				continue // skip malformed lines
			}
			drsURI := parts[0]
			sha := parts[1]
			fmt.Printf("Indexing DRS URI %s with sha256 %s\n", drsURI, sha)

			// write a header "files.drs_uri" followed by drs ID to the file
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
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading manifest file: %w", err)
		}

		fmt.Printf("Cache created in %s\n", utils.DRS_REF_DIR)
		return nil
	},
}
