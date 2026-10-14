//go:build integration

package pull

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRewriteTDRSignedURLVerifiesV4SignatureAndExpiry(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: mustPKIXPublicKey(t, &privateKey.PublicKey)})
	now := time.Now().UTC().Truncate(time.Second)
	date := now.Format("20060102T150405Z")
	query := url.Values{
		"X-Goog-Algorithm":     {gcsSigningAlgorithm},
		"X-Goog-Credential":    {"git-drs-ci@fixture.invalid/" + now.Format("20060102") + "/auto/storage/goog4_request"},
		"X-Goog-Date":          {date},
		"X-Goog-Expires":       {"900"},
		"X-Goog-SignedHeaders": {"host"},
	}
	signedURL := &url.URL{Scheme: "http", Host: "127.0.0.1", Path: "/fixture-bucket/object"}
	signedURL.RawQuery = query.Encode()
	canonical := strings.Join([]string{
		"GET",
		signedURL.EscapedPath(),
		canonicalGCSQuery(query),
		"host:" + gcsSigningHost + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	canonicalHash := sha256.Sum256([]byte(canonical))
	stringToSign := strings.Join([]string{
		gcsSigningAlgorithm,
		date,
		now.Format("20060102") + "/auto/storage/goog4_request",
		hex.EncodeToString(canonicalHash[:]),
	}, "\n")
	digest := sha256.Sum256([]byte(stringToSign))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	query.Set("X-Goog-Signature", hex.EncodeToString(signature))
	signedURL.RawQuery = query.Encode()
	responseBody, err := json.Marshal(map[string]any{"accessUrl": map[string]any{"url": signedURL.String()}})
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{Body: io.NopCloser(bytes.NewReader(responseBody))}
	if err := rewriteTDRSignedURLForEmulator(response, "http://127.0.0.1:12345", publicKeyPEM); err != nil {
		t.Fatalf("valid signature should be accepted: %v", err)
	}
	rewrittenBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rewrittenBody), "http://127.0.0.1:12345/fixture-bucket/object?") {
		t.Fatalf("signed URL was not rewritten to emulator: %s", rewrittenBody)
	}

	for name, tamper := range map[string]func(*url.URL){
		"path": func(value *url.URL) { value.Path = "/fixture-bucket/other" },
		"signature": func(value *url.URL) {
			query := value.Query()
			signature := []byte(query.Get("X-Goog-Signature"))
			if signature[0] == '0' {
				signature[0] = '1'
			} else {
				signature[0] = '0'
			}
			query.Set("X-Goog-Signature", string(signature))
			value.RawQuery = query.Encode()
		},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := *signedURL
			tamper(&mutated)
			if err := verifyGCSV4SignedURL(&mutated, publicKeyPEM, gcsSigningHost, now); err == nil {
				t.Fatal("tampered signed URL was accepted")
			}
		})
	}
	if err := verifyGCSV4SignedURL(signedURL, publicKeyPEM, gcsSigningHost, now.Add(16*time.Minute)); err == nil {
		t.Fatal("expired signed URL was accepted")
	}
}

func mustPKIXPublicKey(t *testing.T, publicKey *rsa.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return der
}
