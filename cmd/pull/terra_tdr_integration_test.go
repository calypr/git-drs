//go:build integration

package pull

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/resolver"
)

func TestIntegrationPullAgainstTDRController(t *testing.T) {
	endpoint := os.Getenv("GIT_DRS_TDR_HTTP_ENDPOINT")
	if endpoint == "" {
		t.Skip("GIT_DRS_TDR_HTTP_ENDPOINT is required")
	}
	tdrURL, err := url.Parse(endpoint)
	if err != nil || tdrURL.Scheme != "http" || tdrURL.Host == "" {
		t.Fatalf("invalid local TDR endpoint %q", endpoint)
	}

	reverseProxy := httputil.NewSingleHostReverseProxy(tdrURL)
	var requestMu sync.Mutex
	var requestPaths []string
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMu.Lock()
		requestPaths = append(requestPaths, r.URL.Path)
		requestMu.Unlock()
		reverseProxy.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)
	oldResolver := newAnVILResolver
	t.Cleanup(func() {
		newAnVILResolver = oldResolver
		resetPullFlagsForTest()
	})
	newAnVILResolver = func(_ context.Context, endpoint string) (resolver.Resolver, error) {
		client := proxy.Client()
		base := client.Transport
		client.Transport = pullRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			authorized := req.Clone(req.Context())
			authorized.Header.Set("Authorization", "Bearer tdr-ci-fixture")
			return base.RoundTrip(authorized)
		})
		return resolver.NewAnVILWithClient(endpoint, client)
	}

	repo := t.TempDir()
	runGitCmdTest(t, repo, "init", "-q")
	t.Chdir(repo)
	const filename = "1614321.merge_output.gvcf.gz"
	const oid = "drs://drs.anv0:v2_4f770147-e372-339b-b9fa-0a7a83cf30cf"
	payload := []byte("git-drs Terra DRS HTTP integration fixture\n")
	if err := os.WriteFile(".gitattributes", []byte(filename+" filter=drs diff=drs merge=drs -text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pointer := fmt.Sprintf("version https://calypr.github.io/spec/v1\noid %s\nsize %d\n", oid, len(payload))
	if err := os.WriteFile(filename, []byte(pointer), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmdTest(t, repo, "add", filename)
	if _, err := config.UpdateRemote("anvil", config.RemoteSelect{Terra: &config.TerraRemote{Endpoint: proxy.URL, Mode: "read-only"}}); err != nil {
		t.Fatal(err)
	}
	resetPullFlagsForTest()
	includePatterns = []string{filename}
	requestMu.Lock()
	requestPaths = nil
	requestMu.Unlock()

	if err := Cmd.RunE(Cmd, nil); err != nil {
		t.Fatalf("pull through TDR controller: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(repo, filename))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("hydrated content = %q, want %q", got, payload)
	}
	requestMu.Lock()
	gotPaths := append([]string(nil), requestPaths...)
	requestMu.Unlock()
	wantPaths := []string{
		"/ga4gh/drs/v1/objects/v2_4f770147-e372-339b-b9fa-0a7a83cf30cf",
		"/ga4gh/drs/v1/objects/v2_4f770147-e372-339b-b9fa-0a7a83cf30cf/access/https-access",
	}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("TDR controller requests = %v, want %v", gotPaths, wantPaths)
	}
}
