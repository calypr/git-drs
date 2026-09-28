package lsfiles

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func printRows(cmd *cobra.Command, rows []fileRow) error {
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	for _, row := range rows {
		switch {
		case nameOnly:
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), row.Path); err != nil {
				return err
			}
		case drsStatus:
			oid := row.ShortOID
			if showLong {
				oid = row.OID
			}
			detail := row.Detail
			if detail == "" {
				detail = "-"
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\t%s\n", oid, row.Status, row.Path, detail); err != nil {
				return err
			}
		default:
			oid := row.ShortOID
			if showLong {
				oid = row.OID
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", oid, row.Status, row.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

func printBrowseListings(cmd *cobra.Command, listings []browseListing, longListing, humanReadable bool) error {
	showHeaders := false
	if len(listings) > 1 {
		for _, listing := range listings {
			if listing.directory {
				showHeaders = true
				break
			}
		}
	}
	wroteEntries := false
	for _, listing := range listings {
		if showHeaders && listing.directory {
			if wroteEntries {
				if _, err := fmt.Fprintln(cmd.OutOrStdout()); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s:\n", listing.operand); err != nil {
				return err
			}
			wroteEntries = true
		}
		for _, entry := range listing.entries {
			if longListing {
				size := formatBrowseSize(entry.size, humanReadable)
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %8s %s %s\n", entry.mode.String(), size, entry.modTime.Format("2006-01-02 15:04"), entry.name); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintln(cmd.OutOrStdout(), entry.name); err != nil {
				return err
			}
			wroteEntries = true
		}
	}
	return nil
}
