package indexd_client

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/calypr/data-client/client/conf"
	"github.com/golang-jwt/jwt/v5"
)

func TestGetExpiration(t *testing.T) {
	claims := jwt.MapClaims{
		"exp": float64(time.Now().Add(2 * time.Hour).Unix()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	exp, err := GetExpiration("bearer " + tokenString)
	if err != nil {
		t.Fatalf("GetExpiration error: %v", err)
	}
	if exp.Before(time.Now()) {
		t.Fatalf("expected future expiration, got %v", exp)
	}
}

func TestGetExpiration_MissingExp(t *testing.T) {
	claims := jwt.MapClaims{
		"sub": "user",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := GetExpiration(tokenString); err == nil {
		t.Fatalf("expected error for missing exp")
	}
}

func TestRealAuthHandler_AddAuthHeader(t *testing.T) {
	claims := jwt.MapClaims{
		"exp": float64(time.Now().Add(2 * time.Hour).Unix()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	rh := &RealAuthHandler{Cred: conf.Credential{AccessToken: tokenString}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	if err := rh.AddAuthHeader(req); err != nil {
		t.Fatalf("AddAuthHeader error: %v", err)
	}
	if req.Header.Get("Authorization") == "" {
		t.Fatalf("expected Authorization header")
	}
}
