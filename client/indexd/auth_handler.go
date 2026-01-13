package indexd_client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/calypr/data-client/client/api"
	"github.com/calypr/data-client/client/conf"
	"github.com/calypr/data-client/client/logs"
	"github.com/calypr/data-client/client/request"
	GoJwt "github.com/golang-jwt/jwt/v5"
)

// RealAuthHandler uses actual Gen3 authentication
type RealAuthHandler struct {
	Cred conf.Credential
}

func (rh *RealAuthHandler) AddAuthHeader(req *http.Request) error {
	return rh.addGen3AuthHeader(req.Context(), req)
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

// Kindof hackish. There exists logic to do this deeper in gen3-client but it is not exported
func RefreshToken(ctx context.Context, cred *conf.Credential) error {
	expiration, err := GetExpiration(cred.AccessToken)

	// Update AccessToken if token is old or there was an error getting the experation
	if err != nil || expiration.Before(time.Now()) {
		logger, closer := logs.New(cred.Profile)
		defer closer()
		conf := conf.NewConfigure(logger)
		r := api.NewFunctions(conf, request.NewRequestInterface(logger, cred, conf), cred, logger)
		f, ok := r.(*api.Functions)
		if !ok {
			return fmt.Errorf("Function interface is not of type api.Functions")
		}
		err := f.NewAccessToken(ctx)
		if err != nil {
			return err
		}
		cred.AccessToken = f.Cred.AccessToken
		err = conf.Save(cred)
		if err != nil {
			return err
		}
	}
	return nil
}

func (rh *RealAuthHandler) refreshToken(ctx context.Context) error {
	return RefreshToken(ctx, &rh.Cred)
}

func (rh *RealAuthHandler) addGen3AuthHeader(ctx context.Context, req *http.Request) error {
	err := rh.refreshToken(ctx)
	if err != nil {
		return err
	}
	// Add headers to the request
	authStr := "Bearer " + rh.Cred.AccessToken
	req.Header.Set("Authorization", authStr)
	return nil
}
