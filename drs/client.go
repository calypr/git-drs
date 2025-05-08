package drs

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
)

type Client struct {
	base *url.URL
}

func NewClient(base string) (*Client, error) {
	baseURL, err := url.Parse(base)
	return &Client{baseURL}, err
}

func (cl *Client) GetObject(id string) (*DRSObject, error) {

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

	out := DRSObject{}
	err = json.Unmarshal(body, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
