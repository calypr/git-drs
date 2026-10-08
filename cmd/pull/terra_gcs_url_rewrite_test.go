//go:build integration

package pull

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	gcsSigningAlgorithm = "GOOG4-RSA-SHA256"
	gcsSignedAuthority  = "http://127.0.0.1/"
	gcsSigningHost      = "storage.googleapis.com"
)

func rewriteTDRSignedURLForEmulator(response *http.Response, emulatorEndpoint string, publicKeyPEM []byte) error {
	if response == nil || response.Body == nil {
		return fmt.Errorf("TDR response has no body")
	}
	if !strings.HasPrefix(emulatorEndpoint, "http://127.0.0.1:") && !strings.HasPrefix(emulatorEndpoint, "http://host.docker.internal:") {
		return fmt.Errorf("invalid local GCS emulator endpoint %q", emulatorEndpoint)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read TDR response: %w", err)
	}
	_ = response.Body.Close()

	signedURL, err := signedURLFromJSON(body)
	if err != nil {
		return err
	}
	if signedURL.Scheme+"://"+signedURL.Host+"/" != gcsSignedAuthority {
		return fmt.Errorf("TDR signed URL authority is %q, want %q", signedURL.Scheme+"://"+signedURL.Host+"/", gcsSignedAuthority)
	}
	if err := verifyGCSV4SignedURL(signedURL, publicKeyPEM, gcsSigningHost, time.Now()); err != nil {
		return fmt.Errorf("verify TDR GCS signed URL: %w", err)
	}
	oldAuthority := signedURL.Scheme + "://" + signedURL.Host
	newAuthority := strings.TrimRight(emulatorEndpoint, "/")
	if bytes.Count(body, []byte(oldAuthority)) != 1 {
		return fmt.Errorf("TDR response must contain its signed URL authority exactly once")
	}
	body = bytes.Replace(body, []byte(oldAuthority), []byte(newAuthority), 1)
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	if response.Header == nil {
		response.Header = make(http.Header)
	}
	response.Header.Set("Content-Length", fmt.Sprint(len(body)))
	return nil
}

func signedURLFromJSON(body []byte) (*url.URL, error) {
	var document any
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("decode TDR access response: %w", err)
	}
	var signedURLs []*url.URL
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for _, child := range value {
				visit(child)
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		case string:
			parsed, err := url.Parse(value)
			if err == nil && parsed.IsAbs() && parsed.Query().Get("X-Goog-Algorithm") != "" {
				signedURLs = append(signedURLs, parsed)
			}
		}
	}
	visit(document)
	if len(signedURLs) != 1 {
		return nil, fmt.Errorf("TDR response contains %d V4 signed URLs, want exactly one", len(signedURLs))
	}
	return signedURLs[0], nil
}

func verifyGCSV4SignedURL(signedURL *url.URL, publicKeyPEM []byte, signingHost string, now time.Time) error {
	if signedURL == nil || signedURL.Scheme != "http" || signedURL.Host == "" || signedURL.Path == "" {
		return fmt.Errorf("signed URL must be an absolute HTTP URL with a host and path")
	}
	query := signedURL.Query()
	if query.Get("X-Goog-Algorithm") != gcsSigningAlgorithm {
		return fmt.Errorf("unsupported signing algorithm %q", query.Get("X-Goog-Algorithm"))
	}
	credential := query.Get("X-Goog-Credential")
	credentialParts := strings.SplitN(credential, "/", 2)
	if len(credentialParts) != 2 || credentialParts[0] == "" || credentialParts[1] == "" {
		return fmt.Errorf("signed URL has invalid credential scope %q", credential)
	}
	signedAt, err := time.Parse("20060102T150405Z", query.Get("X-Goog-Date"))
	if err != nil {
		return fmt.Errorf("signed URL has invalid signing time: %w", err)
	}
	expires, err := strconv.ParseInt(query.Get("X-Goog-Expires"), 10, 64)
	if err != nil || expires < 1 || expires > 604800 {
		return fmt.Errorf("signed URL has invalid expiration %q", query.Get("X-Goog-Expires"))
	}
	if now.Before(signedAt.Add(-5*time.Minute)) || !now.Before(signedAt.Add(time.Duration(expires)*time.Second)) {
		return fmt.Errorf("signed URL is not currently valid (signed %s, expires after %ds)", signedAt.UTC().Format(time.RFC3339), expires)
	}
	if query.Get("X-Goog-SignedHeaders") != "host" {
		return fmt.Errorf("signed URL signed headers are %q, want host", query.Get("X-Goog-SignedHeaders"))
	}
	signature, err := hex.DecodeString(query.Get("X-Goog-Signature"))
	if err != nil || len(signature) == 0 {
		return fmt.Errorf("signed URL has an invalid signature encoding")
	}
	publicKey, err := parseRSAPublicKey(publicKeyPEM)
	if err != nil {
		return err
	}
	if len(signature) != publicKey.Size() {
		return fmt.Errorf("signed URL signature is %d bytes, RSA key requires %d", len(signature), publicKey.Size())
	}

	canonicalRequest := strings.Join([]string{
		"GET",
		signedURL.EscapedPath(),
		canonicalGCSQuery(query),
		"host:" + signingHost + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	canonicalHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		gcsSigningAlgorithm,
		query.Get("X-Goog-Date"),
		credentialParts[1],
		hex.EncodeToString(canonicalHash[:]),
	}, "\n")
	digest := sha256.Sum256([]byte(stringToSign))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature); err != nil {
		return fmt.Errorf("RSA-SHA256 signature does not match the canonical request: %w", err)
	}
	return nil
}

func canonicalGCSQuery(query url.Values) string {
	type pair struct{ key, value string }
	pairs := make([]pair, 0, len(query))
	for key, values := range query {
		if key == "X-Goog-Signature" {
			continue
		}
		for _, value := range values {
			pairs = append(pairs, pair{rfc3986Escape(key), rfc3986Escape(value)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].key == pairs[j].key {
			return pairs[i].value < pairs[j].value
		}
		return pairs[i].key < pairs[j].key
	})
	parts := make([]string, len(pairs))
	for i, pair := range pairs {
		parts[i] = pair.key + "=" + pair.value
	}
	return strings.Join(parts, "&")
}

func rfc3986Escape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func parseRSAPublicKey(publicKeyPEM []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("TDR signing public key is not PEM encoded")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse TDR signing public key: %w", err)
	}
	publicKey, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("TDR signing public key is %T, want RSA", parsed)
	}
	return publicKey, nil
}
