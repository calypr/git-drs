package list

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v6"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "list files",
	Long:    ``,
	Args:    cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		gitTop, err := getRepo()
		if err != nil {
			return fmt.Errorf("could not get git repository: %w", err)
		}

		manifestDir := filepath.Join(gitTop, "MANIFEST")
		fmt.Printf("Manifest: %s\n", manifestDir)
		s, err := os.Stat(manifestDir)
		if err != nil {
			return err
		}
		if s.IsDir() {
			files, err := filepath.Glob(filepath.Join(manifestDir, "*"))
			if err != nil {
				return err
			}
			for _, i := range files {
				fmt.Printf("%s\n", i)
			}
		}
		return nil
	},
}

func getRepo() (string, error) {
	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error: %s\n", err)
	}

	// Get the repo "closest" to the working directory
	repo, err := git.PlainOpenWithOptions(cwd, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return "", fmt.Errorf("could not open git repository: %w", err)
	}

	// Get the worktree from the repo
	worktree, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("could not get worktree: %w", err)
	}

	// Get the root directory of the repo
	gitTop := worktree.Filesystem.Root()

	return gitTop, nil
}
