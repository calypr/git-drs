package utils

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

func TestParseS3URL(t *testing.T) {
	bucket, key, err := ParseS3URL("s3://my-bucket/path/to/file.txt")
	if err != nil {
		t.Fatalf("ParseS3URL error: %v", err)
	}
	if bucket != "my-bucket" || key != "path/to/file.txt" {
		t.Fatalf("unexpected bucket/key: %s/%s", bucket, key)
	}
}

func TestParseS3URLErrors(t *testing.T) {
	t.Run("missing prefix", func(t *testing.T) {
		if _, _, err := ParseS3URL("http://bucket/key"); err == nil {
			t.Fatalf("expected error for missing s3 prefix")
		}
	})

	t.Run("missing key", func(t *testing.T) {
		if _, _, err := ParseS3URL("s3://bucket"); err == nil {
			t.Fatalf("expected error for missing key")
		}
	})

	t.Run("trailing slash", func(t *testing.T) {
		if _, _, err := ParseS3URL("s3://bucket/"); err == nil {
			t.Fatalf("expected error for trailing slash")
		}
	})
}

func TestSimpleRunCommandNotFound(t *testing.T) {
	if _, err := SimpleRun([]string{"command-that-does-not-exist-123"}); err == nil {
		t.Fatalf("expected error for missing command")
	}
}

//func TestGitTopLevelAndSimpleRun(t *testing.T) {
//	tmp := t.TempDir()
//	cmd := exec.Command("git", "init", tmp)
//	if out, err := cmd.CombinedOutput(); err != nil {
//		t.Fatalf("git init failed: %v: %s", err, string(out))
//	}
//
//	cwd, err := os.Getwd()
//	if err != nil {
//		t.Fatalf("getwd: %v", err)
//	}
//	if err := os.Chdir(tmp); err != nil {
//		t.Fatalf("chdir: %v", err)
//	}
//	t.Cleanup(func() {
//		_ = os.Chdir(cwd)
//	})
//
//	top, err := GitTopLevel()
//	if err != nil {
//		t.Fatalf("GitTopLevel error: %v", err)
//	}
//	if top != tmp {
//		t.Fatalf("expected top %s, got %s", tmp, top)
//	}
//
//	out, err := SimpleRun([]string{"git", "rev-parse", "--show-toplevel"})
//	if err != nil {
//		t.Fatalf("SimpleRun error: %v", err)
//	}
//	if out == "" {
//		t.Fatalf("expected output")
//	}
//}
