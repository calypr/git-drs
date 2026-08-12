package config

import "strings"

type DRSRemote interface {
	GetProjectId() string
	GetOrganization() string
	GetEndpoint() string
	GetBucketName() string
	GetStoragePrefix() string
}

type RemoteSelect struct {
	Gen3                     *Gen3Remote
	Local                    *LocalRemote
	Terra                    *TerraRemote
	Generic                  *GenericRemote
	AccessMethod             string
	GlobusDefaultDestination string
	GlobusCollections        map[string]string
	GlobusDestinationPaths   map[string]string
	// AllowedGlobusSources is nil when shared policy imposes no restriction.
	// A non-nil empty slice intentionally disables every Globus source.
	AllowedGlobusSources []string
	EndpointFromShared   bool
}

// GenericRemote is the compositional configuration produced by the unified
// remote-add command. Credential contains a source identifier, never a secret.
type GenericRemote struct {
	Endpoint, Provider, Auth, Credential, Scope, Storage, Checkout string
	Preset, RegistryServiceID                                      string
	PresetVersion                                                  int
}

func (r GenericRemote) GetProjectId() string     { _, p, _ := strings.Cut(r.Scope, "/"); return p }
func (r GenericRemote) GetOrganization() string  { o, _, _ := strings.Cut(r.Scope, "/"); return o }
func (r GenericRemote) GetEndpoint() string      { return r.Endpoint }
func (r GenericRemote) GetBucketName() string    { b, _, _ := strings.Cut(r.Storage, "/"); return b }
func (r GenericRemote) GetStoragePrefix() string { _, p, _ := strings.Cut(r.Storage, "/"); return p }

type Gen3Remote struct {
	Endpoint      string `yaml:"endpoint"`
	ProjectID     string `yaml:"project_id"`
	Bucket        string `yaml:"bucket"`
	Organization  string `yaml:"organization"`
	StoragePrefix string `yaml:"storage_prefix"`
}

func (s Gen3Remote) GetProjectId() string     { return s.ProjectID }
func (s Gen3Remote) GetOrganization() string  { return s.Organization }
func (s Gen3Remote) GetEndpoint() string      { return s.Endpoint }
func (s Gen3Remote) GetBucketName() string    { return s.Bucket }
func (s Gen3Remote) GetStoragePrefix() string { return s.StoragePrefix }

type TerraRemote struct {
	Endpoint string `yaml:"endpoint"`
	Auth     string `yaml:"auth"`
	Mode     string `yaml:"mode"`
}

func (t TerraRemote) GetProjectId() string     { return "" }
func (t TerraRemote) GetOrganization() string  { return "" }
func (t TerraRemote) GetEndpoint() string      { return t.Endpoint }
func (t TerraRemote) GetBucketName() string    { return "" }
func (t TerraRemote) GetStoragePrefix() string { return "" }

type LocalRemote struct {
	BaseURL       string
	ProjectID     string
	Bucket        string
	Organization  string
	StoragePrefix string
	BasicUsername string
	BasicPassword string
}

func (l LocalRemote) GetProjectId() string {
	if l.ProjectID != "" {
		return l.ProjectID
	}
	return "local-project"
}

func (l LocalRemote) GetOrganization() string  { return l.Organization }
func (l LocalRemote) GetEndpoint() string      { return l.BaseURL }
func (l LocalRemote) GetBucketName() string    { return l.Bucket }
func (l LocalRemote) GetStoragePrefix() string { return l.StoragePrefix }
