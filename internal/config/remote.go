package config

type DRSRemote interface {
	GetProjectId() string
	GetOrganization() string
	GetEndpoint() string
	GetBucketName() string
	GetStoragePrefix() string
}

type RemoteSelect struct {
	Gen3  *Gen3Remote
	Local *LocalRemote
	Terra *TerraRemote
}

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
