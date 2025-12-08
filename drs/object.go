package drs

import "github.com/calypr/git-drs/drs/hash"

type AccessURL struct {
	URL     string   `json:"url"`
	Headers []string `json:"headers"`
}

type Authorizations struct {
	//This structure is not stored in the file system
	Value string `json:"value"`
}

type AccessMethod struct {
	Type           string          `json:"type"`
	AccessURL      AccessURL       `json:"access_url"`
	AccessID       string          `json:"access_id,omitempty"`
	Cloud          string          `json:"cloud,omitempty"`
	Region         string          `json:"region,omitempty"`
	Avalible       string          `json:"available,omitempty"`
	Authorizations *Authorizations `json:"Authorizations,omitempty"`
}

type Contents struct {
}

type DRSPage struct {
	DRSObjects []DRSObject `json:"drs_objects"`
}

type DRSObjectResult struct {
	Object *DRSObject
	Error  error
}

type DRSObject struct {
	Id            string         `json:"id"`
	Name          string         `json:"name"`
	SelfURI       string         `json:"self_uri,omitempty"`
	Size          int64          `json:"size"`
	CreatedTime   string         `json:"created_time,omitempty"`
	UpdatedTime   string         `json:"updated_time,omitempty"`
	Version       string         `json:"version,omitempty"`
	MimeType      string         `json:"mime_type,omitempty"`
	Checksums     hash.HashInfo  `json:"checksums"`
	AccessMethods []AccessMethod `json:"access_methods"`
	Contents      []Contents     `json:"contents,omitempty"`
	Description   string         `json:"description,omitempty"`
	Aliases       []string       `json:"aliases,omitempty"`
}
