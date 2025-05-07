package drs

type Checksum struct {
	Checksum string `json:"checksum"`
	Type     string `json:"type"`
}

type AccessURL struct {
	URL     string   `json:"url"`
	Headers []string `json:"headers"`
}

type Authorizations struct {
	//This structue is not stored in the file system
}

type AccessMethod struct {
	Type           string         `json:"type"`
	AccessURL      AccessURL      `json:"access_url"`
	AccessID       string         `json:"access_id"`
	Cloud          string         `json:"cloud"`
	Region         string         `json:"region"`
	Avalible       string         `json:"available"`
	Authorizations Authorizations `json:"Authorizations"`
}

type Contents struct {
}

type DRSObject struct {
	Id            string         `json:"id"`
	Name          string         `json:"name"`
	SelfURL       string         `json:"self_url"`
	Size          int64          `json:"size"`
	CreatedTime   string         `json:"created_time"`
	UpdatedTime   string         `json:"updated_time"`
	Version       string         `json:"version"`
	MimeType      string         `json:"mime_type"`
	Checksums     []Checksum     `json:"checksum"`
	AccessMethods []AccessMethod `json:"access_methods"`
	Contents      []Contents     `json:"contents"`
	Description   string         `json:"description"`
	Aliases       []string       `json:"aliases"`
}
