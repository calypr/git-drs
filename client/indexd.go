package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"

	"github.com/bmeg/git-drs/drs"
)

type IndexDClient struct {
	base *url.URL
}

func NewIndexDClient(base string) (ObjectStoreClient, error) {
	baseURL, err := url.Parse(base)
	return &IndexDClient{baseURL}, err
}

// DownloadFile implements ObjectStoreClient.
func (cl *IndexDClient) DownloadFile(id string, dstPath string) (*drs.DRSObject, error) {
	panic("unimplemented")
}

// RegisterFile implements ObjectStoreClient.
func (cl *IndexDClient) RegisterFile(path string, name string) (*drs.DRSObject, error) {
	panic("unimplemented")
}

func (cl *IndexDClient) QueryID(id string) (*drs.DRSObject, error) {

	a := *cl.base
	a.Path = filepath.Join(a.Path, "drs/v1/objects", id)

	response, err := http.Get(a.String())
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	//log.Printf("Getting URL %s\n", a.String())
	//fmt.Printf("%s\n", string(body))

	out := drs.DRSObject{}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
