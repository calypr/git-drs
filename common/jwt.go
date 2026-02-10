package common

import (
	"fmt"
	"net/url"

	"github.com/golang-jwt/jwt/v5"
)

func ParseEmailFromToken(tokenString string) (string, error) {
	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(tokenString, &claims)
	if err != nil {
		return "", fmt.Errorf("failed to decode token in ParseEmailFromToken: '%s': %w", tokenString, err)
	}
	context, ok := claims["context"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'context' claim structure")
	}
	user, ok := context["user"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'context.user' claim structure")
	}
	name, ok := user["name"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'context.user.name' claim")
	}
	return name, nil
}

func ParseAPIEndpointFromToken(tokenString string) (string, error) {
	claims := jwt.MapClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(tokenString, &claims)
	if err != nil {
		return "", fmt.Errorf("failed to decode token in ParseAPIEndpointFromToken: '%s': %w", tokenString, err)
	}
	issUrl, ok := claims["iss"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'iss' claim")
	}
	parsedURL, err := url.Parse(issUrl)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host), nil
}
