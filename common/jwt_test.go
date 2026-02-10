package common

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestParseEmailFromToken(t *testing.T) {
	claims := jwt.MapClaims{
		"context": map[string]any{
			"user": map[string]any{
				"name": "user@example.com",
			},
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	email, err := ParseEmailFromToken(tokenString)
	if err != nil {
		t.Fatalf("ParseEmailFromToken error: %v", err)
	}
	if email != "user@example.com" {
		t.Fatalf("expected user@example.com, got %s", email)
	}
}

func TestParseEmailFromTokenErrors(t *testing.T) {
	t.Run("invalid token", func(t *testing.T) {
		if _, err := ParseEmailFromToken("not-a-token"); err == nil {
			t.Fatalf("expected error for invalid token")
		}
	})

	t.Run("missing context", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})
		tokenString, err := token.SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		if _, err := ParseEmailFromToken(tokenString); err == nil {
			t.Fatalf("expected error for missing context")
		}
	})

	t.Run("missing user", func(t *testing.T) {
		claims := jwt.MapClaims{
			"context": map[string]any{},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		if _, err := ParseEmailFromToken(tokenString); err == nil {
			t.Fatalf("expected error for missing user")
		}
	})

	t.Run("missing name", func(t *testing.T) {
		claims := jwt.MapClaims{
			"context": map[string]any{
				"user": map[string]any{},
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		if _, err := ParseEmailFromToken(tokenString); err == nil {
			t.Fatalf("expected error for missing name")
		}
	})
}

func TestParseAPIEndpointFromToken(t *testing.T) {
	claims := jwt.MapClaims{
		"iss": "https://api.example.com/auth",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	endpoint, err := ParseAPIEndpointFromToken(tokenString)
	if err != nil {
		t.Fatalf("ParseAPIEndpointFromToken error: %v", err)
	}
	if endpoint != "https://api.example.com" {
		t.Fatalf("expected https://api.example.com, got %s", endpoint)
	}
}

func TestParseAPIEndpointFromTokenErrors(t *testing.T) {
	t.Run("missing iss", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})
		tokenString, err := token.SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		if _, err := ParseAPIEndpointFromToken(tokenString); err == nil {
			t.Fatalf("expected error for missing iss")
		}
	})

	t.Run("invalid url", func(t *testing.T) {
		claims := jwt.MapClaims{
			"iss": "://missing",
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		if _, err := ParseAPIEndpointFromToken(tokenString); err == nil {
			t.Fatalf("expected error for invalid url")
		}
	})
}
