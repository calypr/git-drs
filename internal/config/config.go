package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/go-git/go-git/v5"
	"gopkg.in/yaml.v3"
)

// RemoteType represents the type of server being initialized
type RemoteType string
type Remote string

const (
	ORIGIN = "origin"

	Gen3ServerType  RemoteType = "gen3"
	LocalServerType RemoteType = "local"
	TerraServerType RemoteType = "terra"
	GA4GHServerType RemoteType = "ga4gh"

	configSection          = "drs"
	remoteSubsectionPrefix = "remote."
)

var ErrNoDefaultRemote = errors.New("no default remote configured")

const sharedPolicyPath = ".git-drs/drs-policies.yaml"

type sharedPolicy struct {
	Version int                           `yaml:"version"`
	Remotes map[string]sharedRemotePolicy `yaml:"remotes"`
}

type sharedRemotePolicy struct {
	Endpoint  string `yaml:"endpoint"`
	Provider  string `yaml:"provider"`
	Auth      string `yaml:"auth"`
	Scope     string `yaml:"scope"`
	Selection struct {
		AccessMethod string `yaml:"access_method"`
	} `yaml:"selection"`
	Transfer struct {
		Globus struct {
			AllowedSourceCollections *[]string `yaml:"allowed_source_collections"`
		} `yaml:"globus"`
	} `yaml:"transfer"`
}

func loadSharedPolicy(cfg *Config) error {
	root, err := gitrepo.GitTopLevel()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, sharedPolicyPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var policy sharedPolicy
	if err := decoder.Decode(&policy); err != nil {
		return fmt.Errorf("%s: %w", sharedPolicyPath, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%s: multiple YAML documents are not supported", sharedPolicyPath)
		}
		return fmt.Errorf("%s: %w", sharedPolicyPath, err)
	}
	if policy.Version != 1 {
		return fmt.Errorf("%s: unsupported version %d", sharedPolicyPath, policy.Version)
	}
	for name, shared := range policy.Remotes {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s: remote name must not be empty", sharedPolicyPath)
		}
		remote := RemoteSelect{AccessMethod: strings.TrimSpace(shared.Selection.AccessMethod), FromSharedPolicy: true}
		if remote.AccessMethod != "" {
			if _, err := parseSharedAccessPolicy(remote.AccessMethod); err != nil {
				return fmt.Errorf("%s: remotes.%s.selection.access_method: %w", sharedPolicyPath, name, err)
			}
		}
		endpoint := strings.TrimSpace(shared.Endpoint)
		provider := strings.TrimSpace(shared.Provider)
		if endpoint != "" || provider != "" {
			if endpoint == "" || provider == "" {
				return fmt.Errorf("%s: remotes.%s endpoint and provider must be set together", sharedPolicyPath, name)
			}
			u, err := url.ParseRequestURI(endpoint)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				return fmt.Errorf("%s: remotes.%s.endpoint must be an HTTPS URL without credentials", sharedPolicyPath, name)
			}
			remote.Generic = &GenericRemote{Endpoint: endpoint, Provider: provider, Auth: strings.TrimSpace(shared.Auth), Scope: strings.TrimSpace(shared.Scope)}
			remote.EndpointFromShared = true
		}
		if values := shared.Transfer.Globus.AllowedSourceCollections; values != nil {
			remote.AllowedGlobusSources = make([]string, 0, len(*values))
			seen := map[string]bool{}
			for _, value := range *values {
				value = strings.ToLower(strings.TrimSpace(value))
				if value == "" {
					return fmt.Errorf("%s: remotes.%s.transfer.globus.allowed_source_collections contains an empty collection ID", sharedPolicyPath, name)
				}
				if !seen[value] {
					seen[value] = true
					remote.AllowedGlobusSources = append(remote.AllowedGlobusSources, value)
				}
			}
		}
		cfg.Remotes[Remote(name)] = remote
	}
	if len(cfg.Remotes) == 1 {
		cfg.DefaultRemote = firstRemote(cfg)
	}
	return nil
}

func parseSharedAccessPolicy(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "auto" {
		return raw, nil
	}
	mode, method, ok := strings.Cut(raw, ":")
	if !ok || (mode != "prefer" && mode != "require") || (method != "https" && method != "globus") {
		return "", fmt.Errorf("invalid value %q; use auto, prefer:https, prefer:globus, require:https, or require:globus", raw)
	}
	return raw, nil
}

// Config holds the overall config structure
type Config struct {
	DefaultRemote Remote
	Remotes       map[Remote]RemoteSelect
}

func (c Config) GetRemote(remote Remote) DRSRemote {
	x, ok := c.Remotes[remote]
	if !ok {
		return nil
	}
	if x.Gen3 != nil {
		return x.Gen3
	} else if x.Local != nil {
		return x.Local
	} else if x.Terra != nil {
		return x.Terra
	} else if x.Generic != nil {
		return x.Generic
	}
	return nil
}

// GetDefaultRemote returns the configured default remote with validation
func (c Config) GetDefaultRemote() (Remote, error) {
	if c.DefaultRemote == "" {
		return "", fmt.Errorf(
			"%w.\n"+
				"Set one with: git drs remote set <name>\n"+
				"Available remotes: %v\n"+
				"Config: %v\n",
			ErrNoDefaultRemote,
			c.listRemoteNames(),
			c,
		)
	}

	if _, ok := c.Remotes[c.DefaultRemote]; !ok {
		return "", fmt.Errorf(
			"default remote '%s' not found in configuration.\n"+
				"Available remotes: %v",
			c.DefaultRemote,
			c.listRemoteNames(),
		)
	}

	return c.DefaultRemote, nil
}

// GetRemoteOrDefault returns the specified remote if provided, otherwise returns the default remote
func (c Config) GetRemoteOrDefault(remote string) (Remote, error) {
	if remote != "" {
		return Remote(remote), nil
	}
	return c.GetDefaultRemote()
}

// listRemoteNames returns a slice of all remote names for error messages
func (c Config) listRemoteNames() []string {
	names := make([]string, 0, len(c.Remotes))
	for name := range c.Remotes {
		names = append(names, string(name))
	}
	return names
}

// getRepo opens the current git repository
func getRepo() (*git.Repository, error) {
	return gitrepo.GetRepo()
}

// updates and git adds a Git DRS config file
// this should handle three cases:
// 1. create a new config file if it does not exist / is empty
// 2. return an error if the config file is invalid
// 3. update the existing config file, making sure to combine the new serversMap with the existing one
// UpdateRemote updates and saves configuration using go-git
func UpdateRemote(name Remote, remote RemoteSelect) (*Config, error) {
	repo, err := getRepo()
	if err != nil {
		return nil, err
	}

	conf, err := repo.Config()
	if err != nil {
		return nil, err
	}

	// Update drs.remote.<name> subsection
	remoteSubsectionName := fmt.Sprintf("%s%s", remoteSubsectionPrefix, name)
	remoteSubsection := conf.Raw.Section(configSection).Subsection(remoteSubsectionName)
	if remote.AccessMethod != "" {
		remoteSubsection.SetOption("access-method", remote.AccessMethod)
	}
	if remote.GlobusDefaultDestination != "" {
		remoteSubsection.SetOption("globus-default-destination", remote.GlobusDefaultDestination)
	}
	if len(remote.GlobusCollections) > 0 {
		remoteSubsection.SetOption("globus-collection", configMapEntries(remote.GlobusCollections)...)
	}
	if len(remote.GlobusDestinationPaths) > 0 {
		remoteSubsection.SetOption("globus-destination-path", configMapEntries(remote.GlobusDestinationPaths)...)
	}

	if remote.Gen3 != nil {
		remoteSubsection.SetOption("type", "gen3")
		remoteSubsection.SetOption("endpoint", remote.Gen3.Endpoint)
		remoteSubsection.SetOption("project", remote.Gen3.ProjectID)
		remoteSubsection.SetOption("bucket", remote.Gen3.Bucket)
		if remote.Gen3.Organization != "" {
			remoteSubsection.SetOption("organization", remote.Gen3.Organization)
		}
		if remote.Gen3.StoragePrefix != "" {
			remoteSubsection.SetOption("storage_prefix", remote.Gen3.StoragePrefix)
		}
	} else if remote.Terra != nil {
		remoteSubsection.SetOption("type", "terra")
		remoteSubsection.SetOption("endpoint", remote.Terra.Endpoint)
		if remote.Terra.Auth != "" {
			remoteSubsection.SetOption("auth", remote.Terra.Auth)
		}
		if remote.Terra.Mode != "" {
			remoteSubsection.SetOption("mode", remote.Terra.Mode)
		}
	} else if remote.Local != nil {
		remoteSubsection.SetOption("type", "local")
		remoteSubsection.SetOption("endpoint", remote.Local.BaseURL)
		if remote.Local.ProjectID != "" {
			remoteSubsection.SetOption("project", remote.Local.ProjectID)
		}
		if remote.Local.Bucket != "" {
			remoteSubsection.SetOption("bucket", remote.Local.Bucket)
		}
		if remote.Local.Organization != "" {
			remoteSubsection.SetOption("organization", remote.Local.Organization)
		}
		if remote.Local.StoragePrefix != "" {
			remoteSubsection.SetOption("storage_prefix", remote.Local.StoragePrefix)
		}
	} else if remote.Generic != nil {
		r := remote.Generic
		remoteSubsection.SetOption("type", "ga4gh")
		remoteSubsection.SetOption("endpoint", r.Endpoint)
		remoteSubsection.SetOption("provider", r.Provider)
		remoteSubsection.SetOption("auth", r.Auth)
		for key, value := range map[string]string{"credential": r.Credential, "scope": r.Scope, "storage": r.Storage, "checkout": r.Checkout, "preset": r.Preset, "registry-service-id": r.RegistryServiceID} {
			if value != "" {
				remoteSubsection.SetOption(key, value)
			}
		}
		if r.PresetVersion > 0 {
			remoteSubsection.SetOption("preset-version", fmt.Sprint(r.PresetVersion))
		}
	}

	// Set default remote if not set
	configRoot := conf.Raw.Section(configSection)
	defaultRemote := configRoot.Option("default-remote")
	if defaultRemote == "" {
		configRoot.SetOption("default-remote", string(name))
	}

	// Save config
	if err := repo.Storer.SetConfig(conf); err != nil {
		return nil, err
	}

	return LoadConfig()
}

func parseAndAddRemote(cfg *Config, subsectionName string, remoteType string, endpoint string, project string, bucket string, organization string, storagePrefix string, auth string, mode string) {
	if !strings.HasPrefix(subsectionName, remoteSubsectionPrefix) {
		return
	}

	remoteName := Remote(strings.TrimPrefix(subsectionName, remoteSubsectionPrefix))
	rs := RemoteSelect{}

	if remoteType == "gen3" || remoteType == "" {
		rs.Gen3 = &Gen3Remote{
			Endpoint:      endpoint,
			ProjectID:     project,
			Bucket:        bucket,
			Organization:  organization,
			StoragePrefix: storagePrefix,
		}
	} else if remoteType == "terra" {
		rs.Terra = &TerraRemote{
			Endpoint: endpoint,
			Auth:     auth,
			Mode:     mode,
		}
	} else if remoteType == "local" {
		rs.Local = &LocalRemote{
			BaseURL:       endpoint,
			ProjectID:     project,
			Bucket:        bucket,
			Organization:  organization,
			StoragePrefix: storagePrefix,
		}
	}

	cfg.Remotes[remoteName] = rs
}

func addGenericRemote(cfg *Config, name Remote, opts map[string]string) {
	version, _ := strconv.Atoi(opts["preset-version"])
	cfg.Remotes[name] = RemoteSelect{AccessMethod: opts["access-method"], Generic: &GenericRemote{
		Endpoint: opts["endpoint"], Provider: opts["provider"], Auth: opts["auth"],
		Credential: opts["credential"], Scope: opts["scope"], Storage: opts["storage"],
		Checkout: opts["checkout"], Preset: opts["preset"], PresetVersion: version,
		RegistryServiceID: opts["registry-service-id"],
	}}
}

func withGenericEndpoint(remote RemoteSelect, endpoint string) RemoteSelect {
	generic := *remote.Generic
	generic.Endpoint = endpoint
	remote.Generic = &generic
	return remote
}

func configMapEntries(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}

func parseConfigMap(option string, values []string) (map[string]string, error) {
	out := make(map[string]string, len(values))
	for _, value := range values {
		if strings.Count(value, "=") != 1 {
			return nil, fmt.Errorf("invalid %s value %q; expected <collection-id>=<value>", option, value)
		}
		key, mapped, ok := strings.Cut(strings.TrimSpace(value), "=")
		key, mapped = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(mapped)
		if !ok || key == "" || mapped == "" {
			return nil, fmt.Errorf("invalid %s value %q; expected <collection-id>=<value>", option, value)
		}
		if previous, exists := out[key]; exists && previous != mapped {
			return nil, fmt.Errorf("conflicting %s values for %q", option, key)
		}
		out[key] = mapped
	}
	return out, nil
}

func applyRemotePolicy(remote *RemoteSelect, accessMethod, defaultDestination string, collections, destinationPaths []string) error {
	if accessMethod = strings.TrimSpace(accessMethod); accessMethod != "" {
		remote.AccessMethod = accessMethod
	}
	remote.GlobusDefaultDestination = strings.TrimSpace(defaultDestination)
	var err error
	remote.GlobusCollections, err = parseConfigMap("globus-collection", collections)
	if err != nil {
		return err
	}
	remote.GlobusDestinationPaths, err = parseConfigMap("globus-destination-path", destinationPaths)
	return err
}

func last(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func loadGitConfigOverrides(cfg *Config) error {
	cmd := exec.Command("git", "config", "--local", "--get-regexp", `^drs\.`)
	out, err := cmd.Output()
	if err != nil {
		// git config exits non-zero when no matching keys exist. In that case,
		// the go-git result above is still the complete repository-local config.
		return nil
	}

	remoteOptions := make(map[Remote]map[string][]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if key == "drs.default-remote" {
			cfg.DefaultRemote = Remote(value)
			continue
		}
		const prefix = "drs.remote."
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		idx := strings.LastIndex(rest, ".")
		if idx <= 0 || idx == len(rest)-1 {
			continue
		}
		name := Remote(rest[:idx])
		option := rest[idx+1:]
		if remoteOptions[name] == nil {
			remoteOptions[name] = make(map[string][]string)
		}
		remoteOptions[name][option] = append(remoteOptions[name][option], value)
	}

	for name, opts := range remoteOptions {
		scalars := make(map[string]string, len(opts))
		for key, values := range opts {
			scalars[key] = last(values)
		}
		shared := cfg.Remotes[name]
		localEndpoint := scalars["type"] != "" || scalars["endpoint"] != ""
		if scalars["type"] == "ga4gh" {
			addGenericRemote(cfg, name, scalars)
		} else if scalars["type"] == "" && scalars["endpoint"] != "" && shared.Generic != nil {
			cfg.Remotes[name] = withGenericEndpoint(shared, scalars["endpoint"])
		} else if scalars["type"] != "" || scalars["endpoint"] != "" {
			parseAndAddRemote(
				cfg,
				remoteSubsectionPrefix+string(name),
				scalars["type"],
				scalars["endpoint"],
				scalars["project"],
				scalars["bucket"],
				scalars["organization"],
				scalars["storage_prefix"],
				scalars["auth"],
				scalars["mode"],
			)
		}
		remote := cfg.Remotes[name]
		if remote.Gen3 == nil && remote.Local == nil && remote.Terra == nil && remote.Generic == nil {
			remote.Gen3, remote.Local, remote.Terra, remote.Generic = shared.Gen3, shared.Local, shared.Terra, shared.Generic
		}
		if remote.AccessMethod == "" {
			remote.AccessMethod = shared.AccessMethod
		}
		remote.AllowedGlobusSources = shared.AllowedGlobusSources
		remote.FromSharedPolicy = shared.FromSharedPolicy
		if localEndpoint {
			remote.EndpointFromShared = false
		}
		if err := applyRemotePolicy(&remote, scalars["access-method"], scalars["globus-default-destination"], opts["globus-collection"], opts["globus-destination-path"]); err != nil {
			return fmt.Errorf("remote %q: %w", name, err)
		}
		cfg.Remotes[name] = remote
	}
	return nil
}

// LoadConfig loads configuration using go-git
func LoadConfig() (*Config, error) {
	repo, err := getRepo()
	if err != nil {
		return nil, err
	}

	conf, err := repo.Config()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Remotes: make(map[Remote]RemoteSelect),
	}
	if err := loadSharedPolicy(cfg); err != nil {
		return nil, err
	}
	// Iterate over all sections to find 'drs' and its subsections
	for _, section := range conf.Raw.Sections {
		if section.Name != configSection {
			continue
		}

		// Check for default-remote in the section root.
		dr := section.Option("default-remote")
		if dr != "" {
			cfg.DefaultRemote = Remote(dr)
		}

		for _, subsection := range section.Subsections {
			if !strings.HasPrefix(subsection.Name, remoteSubsectionPrefix) {
				continue
			}
			name := Remote(strings.TrimPrefix(subsection.Name, remoteSubsectionPrefix))
			shared := cfg.Remotes[name]
			localEndpoint := subsection.Option("type") != "" || subsection.Option("endpoint") != ""
			if subsection.Option("type") == "ga4gh" {
				opts := make(map[string]string)
				for _, key := range []string{"endpoint", "provider", "auth", "credential", "scope", "storage", "checkout", "preset", "preset-version", "registry-service-id", "access-method"} {
					opts[key] = subsection.Option(key)
				}
				addGenericRemote(cfg, Remote(strings.TrimPrefix(subsection.Name, remoteSubsectionPrefix)), opts)
			} else if subsection.Option("type") == "" && subsection.Option("endpoint") != "" && shared.Generic != nil {
				cfg.Remotes[name] = withGenericEndpoint(shared, subsection.Option("endpoint"))
			} else if subsection.Option("type") != "" || subsection.Option("endpoint") != "" {
				parseAndAddRemote(
					cfg,
					subsection.Name,
					subsection.Option("type"),
					subsection.Option("endpoint"),
					subsection.Option("project"),
					subsection.Option("bucket"),
					subsection.Option("organization"),
					subsection.Option("storage_prefix"),
					subsection.Option("auth"),
					subsection.Option("mode"),
				)
			}
			remote := cfg.Remotes[name]
			if remote.Gen3 == nil && remote.Local == nil && remote.Terra == nil && remote.Generic == nil {
				remote.Gen3, remote.Local, remote.Terra, remote.Generic = shared.Gen3, shared.Local, shared.Terra, shared.Generic
			}
			if remote.AccessMethod == "" {
				remote.AccessMethod = shared.AccessMethod
			}
			remote.AllowedGlobusSources = shared.AllowedGlobusSources
			remote.FromSharedPolicy = shared.FromSharedPolicy
			if localEndpoint {
				remote.EndpointFromShared = false
			}
			if err := applyRemotePolicy(&remote, subsection.Option("access-method"), subsection.Option("globus-default-destination"), subsection.OptionAll("globus-collection"), subsection.OptionAll("globus-destination-path")); err != nil {
				return nil, fmt.Errorf("remote %q: %w", name, err)
			}
			cfg.Remotes[name] = remote
		}
	}

	if err := loadGitConfigOverrides(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func CreateEmptyConfig() error {
	// With go-git, we just verify we are in a repo?
	// Existing behavior was ensuring file existence.
	// We can check if we can open the repo.
	_, err := getRepo()
	return err
}

// SaveConfig writes the configuration using go-git
func SaveConfig(cfg *Config) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}

	conf, err := repo.Config()
	if err != nil {
		return err
	}

	if cfg.DefaultRemote != "" {
		conf.Raw.Section(configSection).SetOption("default-remote", string(cfg.DefaultRemote))
	}

	return repo.Storer.SetConfig(conf)
}

func RemoveRemote(name Remote) (*Config, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	if _, ok := cfg.Remotes[name]; !ok {
		return nil, fmt.Errorf("remote '%s' not found", name)
	}

	delete(cfg.Remotes, name)

	keys := []string{
		fmt.Sprintf("drs.remote.%s.type", name),
		fmt.Sprintf("drs.remote.%s.endpoint", name),
		fmt.Sprintf("drs.remote.%s.project", name),
		fmt.Sprintf("drs.remote.%s.bucket", name),
		fmt.Sprintf("drs.remote.%s.organization", name),
		fmt.Sprintf("drs.remote.%s.storage_prefix", name),
		fmt.Sprintf("drs.remote.%s.auth", name),
		fmt.Sprintf("drs.remote.%s.mode", name),
		fmt.Sprintf("drs.remote.%s.provider", name),
		fmt.Sprintf("drs.remote.%s.credential", name),
		fmt.Sprintf("drs.remote.%s.scope", name),
		fmt.Sprintf("drs.remote.%s.storage", name),
		fmt.Sprintf("drs.remote.%s.checkout", name),
		fmt.Sprintf("drs.remote.%s.preset", name),
		fmt.Sprintf("drs.remote.%s.preset-version", name),
		fmt.Sprintf("drs.remote.%s.registry-service-id", name),
		fmt.Sprintf("drs.remote.%s.access-method", name),
		fmt.Sprintf("drs.remote.%s.globus-default-destination", name),
		fmt.Sprintf("drs.remote.%s.globus-collection", name),
		fmt.Sprintf("drs.remote.%s.globus-destination-path", name),
		fmt.Sprintf("drs.remote.%s.token", name),
		fmt.Sprintf("drs.remote.%s.username", name),
		fmt.Sprintf("drs.remote.%s.password", name),
		fmt.Sprintf("remote.%s.lfsurl", name),
	}
	if err := gitrepo.UnsetGitConfigOptions(keys); err != nil {
		return nil, err
	}

	if cfg.DefaultRemote == name {
		cfg.DefaultRemote = firstRemote(cfg)
	}

	if err := SaveConfig(cfg); err != nil {
		return nil, err
	}

	if cfg.DefaultRemote == "" {
		if err := gitrepo.UnsetGitConfigOptions([]string{"drs.default-remote"}); err != nil {
			return nil, err
		}
	}

	return LoadConfig()
}

func firstRemote(cfg *Config) Remote {
	if cfg == nil || len(cfg.Remotes) == 0 {
		return ""
	}

	names := make([]string, 0, len(cfg.Remotes))
	for name := range cfg.Remotes {
		names = append(names, string(name))
	}
	sort.Strings(names)
	return Remote(names[0])
}
