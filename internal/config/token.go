package config

import (
	"fmt"
	"net/url"

	"github.com/golang-jwt/jwt/v5"
)

func ParseAPIEndpointFromToken(tokenString string) (string, error) {
	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(tokenString, &claims)
	if err != nil {
		return "", fmt.Errorf("failed to decode token in ParseAPIEndpointFromToken: '%s': %w", tokenString, err)
	}
	issURL, ok := claims["iss"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'iss' claim")
	}
	parsedURL, err := url.Parse(issURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host), nil
}
