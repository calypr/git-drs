package auth

import (
	"fmt"
	"strings"

	"github.com/calypr/git-drs/internal/globusauth"
	"github.com/spf13/cobra"
)

var globusScopes []string

var GlobusCmd = &cobra.Command{
	Use:   "globus [login|status|logout]",
	Short: "Log in to Globus and manage Transfer API authentication",
	Long:  fmt.Sprintf("Log in with Globus Auth and store refreshable credentials for the Transfer API. Set %s to your registered native application client ID before the first login. %s remains available as a non-persistent override for automation.", globusauth.ClientIDEnv, globusauth.TransferTokenEnv),
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		action := "login"
		if len(args) == 1 {
			action = strings.ToLower(strings.TrimSpace(args[0]))
		}
		switch action {
		case "login":
			name, err := globusauth.Login(cmd.Context(), globusScopes)
			if err != nil {
				return err
			}
			if err := globusauth.CheckStored(cmd.Context()); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Globus login successful; credentials stored in %s\n", name)
			return err
		case "status":
			if err := globusauth.CheckStored(cmd.Context()); err != nil {
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Globus Transfer API authentication OK")
			return err
		case "logout":
			if err := globusauth.Logout(); err != nil {
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Globus credentials removed")
			return err
		default:
			return fmt.Errorf("unknown Globus auth action %q; use login, status, or logout", action)
		}
	},
}

func init() {
	GlobusCmd.Flags().StringSliceVar(&globusScopes, "scope", nil, "Additional OAuth scope, such as a collection data_access scope (repeatable)")
}
