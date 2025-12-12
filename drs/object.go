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
	Available      string          `json:"available,omitempty"`
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

type OutputObject struct {
	Id            string          `json:"id"`
	Name          string          `json:"name"`
	SelfURI       string          `json:"self_uri,omitempty"`
	Size          int64           `json:"size"`
	CreatedTime   string          `json:"created_time,omitempty"`
	UpdatedTime   string          `json:"updated_time,omitempty"`
	Version       string          `json:"version,omitempty"`
	MimeType      string          `json:"mime_type,omitempty"`
	Checksums     []hash.Checksum `json:"checksums"`
	AccessMethods []AccessMethod  `json:"access_methods"`
	Contents      []Contents      `json:"contents,omitempty"`
	Description   string          `json:"description,omitempty"`
	Aliases       []string        `json:"aliases,omitempty"`
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

// ConvertOutputObjectToDRSObject converts the OutputObject struct to a DRSObject struct.
func ConvertOutputObjectToDRSObject(in *OutputObject) *DRSObject {
	if in == nil {
		return nil
	}

	// 1. Convert the slice of Checksum structs to the HashInfo struct.
	hashInfo := hash.ConvertChecksumsToHashInfo(in.Checksums)

	// 2. Map all fields directly.
	return &DRSObject{
		Id:          in.Id,
		Name:        in.Name,
		SelfURI:     in.SelfURI,
		Size:        in.Size,
		CreatedTime: in.CreatedTime,
		UpdatedTime: in.UpdatedTime,
		Version:     in.Version,
		MimeType:    in.MimeType,
		// The key conversion:
		Checksums: hashInfo,
		// Direct mapping for other fields:
		AccessMethods: in.AccessMethods,
		Contents:      in.Contents,
		Description:   in.Description,
		Aliases:       in.Aliases,
	}
}
