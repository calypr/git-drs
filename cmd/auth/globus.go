package auth

import (
	"fmt"

	"github.com/calypr/git-drs/internal/globusauth"
	"github.com/spf13/cobra"
)

var GlobusCmd = &cobra.Command{
	Use:   "globus",
	Short: "Verify Globus Transfer API authentication",
	Long:  fmt.Sprintf("Verify that git-drs can authenticate to the Globus Transfer API using the access token in %s. The token must include the %s scope, plus any collection-specific data_access dependent scopes required by the source or destination collections.", globusauth.TransferTokenEnv, globusauth.TransferScope),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return fmt.Errorf("error: accepts no arguments, received %d\n\nUsage: %s", len(args), cmd.UseLine())
		}
		identity, err := globusauth.Check(cmd.Context(), nil)
		if err != nil {
			return err
		}
		if identity == "" {
			identity = "authenticated"
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Globus Transfer API authentication OK: %s\n", identity)
		return err
	},
}
