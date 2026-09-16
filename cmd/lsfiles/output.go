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
