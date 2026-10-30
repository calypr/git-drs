package resolver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTerraHubResolverContract(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v4/drs/resolve" {
			t.Errorf("request = %s %s, want POST /api/v4/drs/resolve", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer hub-test-token" {
			t.Errorf("Authorization = %q, want test bearer", got)
		}
		var request terraHubRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.URL != "drs://drs.anv0:v2_fixture" {
			t.Errorf("request URL = %q", request.URL)
		}
		if strings.Join(request.Fields, ",") != "size,fileName,hashes,accessUrl" {
			t.Errorf("request fields = %v", request.Fields)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"size":43,"fileName":"fixture.vcf.gz","hashes":{"md5":"abc","crc32c":"def"},"accessUrl":{"url":"https://storage.example/object","headers":{"X-Fixture":"fixture-token","Accept":"application/octet-stream"}}}`))
	}))
	defer server.Close()

	client := server.Client()
	baseTransport := client.Transport
	client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		request.Header.Set("Authorization", "Bearer hub-test-token")
		return baseTransport.RoundTrip(request)
	})
	r, err := NewTerraHubWithClient(server.URL, client)
	if err != nil {
		t.Fatal(err)
	}
	object, err := r.GetObject(context.Background(), "drs://drs.anv0:v2_fixture")
	if err != nil {
		t.Fatal(err)
	}
	if object.ID != "v2_fixture" || object.DRSURI != "drs://drs.anv0:v2_fixture" || object.Name != "fixture.vcf.gz" || object.Size != 43 {
		t.Fatalf("object metadata = %+v", object)
	}
	if len(object.Checksums) != 2 || object.Checksums[0] != (Checksum{Type: "crc32c", Checksum: "def"}) || object.Checksums[1] != (Checksum{Type: "md5", Checksum: "abc"}) {
		t.Fatalf("object checksums = %+v", object.Checksums)
	}
	if len(object.AccessMethods) != 1 || object.AccessMethods[0].AccessURL == nil {
		t.Fatalf("access methods = %+v", object.AccessMethods)
	}
	access := object.AccessMethods[0].AccessURL
	if access.URL != "https://storage.example/object" || strings.Join(access.Headers, ",") != "Accept: application/octet-stream,X-Fixture: fixture-token" {
		t.Fatalf("access URL = %+v", access)
	}
}

func TestTerraHubHTTPClientAllowsOnlyLoopbackHTTP(t *testing.T) {
	client := &http.Client{}
	for _, endpoint := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		if _, err := NewTerraHubWithClient(endpoint, client); err != nil {
			t.Errorf("NewTerraHubWithClient(%q): %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{"http://hub.example", "https://hub.example/path?token=x", "https://user:pass@hub.example"} {
		if _, err := NewTerraHubWithClient(endpoint, client); err == nil {
			t.Errorf("NewTerraHubWithClient(%q) unexpectedly succeeded", endpoint)
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
