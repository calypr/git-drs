package resolver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnVILResolverContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ga4gh/drs/v1/objects/object-1":
			_, _ = w.Write([]byte(`{"id":"object-1","size":4,"checksums":[{"type":"sha256","checksum":"abcd"}],"access_methods":[{"type":"https","access_id":"a1"}]}`))
		case "/ga4gh/drs/v1/objects/object-1/access/a1":
			_, _ = w.Write([]byte(`{"url":"https://storage.example/signed"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	r, err := NewAnVILWithClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	obj, err := r.GetObject(context.Background(), "DRS://AUTHORITY.EXAMPLE/object-1")
	if err != nil {
		t.Fatal(err)
	}
	if obj.DRSURI != "drs://authority.example/object-1" || obj.Size != 4 || obj.AccessMethods[0].AccessID != "a1" {
		t.Fatalf("unexpected object: %+v", obj)
	}
	access, err := r.GetAccess(context.Background(), obj.DRSURI, "a1")
	if err != nil || access.URL != "https://storage.example/signed" {
		t.Fatalf("unexpected access result: %+v, %v", access, err)
	}
}

func TestAnVILResolverAcceptsCompactDRSURI(t *testing.T) {
	const compactURI = "drs://drs.anv0:v2_e68887be-c583-375a-a773-48771192c8fa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ga4gh/drs/v1/objects/v2_e68887be-c583-375a-a773-48771192c8fa" {
			t.Fatalf("unexpected resolver path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"v2_e68887be-c583-375a-a773-48771192c8fa","size":42}`))
	}))
	defer server.Close()

	r, err := NewAnVILWithClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	obj, err := r.GetObject(context.Background(), compactURI)
	if err != nil {
		t.Fatalf("compact DRS URI should be valid: %v", err)
	}
	if obj.DRSURI != compactURI || obj.ID != "v2_e68887be-c583-375a-a773-48771192c8fa" {
		t.Fatalf("unexpected object: %+v", obj)
	}
}

func TestAnVILResolverRedactsErrorBodiesAndClassifiesAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Bearer secret signed=https://secret", http.StatusForbidden)
	}))
	defer server.Close()
	r, _ := NewAnVILWithClient(server.URL, server.Client())
	_, err := r.GetObject(context.Background(), "drs://example.org/object")
	if !errors.Is(err, ErrUnauthorized) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected redacted authorization error, got %v", err)
	}
}
