//go:build integration

package pull

import (
	"bytes"
	"context"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"hash/crc32"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/lfs"
)

func TestIntegrationPullThroughTerraHubAndTDR(t *testing.T) {
	fixtures := []terraCLIObject{
		{
			filename: "1614321.merge_output.gvcf.gz",
			oid:      "drs://drs.anv0:v2_4f770147-e372-339b-b9fa-0a7a83cf30cf",
			payload:  []byte("git-drs Terra DRS HTTP integration fixture\n"),
		},
		{
			filename: "gregor-1614321.merge_output.gvcf.gz",
			oid:      "drs://drs.anv0:v2_c5ae75de-1f5c-3d40-bcd9-02f827fbf2d3",
			payload:  []byte("synthetic GREGoR 1614321 fixture; not genomic data\n"),
		},
		{
			filename: "1614322.merge_output.gvcf.gz",
			oid:      "drs://drs.anv0:v2_4872d195-f80a-3662-90a7-2aa90e0a7f53",
			payload:  []byte("synthetic GREGoR 1614322 fixture; not genomic data\n"),
		},
	}
	binary := os.Getenv("GIT_DRS_BINARY")
	hubEndpoint := os.Getenv("GIT_DRS_HUB_HTTP_ENDPOINT")
	proxyLog := os.Getenv("GIT_DRS_TDR_PROXY_LOG")
	gcsEndpoint := os.Getenv("GIT_DRS_TDR_GCS_ENDPOINT")
	keyPath := os.Getenv("GIT_DRS_TDR_SIGNING_PUBLIC_KEY_FILE")
	tdrHTTPEndpoint := os.Getenv("GIT_DRS_TDR_HTTP_ENDPOINT")
	if binary == "" || hubEndpoint == "" || proxyLog == "" || gcsEndpoint == "" || keyPath == "" || tdrHTTPEndpoint == "" {
		t.Skip("GIT_DRS_BINARY and the local Hub/TDR/GCS fixture endpoints are required")
	}
	publicKeyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read TDR signing public key: %v", err)
	}
	upstream, err := url.Parse(hubEndpoint)
	if err != nil || upstream.Scheme != "http" || upstream.Hostname() != "127.0.0.1" {
		t.Fatalf("invalid local Hub endpoint %q", hubEndpoint)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate mock ADC key: %v", err)
	}
	var tokenRequests atomic.Int32
	tokenListener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen for mock OAuth token endpoint: %v", err)
	}
	tokenEndpoint := fmt.Sprintf("http://host.docker.internal:%d", tokenListener.Addr().(*net.TCPAddr).Port)
	tokenServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || (r.URL.Path != "/token" && r.URL.Path != "/bad-token") {
			http.Error(w, "unexpected OAuth request", http.StatusNotFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			http.Error(w, "unexpected grant type", http.StatusBadRequest)
			return
		}
		parts := strings.Split(r.Form.Get("assertion"), ".")
		if len(parts) != 3 {
			http.Error(w, "invalid service-account JWT", http.StatusBadRequest)
			return
		}
		claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			http.Error(w, "invalid JWT claims", http.StatusBadRequest)
			return
		}
		var claims struct {
			Issuer   string `json:"iss"`
			Audience string `json:"aud"`
			Scope    string `json:"scope"`
		}
		if err := json.Unmarshal(claimsBytes, &claims); err != nil || claims.Issuer != "git-drs-ci@example.invalid" || claims.Audience != tokenEndpoint+r.URL.Path || !strings.Contains(claims.Scope, "cloud-platform") {
			http.Error(w, "unexpected service-account claims", http.StatusBadRequest)
			return
		}
		signature, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil {
			http.Error(w, "invalid JWT signature encoding", http.StatusBadRequest)
			return
		}
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
			http.Error(w, "service-account JWT signature invalid", http.StatusBadRequest)
			return
		}
		tokenRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		accessToken := "tdr-ci-fixture"
		if r.URL.Path == "/bad-token" {
			accessToken = "tdr-ci-invalid"
		}
		_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, accessToken)
	}))
	tokenServer.Listener = tokenListener
	tokenServer.Start()
	defer tokenServer.Close()

	var resolveRequests atomic.Int32
	var serviceInfoRequests atomic.Int32
	var resolvedFilesMu sync.Mutex
	resolvedFiles := make(map[string]int)
	var hubBearer atomic.Value
	frontListener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen for local Hub proxy: %v", err)
	}
	frontCertificate, frontCertificatePEM, err := makeDockerHostCertificate()
	if err != nil {
		t.Fatalf("make local Hub TLS certificate: %v", err)
	}
	frontProxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destination := "http://" + upstream.Host + r.URL.RequestURI()
		if r.URL.Path == "/ga4gh/drs/v1/service-info" {
			destination = strings.TrimRight(tdrHTTPEndpoint, "/") + r.URL.RequestURI()
			serviceInfoRequests.Add(1)
		}
		forwarded, err := http.NewRequestWithContext(r.Context(), r.Method, destination, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		forwarded.Header = r.Header.Clone()
		if r.URL.Path == "/ga4gh/drs/v1/service-info" {
			forwarded.Host = "127.0.0.1"
		} else {
			forwarded.Host = upstream.Host
		}
		response, err := http.DefaultTransport.RoundTrip(forwarded)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		if r.URL.Path == "/api/v4/drs/resolve" {
			resolveRequests.Add(1)
			hubBearer.Store(r.Header.Get("Authorization"))
			if response.StatusCode == http.StatusOK {
				filename, err := verifyTerraHubMetadata(response, fixtures)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
				resolvedFilesMu.Lock()
				resolvedFiles[filename]++
				resolvedFilesMu.Unlock()
				if err := rewriteTDRSignedURLForEmulator(response, gcsEndpoint, publicKeyPEM); err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
			}
		}
		for name, values := range response.Header {
			if strings.EqualFold(name, "Connection") || strings.EqualFold(name, "Transfer-Encoding") {
				continue
			}
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	frontProxy.Listener = frontListener
	frontProxy.TLS = &tls.Config{Certificates: []tls.Certificate{frontCertificate}, MinVersion: tls.VersionTLS12}
	frontProxy.StartTLS()
	defer frontProxy.Close()
	frontProxyURL := fmt.Sprintf("https://host.docker.internal:%d", frontListener.Addr().(*net.TCPAddr).Port)
	terraServiceEndpoint := fmt.Sprintf("https://data.terra.bio:%d", frontListener.Addr().(*net.TCPAddr).Port)

	credentialsPath := filepath.Join(t.TempDir(), "service-account.json")
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal ADC key: %v", err)
	}
	credentials, err := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "git-drs-ci", "private_key_id": "git-drs-ci-key",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})),
		"client_email": "git-drs-ci@example.invalid", "client_id": "1234567890",
		"token_uri": tokenEndpoint + "/token", "universe_domain": "googleapis.com",
	})
	if err != nil {
		t.Fatalf("encode ADC credentials: %v", err)
	}
	if err := os.WriteFile(credentialsPath, credentials, 0o600); err != nil {
		t.Fatalf("write ADC credentials: %v", err)
	}
	certPath := filepath.Join(t.TempDir(), "hub-proxy-ca.pem")
	if err := os.WriteFile(certPath, frontCertificatePEM, 0o600); err != nil {
		t.Fatalf("write Hub proxy TLS certificate: %v", err)
	}

	repo := t.TempDir()
	runGitCmdTest(t, repo, "init", "-q")
	runGitCmdTest(t, repo, "config", "user.name", "git-drs CI fixture")
	runGitCmdTest(t, repo, "config", "user.email", "git-drs-ci@example.invalid")
	var pointers = make(map[string]string, len(fixtures))
	attributes := make([]string, 0, len(fixtures))
	for _, fixture := range fixtures {
		pointer := fmt.Sprintf("version https://calypr.github.io/spec/v1\noid %s\nsize %d\n", fixture.oid, len(fixture.payload))
		pointers[fixture.filename] = pointer
		attributes = append(attributes, fixture.filename+" filter=drs diff=drs merge=drs -text")
		if err := os.WriteFile(filepath.Join(repo, fixture.filename), []byte(pointer), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte(strings.Join(attributes, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		runGitCmdTest(t, repo, "add", fixture.filename)
	}
	t.Chdir(repo)
	objectsRoot, err := lfs.ResolveObjectsRoot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cachePaths := make(map[string]string, len(fixtures))
	for _, fixture := range fixtures {
		cachePath, err := lfs.ObjectPath(objectsRoot, fixture.oid)
		if err != nil {
			t.Fatal(err)
		}
		cachePaths[fixture.filename] = cachePath
		if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
			t.Fatalf("cache must be empty before pull: %s: %v", cachePath, err)
		}
	}

	runContainer := func(credentialsFile, executable string, arguments ...string) ([]byte, error) {
		commandLine := `mkdir -p /tmp/git-drs-ci-home && git config --global --add safe.directory /repo && git config --global filter.drs.clean "/usr/local/bin/git-drs clean -- %f" && git config --global filter.drs.smudge "/usr/local/bin/git-drs smudge -- %f" && git config --global filter.drs.process "/usr/local/bin/git-drs filter" && git config --global filter.drs.required true && exec ` + shellQuote(executable)
		for _, argument := range arguments {
			commandLine += " " + shellQuote(argument)
		}
		command := exec.Command("docker", "run", "--rm",
			"--add-host", "host.docker.internal:host-gateway",
			"--add-host", "data.terra.bio:host-gateway",
			"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
			"--volume", binary+":/usr/local/bin/git-drs:ro",
			"--volume", repo+":/repo",
			"--volume", credentialsFile+":/ci/service-account.json:ro",
			"--volume", certPath+":/ci/hub-proxy-ca.pem:ro",
			"--workdir", "/repo",
			"--env", "HOME=/tmp/git-drs-ci-home",
			"--env", "GIT_CONFIG_NOSYSTEM=1",
			"--env", "GOOGLE_APPLICATION_CREDENTIALS=/ci/service-account.json",
			"--env", "SSL_CERT_FILE=/ci/hub-proxy-ca.pem",
			"--entrypoint", "/bin/sh", "alpine/git@sha256:062a01ad7a0eb17cff382bc5e26086b4d710e56dfdfdf001109a49b6d9bd378c", "-c",
			commandLine)
		return command.CombinedOutput()
	}
	runCLIResult := func(credentialsFile string, arguments ...string) ([]byte, error) {
		return runContainer(credentialsFile, "/usr/local/bin/git-drs", arguments...)
	}
	runCLI := func(arguments ...string) []byte {
		output, err := runCLIResult(credentialsPath, arguments...)
		if err != nil {
			t.Fatalf("git-drs %s exited with %v:\n%s", strings.Join(arguments, " "), err, output)
		}
		return output
	}
	runGit := func(arguments ...string) []byte {
		output, err := runContainer(credentialsPath, "git", arguments...)
		if err != nil {
			t.Fatalf("git %s exited with %v:\n%s", strings.Join(arguments, " "), err, output)
		}
		return output
	}
	remoteOutput := runCLI("remote", "add", "anvil", terraServiceEndpoint, "--provider", "terra", "--auth", "google-adc", "--hub-endpoint", frontProxyURL)
	if !strings.Contains(string(remoteOutput), "Remote saved.") {
		t.Fatalf("remote add output does not confirm configuration:\n%s", remoteOutput)
	}
	runCLI("remote", "set", "anvil")
	remoteList := runCLI("remote", "list")
	if !strings.Contains(string(remoteList), "* anvil      terra") || !strings.Contains(string(remoteList), terraServiceEndpoint) {
		t.Fatalf("remote list does not report the selected Terra remote:\n%s", remoteList)
	}
	pingOutput := runCLI("ping", "anvil")
	if !strings.Contains(string(pingOutput), "health: ok") || !strings.Contains(string(pingOutput), "service-info:") {
		t.Fatalf("Terra ping did not report service health and service-info:\n%s", pingOutput)
	}
	if got := serviceInfoRequests.Load(); got != 1 {
		t.Fatalf("Terra ping service-info requests = %d, want one real TDR request", got)
	}
	selective := runCLI("pull", "-I", fixtures[1].filename)
	assertFileBytes(t, repo, fixtures[1], selective)
	if _, err := os.Stat(cachePaths[fixtures[0].filename]); !os.IsNotExist(err) {
		t.Fatalf("selective pull populated an unselected cache object: %v", err)
	}
	if got := tokenRequests.Load(); got != 1 {
		t.Fatalf("ADC token exchanges after selective pull = %d, want one", got)
	}
	assertResolvedCounts(t, &resolvedFilesMu, resolvedFiles, map[string]int{fixtures[1].filename: 1})
	if got := resolveRequests.Load(); got != 1 {
		t.Fatalf("Hub resolve requests after selective pull = %d, want one", got)
	}
	if got := hubBearer.Load(); got != "Bearer tdr-ci-fixture" {
		t.Fatalf("Hub received Authorization %v, want ADC fixture token", got)
	}
	assertStagedPointers(t, repo, fixtures, pointers)
	assertCachedObject(t, cachePaths[fixtures[1].filename], fixtures[1])
	for _, unselected := range []terraCLIObject{fixtures[0], fixtures[2]} {
		if _, err := os.Stat(cachePaths[unselected.filename]); !os.IsNotExist(err) {
			t.Fatalf("selective pull unexpectedly cached %s: %v", unselected.filename, err)
		}
	}

	allPull := runCLI("pull")
	for _, fixture := range fixtures {
		assertFileBytes(t, repo, fixture, allPull)
	}
	assertStagedPointers(t, repo, fixtures, pointers)
	assertCachedObjects(t, cachePaths, fixtures)
	if got := tokenRequests.Load(); got != 2 {
		t.Fatalf("ADC token exchanges after all-files pull = %d, want two total", got)
	}
	if got := resolveRequests.Load(); got != 3 {
		t.Fatalf("Hub resolve requests after all-files pull = %d, want three total", got)
	}
	assertResolvedCounts(t, &resolvedFilesMu, resolvedFiles, map[string]int{
		fixtures[1].filename: 1,
		fixtures[0].filename: 1,
		fixtures[2].filename: 1,
	})
	proxyLogBytes, err := os.ReadFile(proxyLog)
	if err != nil {
		t.Fatal(err)
	}
	metadataRequests := make(map[string]int)
	accessRequests := make(map[string]int)
	probes := make(map[string]int)
	for _, line := range bytes.Split(bytes.TrimSpace(proxyLogBytes), []byte("\n")) {
		var request struct {
			Method        string `json:"method"`
			Path          string `json:"path"`
			Authorization string `json:"authorization"`
			Status        int    `json:"status"`
		}
		if err := json.Unmarshal(line, &request); err != nil {
			t.Fatalf("invalid TDR proxy log line %q: %v", line, err)
		}
		objectID := fixtureIDForProxyPath(request.Path, fixtures)
		if objectID == "" {
			t.Fatalf("unexpected TDR request path in proxy log: %s", line)
		}
		if request.Method == "OPTIONS" {
			if request.Authorization != "" || request.Status != http.StatusOK {
				t.Fatalf("invalid unauthenticated TDR OPTIONS request: %s", line)
			}
			probes[objectID]++
		}
		if request.Method == "GET" && !strings.Contains(request.Path, "/access/") {
			if request.Authorization != "Bearer tdr-ci-fixture" || request.Status != http.StatusOK {
				t.Fatalf("invalid TDR metadata request: %s", line)
			}
			metadataRequests[objectID]++
		}
		if request.Method == "GET" && strings.Contains(request.Path, "/access/") {
			if request.Authorization != "Bearer tdr-ci-fixture" || request.Status != http.StatusOK {
				t.Fatalf("invalid TDR access request: %s", line)
			}
			accessRequests[objectID]++
		}
	}
	for _, fixture := range fixtures {
		objectID := fixtureObjectID(fixture)
		if probes[objectID] != 1 || metadataRequests[objectID] != 1 || accessRequests[objectID] != 1 {
			t.Fatalf("TDR request counts for %s = OPTIONS:%d metadata:%d access:%d, want one each; log:\n%s", objectID, probes[objectID], metadataRequests[objectID], accessRequests[objectID], proxyLogBytes)
		}
	}

	// Git checkout reads the staged pointer through the configured process filter.
	if err := os.Remove(filepath.Join(repo, fixtures[1].filename)); err != nil {
		t.Fatal(err)
	}
	checkout := runGit("checkout", "--", fixtures[1].filename)
	assertFileBytes(t, repo, fixtures[1], checkout)
	secondOutput := runCLI("pull")
	for _, fixture := range fixtures {
		assertFileBytes(t, repo, fixture, secondOutput)
	}
	if got := tokenRequests.Load(); got != 2 {
		t.Fatalf("ADC token exchanges after cache-only pull/smudge = %d, want two", got)
	}
	if got := resolveRequests.Load(); got != 3 {
		t.Fatalf("Hub resolve requests after cache-only pull/smudge = %d, want three", got)
	}
	secondProxyLog, err := os.ReadFile(proxyLog)
	if err != nil || !bytes.Equal(secondProxyLog, proxyLogBytes) {
		t.Fatalf("cached pull or checkout smudge contacted TDR: before %d bytes, after %d bytes (error %v)", len(proxyLogBytes), len(secondProxyLog), err)
	}

	manifest := "drs_uri\tpath\n" + fixtures[1].oid + "\trefs/gregor-1614321-copy.gz\n" + fixtures[2].oid + "\trefs/gregor-1614322-copy.gz\n"
	if err := os.WriteFile(filepath.Join(repo, "terra-references.tsv"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	addRefs := runCLI("add-ref", "--manifest", "terra-references.tsv")
	if !strings.Contains(string(addRefs), "2 reference(s) added") && !strings.Contains(string(addRefs), "2 reference(s) validated") {
		t.Fatalf("add-ref manifest output does not report both refs:\n%s", addRefs)
	}
	for i, path := range []string{"refs/gregor-1614321-copy.gz", "refs/gregor-1614322-copy.gz"} {
		wantPointer := pointers[fixtures[i+1].filename]
		worktreePointer, err := os.ReadFile(filepath.Join(repo, path))
		if err != nil || string(worktreePointer) != wantPointer {
			t.Fatalf("add-ref worktree pointer for %s = %q, want %q (error %v)", path, worktreePointer, wantPointer, err)
		}
		runGit("add", path)
		got := runGitOutputTest(t, repo, "show", ":"+path)
		if got != wantPointer {
			t.Fatalf("staged add-ref pointer for %s = %q, want %q", path, got, wantPointer)
		}
	}
	singleRefPath := "refs/single-1614321-copy.gz"
	singleRef := runCLI("add-ref", fixtures[0].oid, singleRefPath)
	if len(strings.TrimSpace(string(singleRef))) != 0 {
		t.Fatalf("single add-ref returned unexpected output:\n%s", singleRef)
	}
	singleWorktreePointer, err := os.ReadFile(filepath.Join(repo, singleRefPath))
	if err != nil || string(singleWorktreePointer) != pointers[fixtures[0].filename] {
		t.Fatalf("single add-ref worktree pointer = %q, want %q (error %v)", singleWorktreePointer, pointers[fixtures[0].filename], err)
	}
	runGit("add", singleRefPath)
	if got := runGitOutputTest(t, repo, "show", ":"+singleRefPath); got != pointers[fixtures[0].filename] {
		t.Fatalf("single add-ref staged pointer = %q, want %q", got, pointers[fixtures[0].filename])
	}
	if got := tokenRequests.Load(); got != 5 {
		t.Fatalf("ADC token exchanges after add-ref operations = %d, want five total (one per resolver)", got)
	}
	if got := resolveRequests.Load(); got != 6 {
		t.Fatalf("Hub resolve requests after add-ref operations = %d, want six total", got)
	}
	assertResolvedCounts(t, &resolvedFilesMu, resolvedFiles, map[string]int{
		fixtures[1].filename: 2,
		fixtures[0].filename: 2,
		fixtures[2].filename: 2,
	})
	finalProxyLog, err := os.ReadFile(proxyLog)
	if err != nil {
		t.Fatal(err)
	}
	finalMetadata, finalAccess, finalProbes := countTDRRequests(t, finalProxyLog, fixtures)
	for _, fixture := range fixtures {
		objectID := fixtureObjectID(fixture)
		want := 2
		if finalMetadata[objectID] != want || finalAccess[objectID] != want || finalProbes[objectID] != want {
			t.Fatalf("final TDR request counts for %s = OPTIONS:%d metadata:%d access:%d, want %d each", objectID, finalProbes[objectID], finalMetadata[objectID], finalAccess[objectID], want)
		}
	}

	var badCredentialDocument map[string]string
	if err := json.Unmarshal(credentials, &badCredentialDocument); err != nil {
		t.Fatal(err)
	}
	badCredentialDocument["token_uri"] = tokenEndpoint + "/bad-token"
	badCredentials, err := json.Marshal(badCredentialDocument)
	if err != nil {
		t.Fatal(err)
	}
	badCredentialsPath := filepath.Join(t.TempDir(), "invalid-service-account.json")
	if err := os.WriteFile(badCredentialsPath, badCredentials, 0o600); err != nil {
		t.Fatal(err)
	}

	// Remove the already verified object so this request must authenticate and resolve remotely.
	if err := os.Remove(filepath.Join(repo, fixtures[0].filename)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, fixtures[0].filename), []byte(pointers[fixtures[0].filename]), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cachePaths[fixtures[0].filename]); err != nil {
		t.Fatalf("remove verified cache object before unauthorized pull: %v", err)
	}
	badOutput, badErr := runCLIResult(badCredentialsPath, "pull", "-I", fixtures[0].filename)
	if badErr == nil {
		t.Fatalf("pull with an invalid fixture bearer token unexpectedly succeeded:\n%s", badOutput)
	}
	if lower := strings.ToLower(string(badOutput)); !strings.Contains(lower, "401") && !strings.Contains(lower, "unauthorized") && !strings.Contains(lower, "not authorized") && !strings.Contains(lower, "invalid fixture bearer token") {
		t.Fatalf("unauthorized pull did not report the authentication failure clearly (exit %v):\n%s", badErr, badOutput)
	}
	if got, err := os.ReadFile(filepath.Join(repo, fixtures[0].filename)); err != nil || string(got) != pointers[fixtures[0].filename] {
		t.Fatalf("unauthorized pull changed worktree bytes: got %q, want pointer %q (error %v)", got, pointers[fixtures[0].filename], err)
	}
	if _, err := os.Stat(cachePaths[fixtures[0].filename]); !os.IsNotExist(err) {
		t.Fatalf("unauthorized pull wrote an object to cache: %v", err)
	}
	if got := runGitOutputTest(t, repo, "show", ":"+fixtures[0].filename); got != pointers[fixtures[0].filename] {
		t.Fatalf("unauthorized pull changed staged pointer: got %q, want %q", got, pointers[fixtures[0].filename])
	}
	if got := tokenRequests.Load(); got != 6 {
		t.Fatalf("ADC exchanges after unauthorized pull = %d, want six total", got)
	}
	if got := resolveRequests.Load(); got != 7 {
		t.Fatalf("Hub resolve requests after unauthorized pull = %d, want seven total", got)
	}
	if got := hubBearer.Load(); got != "Bearer tdr-ci-invalid" {
		t.Fatalf("Hub received Authorization %v during unauthorized pull, want invalid fixture token", got)
	}
	unauthorizedProxyLog, err := os.ReadFile(proxyLog)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(unauthorizedProxyLog, finalProxyLog) {
		t.Fatalf("TDR proxy log changed before unauthorized pull: baseline %d bytes, final %d", len(finalProxyLog), len(unauthorizedProxyLog))
	}
	unauthorizedLines := bytes.Split(bytes.TrimSpace(unauthorizedProxyLog[len(finalProxyLog):]), []byte("\n"))
	if len(unauthorizedLines) != 2 {
		t.Fatalf("unauthorized pull generated %d new TDR requests, want OPTIONS and rejected metadata GET:\n%s", len(unauthorizedLines), unauthorizedProxyLog[len(finalProxyLog):])
	}
	var sawUnauthorizedOptions, sawRejectedMetadata bool
	for _, line := range unauthorizedLines {
		var request struct {
			Method        string `json:"method"`
			Path          string `json:"path"`
			Authorization string `json:"authorization"`
			Status        int    `json:"status"`
		}
		if err := json.Unmarshal(line, &request); err != nil {
			t.Fatalf("invalid unauthorized TDR proxy log line %q: %v", line, err)
		}
		if fixtureIDForProxyPath(request.Path, fixtures) != fixtureObjectID(fixtures[0]) {
			t.Fatalf("unauthorized pull contacted the wrong TDR object: %s", line)
		}
		if request.Method == "OPTIONS" && request.Authorization == "" && request.Status == http.StatusOK {
			sawUnauthorizedOptions = true
		}
		if request.Method == "GET" && !strings.Contains(request.Path, "/access/") && request.Authorization == "Bearer tdr-ci-invalid" && request.Status == http.StatusUnauthorized {
			sawRejectedMetadata = true
		}
	}
	if !sawUnauthorizedOptions || !sawRejectedMetadata {
		t.Fatalf("TDR did not reject the bad bearer token as expected:\n%s", unauthorizedProxyLog[len(finalProxyLog):])
	}
}

func makeDockerHostCertificate() (tls.Certificate, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()),
		NotBefore:    now.Add(-time.Minute), NotAfter: now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true, DNSNames: []string{"host.docker.internal", "data.terra.bio"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	return certificate, certificatePEM, err
}

type terraCLIObject struct {
	filename string
	oid      string
	payload  []byte
}

func verifyTerraHubMetadata(response *http.Response, fixtures []terraCLIObject) (string, error) {
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Hub returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("read Hub response: %w", err)
	}
	_ = response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(body))
	var metadata struct {
		FileName string            `json:"fileName"`
		Size     int64             `json:"size"`
		Hashes   map[string]string `json:"hashes"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil {
		return "", fmt.Errorf("decode Hub metadata: %w", err)
	}
	for _, fixture := range fixtures {
		if metadata.FileName != fixture.filename {
			continue
		}
		crc := crc32.Checksum(fixture.payload, crc32.MakeTable(crc32.Castagnoli))
		if metadata.Size != int64(len(fixture.payload)) ||
			metadata.Hashes["md5"] != fmt.Sprintf("%x", md5.Sum(fixture.payload)) ||
			metadata.Hashes["crc32c"] != fmt.Sprintf("%08x", crc) {
			return "", fmt.Errorf("Hub returned inconsistent metadata for %s: %s", fixture.filename, body)
		}
		return fixture.filename, nil
	}
	return "", fmt.Errorf("Hub returned unexpected file metadata: %s", body)
}

func assertFileBytes(t *testing.T, repo string, fixture terraCLIObject, output []byte) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(repo, fixture.filename))
	if err != nil || !bytes.Equal(got, fixture.payload) {
		t.Fatalf("worktree bytes for %s = %q, want %q (error %v)\n%s", fixture.filename, got, fixture.payload, err, output)
	}
}

func assertStagedPointers(t *testing.T, repo string, fixtures []terraCLIObject, pointers map[string]string) {
	t.Helper()
	for _, fixture := range fixtures {
		if got := runGitOutputTest(t, repo, "show", ":"+fixture.filename); got != pointers[fixture.filename] {
			t.Fatalf("staged index bytes for %s = %q, want pointer %q", fixture.filename, got, pointers[fixture.filename])
		}
	}
}

func assertCachedObjects(t *testing.T, paths map[string]string, fixtures []terraCLIObject) {
	t.Helper()
	for _, fixture := range fixtures {
		assertCachedObject(t, paths[fixture.filename], fixture)
	}
}

func assertCachedObject(t *testing.T, path string, fixture terraCLIObject) {
	t.Helper()
	cached, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(cached, fixture.payload) {
		t.Fatalf("cache bytes for %s do not match fixture: got %d bytes, want %d (error %v)", fixture.filename, len(cached), len(fixture.payload), err)
	}
}

func assertResolvedCounts(t *testing.T, mu *sync.Mutex, counts map[string]int, expected map[string]int) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	for filename, want := range expected {
		if got := counts[filename]; got != want {
			t.Fatalf("Hub resolved %s %d times, want %d", filename, got, want)
		}
	}
}

func fixtureIDForProxyPath(path string, fixtures []terraCLIObject) string {
	for _, fixture := range fixtures {
		id := fixtureObjectID(fixture)
		if strings.Contains(path, "/objects/"+id) {
			return id
		}
	}
	return ""
}

func fixtureObjectID(fixture terraCLIObject) string {
	colon := strings.LastIndex(fixture.oid, ":")
	if colon < 0 || colon == len(fixture.oid)-1 {
		return ""
	}
	return fixture.oid[colon+1:]
}

func countTDRRequests(t *testing.T, proxyLog []byte, fixtures []terraCLIObject) (map[string]int, map[string]int, map[string]int) {
	t.Helper()
	metadataRequests := make(map[string]int)
	accessRequests := make(map[string]int)
	probes := make(map[string]int)
	for _, line := range bytes.Split(bytes.TrimSpace(proxyLog), []byte("\n")) {
		var request struct {
			Method        string `json:"method"`
			Path          string `json:"path"`
			Authorization string `json:"authorization"`
			Status        int    `json:"status"`
		}
		if err := json.Unmarshal(line, &request); err != nil {
			t.Fatalf("invalid TDR proxy log line %q: %v", line, err)
		}
		objectID := fixtureIDForProxyPath(request.Path, fixtures)
		if objectID == "" {
			t.Fatalf("unexpected TDR request path in proxy log: %s", line)
		}
		if request.Method == "OPTIONS" {
			if request.Authorization != "" || request.Status != http.StatusOK {
				t.Fatalf("invalid unauthenticated TDR OPTIONS request: %s", line)
			}
			probes[objectID]++
			continue
		}
		if request.Method != "GET" || request.Authorization != "Bearer tdr-ci-fixture" || request.Status != http.StatusOK {
			t.Fatalf("invalid authenticated TDR request: %s", line)
		}
		if strings.Contains(request.Path, "/access/") {
			accessRequests[objectID]++
		} else {
			metadataRequests[objectID]++
		}
	}
	return metadataRequests, accessRequests, probes
}

func shellQuote(argument string) string {
	return "'" + strings.ReplaceAll(argument, "'", "'\\''") + "'"
}
