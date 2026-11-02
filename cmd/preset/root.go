package preset

import (
	"fmt"
	"sort"

	"github.com/calypr/git-drs/internal/presets"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{Use: "preset", Short: "Inspect built-in DRS server presets"}

var listCmd = &cobra.Command{Use: "list", Short: "List built-in presets", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
	items, err := presets.List()
	if err != nil {
		return err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Alias < items[j].Alias })
	for _, p := range items {
		fmt.Fprintf(cmd.OutOrStdout(), "%-10s %-8s %-30s %s  built-in v%d\n", p.Alias, p.Provider, p.Auth, p.Endpoint, presets.CatalogVersion)
	}
	return nil
}}

var showCmd = &cobra.Command{Use: "show <alias>", Short: "Show a built-in preset", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
	p, ok, err := presets.Lookup(args[0])
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("preset %q not found", args[0])
	}
	fmt.Fprintf(cmd.OutOrStdout(), "alias: %s\nendpoint: %s\nprovider: %s\nauth: %s\nsource: built-in\ncatalog-version: %d\n", p.Alias, p.Endpoint, p.Provider, p.Auth, presets.CatalogVersion)
	if p.RegistryServiceID != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "registry-service-id: %s\n", p.RegistryServiceID)
	}
	return nil
}}

func init() { Cmd.AddCommand(listCmd, showCmd) }
