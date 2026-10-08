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
	"sync/atomic"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/lfs"
)

func TestIntegrationPullThroughTerraHubAndTDR(t *testing.T) {
	payload := []byte("git-drs Terra DRS HTTP integration fixture\n")
	binary := os.Getenv("GIT_DRS_BINARY")
	hubEndpoint := os.Getenv("GIT_DRS_HUB_HTTP_ENDPOINT")
	proxyLog := os.Getenv("GIT_DRS_TDR_PROXY_LOG")
	gcsEndpoint := os.Getenv("GIT_DRS_TDR_GCS_ENDPOINT")
	keyPath := os.Getenv("GIT_DRS_TDR_SIGNING_PUBLIC_KEY_FILE")
	if binary == "" || hubEndpoint == "" || proxyLog == "" || gcsEndpoint == "" || keyPath == "" {
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
		if r.Method != http.MethodPost || r.URL.Path != "/token" {
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
		if err := json.Unmarshal(claimsBytes, &claims); err != nil || claims.Issuer != "git-drs-ci@example.invalid" || claims.Audience != tokenEndpoint+"/token" || !strings.Contains(claims.Scope, "cloud-platform") {
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
		_, _ = io.WriteString(w, `{"access_token":"tdr-ci-fixture","token_type":"Bearer","expires_in":3600}`)
	}))
	tokenServer.Listener = tokenListener
	tokenServer.Start()
	defer tokenServer.Close()

	var resolveRequests atomic.Int32
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
		forwarded, err := http.NewRequestWithContext(r.Context(), r.Method, "http://"+upstream.Host+r.URL.RequestURI(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		forwarded.Header = r.Header.Clone()
		forwarded.Host = upstream.Host
		response, err := http.DefaultTransport.RoundTrip(forwarded)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		if r.URL.Path == "/api/v4/drs/resolve" {
			resolveRequests.Add(1)
			hubBearer.Store(r.Header.Get("Authorization"))
			if err := verifyTerraHubMetadata(response, payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			if err := rewriteTDRSignedURLForEmulator(response, gcsEndpoint, publicKeyPEM); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
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
	runGitCmdTest(t, repo, "config", "drs.default-remote", "anvil")
	runGitCmdTest(t, repo, "config", "drs.remote.anvil.type", "terra")
	runGitCmdTest(t, repo, "config", "drs.remote.anvil.endpoint", "https://data.terra.bio")
	runGitCmdTest(t, repo, "config", "drs.remote.anvil.hub-endpoint", frontProxyURL)
	runGitCmdTest(t, repo, "config", "drs.remote.anvil.mode", "read-only")
	const filename = "1614321.merge_output.gvcf.gz"
	const oid = "drs://drs.anv0:v2_4f770147-e372-339b-b9fa-0a7a83cf30cf"
	pointer := fmt.Sprintf("version https://calypr.github.io/spec/v1\noid %s\nsize %d\n", oid, len(payload))
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte(filename+" filter=drs diff=drs merge=drs -text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, filename), []byte(pointer), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmdTest(t, repo, "add", filename)
	t.Chdir(repo)
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

	runPull := func() []byte {
		command := exec.Command("docker", "run", "--rm",
			"--add-host", "host.docker.internal:host-gateway",
			"--volume", binary+":/usr/local/bin/git-drs:ro",
			"--volume", repo+":/repo",
			"--volume", credentialsPath+":/ci/service-account.json:ro",
			"--volume", certPath+":/ci/hub-proxy-ca.pem:ro",
			"--workdir", "/repo",
			"--env", "HOME=/tmp/git-drs-ci-home",
			"--env", "GIT_CONFIG_NOSYSTEM=1",
			"--env", "GOOGLE_APPLICATION_CREDENTIALS=/ci/service-account.json",
			"--env", "SSL_CERT_FILE=/ci/hub-proxy-ca.pem",
			"--entrypoint", "/bin/sh", "alpine/git@sha256:062a01ad7a0eb17cff382bc5e26086b4d710e56dfdfdf001109a49b6d9bd378c", "-c",
			"mkdir -p /tmp/git-drs-ci-home && git config --global --add safe.directory /repo && git config --global filter.drs.clean \"/usr/local/bin/git-drs clean -- %f\" && git config --global filter.drs.smudge \"/usr/local/bin/git-drs smudge -- %f\" && git config --global filter.drs.process \"/usr/local/bin/git-drs filter\" && git config --global filter.drs.required true && exec /usr/local/bin/git-drs pull -I "+filename)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git-drs pull exited with %v:\n%s", err, output)
		}
		return output
	}
	output := runPull()
	if got, err := os.ReadFile(filepath.Join(repo, filename)); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("CLI worktree bytes = %q, want %q (error %v)\n%s", got, payload, err, output)
	}
	if got := tokenRequests.Load(); got != 1 {
		t.Fatalf("ADC token exchanges = %d, want one", got)
	}
	if got := resolveRequests.Load(); got != 1 {
		t.Fatalf("Hub resolve requests = %d, want one", got)
	}
	if got := hubBearer.Load(); got != "Bearer tdr-ci-fixture" {
		t.Fatalf("Hub received Authorization %v, want ADC fixture token", got)
	}
	if got := runGitOutputTest(t, repo, "show", ":"+filename); got != pointer {
		t.Fatalf("staged index bytes = %q, want original pointer %q", got, pointer)
	}
	cached, err := os.ReadFile(cachePath)
	if err != nil || !bytes.Equal(cached, payload) {
		t.Fatalf("cache bytes do not match fixture: %d bytes, %v", len(cached), err)
	}
	proxyLogBytes, err := os.ReadFile(proxyLog)
	if err != nil {
		t.Fatal(err)
	}
	var metadataRequests, accessRequests int
	var unauthenticatedProbe bool
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
		if request.Method == "OPTIONS" && strings.HasSuffix(request.Path, "/objects/v2_4f770147-e372-339b-b9fa-0a7a83cf30cf") && request.Authorization == "" && request.Status == http.StatusOK {
			unauthenticatedProbe = true
		}
		if request.Method == "GET" && strings.HasSuffix(request.Path, "/objects/v2_4f770147-e372-339b-b9fa-0a7a83cf30cf") {
			if request.Authorization != "Bearer tdr-ci-fixture" || request.Status != http.StatusOK {
				t.Fatalf("invalid TDR metadata request: %s", line)
			}
			metadataRequests++
		}
		if request.Method == "GET" && strings.Contains(request.Path, "/objects/v2_4f770147-e372-339b-b9fa-0a7a83cf30cf/access/") {
			if request.Authorization != "Bearer tdr-ci-fixture" || request.Status != http.StatusOK {
				t.Fatalf("invalid TDR access request: %s", line)
			}
			accessRequests++
		}
	}
	if !unauthenticatedProbe || metadataRequests != 1 || accessRequests != 1 {
		t.Fatalf("Hub did not complete the unauthenticated probe and authenticated TDR requests: %s", proxyLogBytes)
	}

	secondOutput := runPull()
	if got, err := os.ReadFile(filepath.Join(repo, filename)); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("cached pull changed worktree file: %d bytes, %v\n%s", len(got), err, secondOutput)
	}
	if got := tokenRequests.Load(); got != 1 {
		t.Fatalf("ADC token exchanges after cached pull = %d, want one", got)
	}
	if got := resolveRequests.Load(); got != 1 {
		t.Fatalf("Hub resolve requests after cached pull = %d, want one", got)
	}
	secondProxyLog, err := os.ReadFile(proxyLog)
	if err != nil || !bytes.Equal(secondProxyLog, proxyLogBytes) {
		t.Fatalf("cached pull contacted TDR: before %d bytes, after %d bytes (error %v)", len(proxyLogBytes), len(secondProxyLog), err)
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
		BasicConstraintsValid: true, IsCA: true, DNSNames: []string{"host.docker.internal"},
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

func verifyTerraHubMetadata(response *http.Response, payload []byte) error {
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Hub returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("read Hub response: %w", err)
	}
	_ = response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(body))
	var metadata struct {
		FileName string            `json:"fileName"`
		Size     int64             `json:"size"`
		Hashes   map[string]string `json:"hashes"`
	}
	if err := json.Unmarshal(body, &metadata); err != nil {
		return fmt.Errorf("decode Hub metadata: %w", err)
	}
	crc := crc32.Checksum(payload, crc32.MakeTable(crc32.Castagnoli))
	if metadata.FileName != "1614321.merge_output.gvcf.gz" || metadata.Size != int64(len(payload)) ||
		metadata.Hashes["md5"] != fmt.Sprintf("%x", md5.Sum(payload)) ||
		metadata.Hashes["crc32c"] != fmt.Sprintf("%08x", crc) {
		return fmt.Errorf("Hub returned inconsistent file metadata: %s", body)
	}
	return nil
}
