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
var longListing bool
var humanReadable bool
var pointers bool
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
	Use:   "ls-files [path...]",
	Short: "List files in the current directory or inspect DRS pointers",
	Long:  "List visible files and directories in the current directory. Use --pointers to inspect tracked DRS/Git-LFS pointer files, or --drs to include DRS registration details.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if pointerInventoryRequested() {
			if err := validateModeFlags(); err != nil {
				return err
			}
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
		}

		listings, err := collectBrowseListings(args)
		if err != nil {
			return err
		}
		return printBrowseListings(cmd, listings, longListing, humanReadable)
	},
}

func pointerInventoryRequested() bool {
	return pointers || drsStatus || showLong || nameOnly || jsonOutput ||
		len(includePatterns) > 0 || gitRemote != "" || drsRemote != ""
}

func validateModeFlags() error {
	if pointerInventoryRequested() && (longListing || humanReadable) {
		return fmt.Errorf("directory listing flags -l and --human-readable cannot be used with pointer inventory flags")
	}
	return nil
}

func init() {
	Cmd.Flags().StringVarP(&gitRemote, "git-remote", "r", "", "target remote Git server (default: origin)")
	Cmd.Flags().StringVarP(&drsRemote, "drs-remote", "d", "", "target remote DRS server (default: origin)")
	Cmd.Flags().StringArrayVarP(&includePatterns, "include", "I", nil, "include pathspec/glob pattern(s)")
	Cmd.Flags().BoolVar(&showLong, "long", false, "show full object IDs in pointer mode")
	Cmd.Flags().BoolVarP(&longListing, "long-listing", "l", false, "show file details and sizes")
	Cmd.Flags().BoolVar(&humanReadable, "human-readable", false, "show sizes in human-readable units with --long-listing")
	Cmd.Flags().BoolVarP(&nameOnly, "name-only", "n", false, "show only file paths")
	Cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	Cmd.Flags().BoolVar(&pointers, "pointers", false, "list tracked DRS/Git-LFS pointer files")
	Cmd.Flags().BoolVar(&drsStatus, "drs", false, "include DRS registration lookup details")
}
