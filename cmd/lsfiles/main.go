package lsfiles

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

var gitRemote string
var drsRemote string
var includePatterns []string
var showLong bool
var nameOnly bool
var jsonOutput bool
var drsStatus bool

func validateOutputFlags() error {
	if nameOnly && jsonOutput {
		return fmt.Errorf("--name-only and --json are mutually exclusive")
	}
	if showLong && nameOnly {
		return fmt.Errorf("--long and --name-only are mutually exclusive")
	}
	return nil
}

var Cmd = &cobra.Command{
	Use:   "ls-files [pathspec...]",
	Short: "List tracked DRS/LFS pointer files in the repository",
	Long:  "List tracked DRS/Git-LFS pointer files in the repository. By default this behaves like a local file inventory. Use --drs to also resolve DRS registration status.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFlags(); err != nil {
			return err
		}
		patterns := append([]string{}, includePatterns...)
		patterns = append(patterns, args...)
		rows, err := collectRows(context.Background(), gitRemote, drsRemote, patterns, drsStatus)
		if err != nil {
			return err
		}
		return printRows(cmd, rows)
	},
}

func init() {
	Cmd.Flags().StringVarP(&gitRemote, "git-remote", "r", "", "target remote Git server (default: origin)")
	Cmd.Flags().StringVarP(&drsRemote, "drs-remote", "d", "", "target remote DRS server (default: origin)")
	Cmd.Flags().StringArrayVarP(&includePatterns, "include", "I", nil, "include pathspec/glob pattern(s)")
	Cmd.Flags().BoolVarP(&showLong, "long", "l", false, "show full object IDs")
	Cmd.Flags().BoolVarP(&nameOnly, "name-only", "n", false, "show only file paths")
	Cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	Cmd.Flags().BoolVar(&drsStatus, "drs", false, "include DRS registration lookup details")
}
