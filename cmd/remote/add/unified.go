package add

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/calypr/git-drs/cmd/initialize"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/presets"
	"github.com/spf13/cobra"
)

var (
	scopeFlag, authFlag, credentialFlag, providerFlag string
	storageFlag, checkoutFlag                         string
)

func runUnified(cmd *cobra.Command, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("expected <endpoint-or-alias> or <name> <endpoint-or-alias>")
	}
	name, selector := "", args[0]
	if len(args) == 2 {
		name, selector = strings.TrimSpace(args[0]), args[1]
	}
	if strings.HasPrefix(selector, "registry:") {
		return fmt.Errorf("registry selector %q requires registry discovery, which is unavailable; pass the service HTTPS URL explicitly", selector)
	}

	preset, isPreset, err := presets.Lookup(selector)
	if err != nil {
		return err
	}
	endpoint, provider, auth, presetName, registryID, version := selector, "auto", "auto", "", "", 0
	if isPreset {
		endpoint, provider, auth = preset.Endpoint, preset.Provider, preset.Auth
		presetName, registryID, version = preset.Alias, preset.RegistryServiceID, presets.CatalogVersion
	}
	if providerFlag != "" && providerFlag != "auto" {
		provider = providerFlag
	}
	if authFlag != "" && authFlag != "auto" {
		auth = authFlag
	}
	if !validChoice(provider, "auto", "ga4gh", "gen3", "terra", "cgc", "synapse") {
		return fmt.Errorf("unsupported provider %q", provider)
	}
	if !validAuth(auth) {
		return fmt.Errorf("unsupported authentication method %q", auth)
	}
	u, err := url.ParseRequestURI(strings.TrimSpace(endpoint))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("endpoint must be an HTTPS URL without credentials, or a built-in alias")
	}
	if name == "" {
		name = deriveRemoteName(presetName, u.Hostname())
	}
	if name == "" {
		return fmt.Errorf("could not derive a remote name; use the explicit-name form")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`).MatchString(name) {
		return fmt.Errorf("invalid remote name %q", name)
	}
	if scopeFlag != "" {
		if _, _, err := parseScopeArg(scopeFlag); err != nil {
			return err
		}
	}
	if credentialFlag != "" && !validCredentialSource(credentialFlag) {
		return fmt.Errorf("invalid credential source %q; use env:, file:, helper:, profile:, or stdin", credentialFlag)
	}
	if checkoutFlag != "" && !validChoice(checkoutFlag, "pointers", "hydrate") {
		return fmt.Errorf("--checkout must be pointers or hydrate")
	}

	if err := initialize.EnsureInitialized(drslog.GetLogger()); err != nil {
		return fmt.Errorf("failed to initialize repository: %w", err)
	}
	if cfg, loadErr := config.LoadConfig(); loadErr == nil {
		if _, exists := cfg.Remotes[config.Remote(name)]; exists {
			return fmt.Errorf("remote %q already exists; choose an explicit name or remove it first", name)
		}
	}
	r := &config.GenericRemote{Endpoint: u.String(), Provider: provider, Auth: auth, Credential: credentialFlag, Scope: scopeFlag, Storage: storageFlag, Checkout: checkoutFlag, Preset: presetName, PresetVersion: version, RegistryServiceID: registryID}
	fmt.Fprintf(cmd.OutOrStdout(), "Remote: %s\nEndpoint: %s\nProvider: %s\nAuth: %s\n", name, r.Endpoint, r.Provider, r.Auth)
	if r.Preset != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Preset: %s (built-in catalog v%d)\n", r.Preset, r.PresetVersion)
	}
	if _, err := config.UpdateRemote(config.Remote(name), config.RemoteSelect{Generic: r}); err != nil {
		return fmt.Errorf("save remote: %w", err)
	}
	if err := configureRepoRemote(name, r.Endpoint); err != nil {
		return err
	}
	if checkoutFlag != "" {
		skip := "true"
		if checkoutFlag == "hydrate" {
			skip = "false"
		}
		if err := gitrepo.SetGitConfigOptions(map[string]string{"drs.skipsmudge": skip}); err != nil {
			return err
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Remote saved.")
	return nil
}

func deriveRemoteName(alias, host string) string {
	if alias != "" {
		return alias
	}
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "www.")
	return regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(host, "-")
}
func validChoice(v string, choices ...string) bool {
	for _, c := range choices {
		if v == c {
			return true
		}
	}
	return false
}
func validAuth(v string) bool {
	return validChoice(v, "auto", "none", "bearer", "basic", "google-adc", "provider-helper") || strings.HasPrefix(v, "provider-helper:")
}
func validCredentialSource(v string) bool {
	if v == "stdin" {
		return true
	}
	for _, p := range []string{"env:", "file:", "helper:", "profile:"} {
		if strings.HasPrefix(v, p) && len(v) > len(p) {
			return true
		}
	}
	return false
}
