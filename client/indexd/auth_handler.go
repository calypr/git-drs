package indexd_client

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	token "github.com/bmeg/grip-graphql/middleware" //TODO: why this dependency?

	"github.com/calypr/data-client/client/commonUtils"
	"github.com/calypr/data-client/client/jwt"
)

// RealAuthHandler uses actual Gen3 authentication
type RealAuthHandler struct {
	Cred jwt.Credential
}

func GetJWTCredendial(config map[string]string) (jwt.Credential, error) {
	if api_key, ok := config["api_key"]; ok {
		if key_id, ok := config["key_id"]; ok {
			if endpoint, ok := config["endpoint"]; ok {
				c := jwt.Credential{
					APIKey:      api_key,
					KeyId:       key_id,
					APIEndpoint: endpoint,
				}
				r := jwt.Request{}
				err := r.RequestNewAccessToken(c.APIEndpoint+commonUtils.FenceAccessTokenEndpoint, &c)
				if err != nil {
					return c, fmt.Errorf("access token error: %s", err)
				}
				return c, err
			}
			return jwt.Credential{}, fmt.Errorf("endpoint info not in config")
		}
		return jwt.Credential{}, fmt.Errorf("key_id info not in config")
	}
	return jwt.Credential{}, fmt.Errorf("api_key info not in config")
}

func NewAuthHandler(config map[string]string) (*RealAuthHandler, error) {
	cred, err := GetJWTCredendial(config)
	return &RealAuthHandler{Cred: cred}, err
}

func (rh *RealAuthHandler) AddAuthHeader(req *http.Request, profile string) error {
	return rh.addGen3AuthHeader(req, profile)
}

func RefreshToken(cred *jwt.Credential) error {
	expiration, err := token.GetExpiration(cred.AccessToken)
	if err != nil {
		return err
	}
	// Update AccessToken if token is old
	if expiration.Before(time.Now()) {
		r := jwt.Request{}
		err = r.RequestNewAccessToken(cred.APIEndpoint+commonUtils.FenceAccessTokenEndpoint, cred)
		if err != nil {
			// load config and see if the endpoint is printed
			errStr := fmt.Sprintf("error refreshing access token: %v", err)
			if strings.Contains(errStr, "no such host") {
				errStr += ". If you are accessing an internal website, make sure you are connected to the internal network."
			}
			return errors.New(errStr)
		}
	}
	return nil
}

func (rh *RealAuthHandler) refreshToken() error {
	return RefreshToken(&rh.Cred)
}

func (rh *RealAuthHandler) addGen3AuthHeader(req *http.Request, profile string) error {
	err := rh.refreshToken()
	if err != nil {
		return err
	}
	// Add headers to the request
	authStr := "Bearer " + rh.Cred.AccessToken
	req.Header.Set("Authorization", authStr)
	return nil
}
