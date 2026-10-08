//go:build integration

package pull

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/resolver"
)

func TestIntegrationPullThroughTerraHubAndTDR(t *testing.T) {
	hubEndpoint := os.Getenv("GIT_DRS_HUB_HTTP_ENDPOINT")
	proxyLog := os.Getenv("GIT_DRS_TDR_PROXY_LOG")
	gcsEndpoint := os.Getenv("GIT_DRS_TDR_GCS_ENDPOINT")
	if hubEndpoint == "" || proxyLog == "" || gcsEndpoint == "" {
		t.Skip("GIT_DRS_HUB_HTTP_ENDPOINT, GIT_DRS_TDR_PROXY_LOG, and GIT_DRS_TDR_GCS_ENDPOINT are required")
	}
	parsed, err := url.Parse(hubEndpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" {
		t.Fatalf("invalid local Hub endpoint %q", hubEndpoint)
	}

	oldResolver := newHubResolver
	t.Cleanup(func() {
		newHubResolver = oldResolver
		resetPullFlagsForTest()
	})
	var hubCalls int
	newHubResolver = func(_ context.Context, endpoint string) (resolver.Resolver, error) {
		client := &http.Client{Transport: pullRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/api/v4/drs/resolve" {
				hubCalls++
			}
			copy := req.Clone(req.Context())
			copy.Header.Set("Authorization", "Bearer tdr-ci-fixture")
			response, err := http.DefaultTransport.RoundTrip(copy)
			if err != nil {
				return nil, err
			}
			if req.URL.Path == "/api/v4/drs/resolve" {
				if err := rewriteTDRSignedURLForEmulator(response, gcsEndpoint); err != nil {
					return nil, err
				}
			}
			return response, nil
		})}
		return resolver.NewTerraHubWithClient(endpoint, client)
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
	if _, err := config.UpdateRemote("anvil", config.RemoteSelect{Terra: &config.TerraRemote{
		Endpoint: "https://data.terra.bio", HubEndpoint: hubEndpoint, Mode: "read-only",
	}}); err != nil {
		t.Fatal(err)
	}
	objectsRoot, err := lfs.ResolveObjectsRoot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cachePath, err := lfs.ObjectPath(objectsRoot, oid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("cache must be empty before pull: %s: %v", cachePath, err)
	}
	resetPullFlagsForTest()
	includePatterns = []string{filename}
	if err := Cmd.RunE(Cmd, nil); err != nil {
		t.Fatalf("pull through Hub and TDR: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(repo, filename))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("hydrated content = %q, want %q", got, payload)
	}
	if hubCalls != 1 {
		t.Fatalf("Hub resolve calls = %d, want one", hubCalls)
	}
	data, err := os.ReadFile(proxyLog)
	if err != nil {
		t.Fatal(err)
	}
	var metadata, access bool
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var request struct {
			Method        string `json:"method"`
			Path          string `json:"path"`
			Authorization string `json:"authorization"`
		}
		if err := json.Unmarshal(line, &request); err != nil {
			t.Fatalf("invalid TDR proxy log line %q: %v", line, err)
		}
		if request.Method == "GET" && strings.HasSuffix(request.Path, "/objects/v2_4f770147-e372-339b-b9fa-0a7a83cf30cf") {
			metadata = request.Authorization == "Bearer tdr-ci-fixture"
		}
		if request.Method == "GET" && strings.Contains(request.Path, "/objects/v2_4f770147-e372-339b-b9fa-0a7a83cf30cf/access/") {
			access = request.Authorization == "Bearer tdr-ci-fixture"
		}
	}
	if !metadata || !access {
		t.Fatalf("Hub did not make authenticated TDR metadata and access requests: %s", data)
	}
}
