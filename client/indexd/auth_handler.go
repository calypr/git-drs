package indexd_client

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/calypr/data-client/client/common"
	"github.com/calypr/data-client/client/jwt"
	GoJwt "github.com/golang-jwt/jwt/v5"
)

// RealAuthHandler uses actual Gen3 authentication
type RealAuthHandler struct {
	Cred jwt.Credential
}

func (rh *RealAuthHandler) AddAuthHeader(req *http.Request) error {
	return rh.addGen3AuthHeader(req)
}

// Moved this function out of bmeg/grip-graphql/middleware into this repo to simplify deps.
func GetExpiration(tokenString string) (time.Time, error) {
	// Also consider trimming the 'Bearer ' prefix too
	tokenString = strings.TrimPrefix(tokenString, "bearer ")
	token, _, err := new(GoJwt.Parser).ParseUnverified(tokenString, GoJwt.MapClaims{})
	if err != nil {
		return time.Time{}, err
	}

	// Parse and convert from float64 epoch time to time.Time
	if claims, ok := token.Claims.(GoJwt.MapClaims); ok {
		if exp, ok := claims["exp"].(float64); ok {
			temp := int64(exp)
			exp := time.Unix(temp, 0)
			return exp, nil
		}
	}
	return time.Time{}, fmt.Errorf("expiration field 'exp' type float64 not found in token %v", token)
}

func RefreshToken(cred *jwt.Credential) error {
	expiration, err := GetExpiration(cred.AccessToken)
	if err != nil {
		return err
	}
	// Update AccessToken if token is old
	if expiration.Before(time.Now()) {
		r := jwt.Request{}
		err = r.RequestNewAccessToken(cred.APIEndpoint+common.FenceAccessTokenEndpoint, cred)
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

func (rh *RealAuthHandler) addGen3AuthHeader(req *http.Request) error {
	err := rh.refreshToken()
	if err != nil {
		return err
	}
	// Add headers to the request
	authStr := "Bearer " + rh.Cred.AccessToken
	req.Header.Set("Authorization", authStr)
	return nil
}
