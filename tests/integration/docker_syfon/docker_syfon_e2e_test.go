//go:build integration

package dockersyfon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitDrsDockerMinIOE2E(t *testing.T) {
	if strings.TrimSpace(os.Getenv(dockerE2EEnvVar)) != "1" {
		t.Skipf("set %s=1 to run the Docker-backed MinIO integration test", dockerE2EEnvVar)
	}

	ctx := context.Background()
	t.Logf("STEP 1: Starting MinIO Docker container...")
	minioEnv, err := startMinIOContainer(ctx)
	if err != nil {
		if isDockerUnavailable(err) {
			t.Skipf("Docker is unavailable for %s: %v", dockerE2EEnvVar, err)
		}
		t.Fatalf("failed to start MinIO container: %v", err)
	}
	t.Logf("MinIO started at %s (bucket: %s)", minioEnv.endpoint, minioEnv.bucket)
	t.Logf("MinIO credentials: region=%s access_key=%s secret_key=%s", minioEnv.region, minioEnv.accessKey, "<redacted>")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := stopDockerContainer(cleanupCtx, minioEnv.containerID); err != nil {
			t.Logf("warning: failed to terminate MinIO container: %v", err)
		}
	})

	t.Logf("STEP 2: Starting Syfon server process...")
	server := startSyfonServerProcess(t, minioEnv)
	t.Logf("Syfon server listening at %s", server.url)
	t.Cleanup(func() {
		stopSyfonServerProcess(t, server)
	})
	t.Logf("Syfon server ready: healthz=%s/healthz", server.url)

	t.Logf("STEP 3: Starting Gogs Git server process...")
	gogsEnv := startGogsContainer(t, ctx)
	t.Logf("Gogs server listening at %s (repo clone URL: %s)", gogsEnv.endpoint, gogsEnv.repoCloneURL)
	t.Logf("Gogs admin user=%s repo=%s token=%t", gogsEnv.adminUser, gogsEnv.repoName, gogsEnv.apiToken != "")

	workDir := t.TempDir()
	repoDir := filepath.Join(workDir, "repo")
	cloneDir := filepath.Join(workDir, "clone")
	t.Logf("Working directories: workDir=%s repoDir=%s cloneDir=%s", workDir, repoDir, cloneDir)
	t.Logf("Git credential store: %s", gogsEnv.credentialStore)

	t.Logf("STEP 4: Creating git repository and configuring git-drs remote...")
	runCommand(t, workDir, nil, "git", "init", "-b", "main", repoDir)
	runCommand(t, repoDir, nil, "git", "drs", "install")
	configureLocalRepo(t, repoDir, gogsEnv.credentialStore)
	runCommand(t, repoDir, nil, "git", "remote", "add", "origin", gogsEnv.repoCloneURL)
	runCommand(t, repoDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, repoDir, server.url, minioEnv)
	logRepoSnapshot(t, repoDir, "post-init")

	t.Logf("STEP 5: Uploading tracked files through git-drs push...")
	smallPath := filepath.Join(repoDir, "data", "source.txt")
	smallData := []byte("git-drs docker minio e2e payload")
	if err := os.MkdirAll(filepath.Dir(smallPath), 0o755); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(smallPath, smallData, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	largePath := filepath.Join(repoDir, "data", "multipart.bin")
	largeData := bytes.Repeat([]byte("syfon-git-drs-multipart-e2e-"), 7*1024*1024/len("syfon-git-drs-multipart-e2e-")+1)
	largeData = largeData[:7*1024*1024]
	if err := os.WriteFile(largePath, largeData, 0o644); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	runCommand(t, repoDir, nil, "git", "drs", "track", "*.txt", "*.bin")
	runCommand(t, repoDir, nil, "git", "config", "--local", "drs.multipart-threshold", fmt.Sprintf("%d", dockerE2EMultipartMB))
	runCommand(t, repoDir, nil, "git", "add", ".gitattributes", "data/source.txt", "data/multipart.bin")
	runCommand(t, repoDir, nil, "git", "commit", "-m", "docker e2e upload")
	smallPointer := runCommand(t, repoDir, nil, "git", "show", "HEAD:data/source.txt")
	smallDid := parsePointerOID(t, []byte(smallPointer))
	t.Logf("source.txt pointer DID=%s", smallDid)
	largePointer := runCommand(t, repoDir, nil, "git", "show", "HEAD:data/multipart.bin")
	largeDid := parsePointerOID(t, []byte(largePointer))
	t.Logf("multipart.bin pointer DID=%s", largeDid)
	logRepoSnapshot(t, repoDir, "pre-push")
	if err := writeGitCredentialStore(gogsEnv.credentialStore, gogsEnv.repoCloneURL, dockerE2EGogsAdminUser, "wrong-password"); err != nil {
		t.Fatalf("write transient bad git credential store: %v", err)
	}
	if out, err := runCommandOutput(t, repoDir, nil, "git", "drs", "push", "origin"); err == nil {
		t.Fatalf("expected first multipart push to fail, but it succeeded:\n%s", out)
	} else {
		t.Logf("expected first multipart push failure captured for retry path")
	}
	if err := writeGitCredentialStore(gogsEnv.credentialStore, gogsEnv.repoCloneURL, dockerE2EGogsAdminUser, dockerE2EGogsAdminPassword); err != nil {
		t.Fatalf("restore git credential store: %v", err)
	}
	runCommand(t, repoDir, nil, "git", "drs", "push", "origin")
	logRepoSnapshot(t, repoDir, "post-push")
	querySmall := runCommand(t, repoDir, nil, "git", "drs", "query", "--remote", "origin", "--pretty", smallDid)
	if !strings.Contains(querySmall, smallDid) {
		t.Fatalf("query output missing small DID %s:\n%s", smallDid, querySmall)
	}
	t.Logf("query verified source.txt DID %s", smallDid)
	queryLarge := runCommand(t, repoDir, nil, "git", "drs", "query", "--remote", "origin", "--pretty", largeDid)
	if !strings.Contains(queryLarge, largeDid) {
		t.Fatalf("query output missing multipart DID %s:\n%s", largeDid, queryLarge)
	}
	t.Logf("query verified multipart.bin DID %s", largeDid)

	t.Logf("STEP 6: Cloning and pulling the files back from Syfon through Gogs...")
	runCommand(t, workDir, nil, "git", "-c", "credential.helper=store --file "+gogsEnv.credentialStore, "clone", "--branch", "main", gogsEnv.repoCloneURL, cloneDir)
	configureLocalRepo(t, cloneDir, gogsEnv.credentialStore)
	runCommand(t, cloneDir, nil, "git", "drs", "install")
	runCommand(t, cloneDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, cloneDir, server.url, minioEnv)
	runCommand(t, cloneDir, nil, "git", "config", "--local", "drs.multipart-threshold", fmt.Sprintf("%d", dockerE2EMultipartMB))
	logRepoSnapshot(t, cloneDir, "pre-pull")

	restoreLargeObject := temporarilyRemoveMinIOObject(t, minioEnv.s3Client, minioEnv.bucket, largeDid, int64(len(largeData)))
	if out, err := runCommandOutput(t, cloneDir, nil, "git", "drs", "pull", "origin"); err == nil {
		t.Fatalf("expected first multipart pull to fail, but it succeeded:\n%s", out)
	} else {
		t.Logf("expected first multipart pull failure captured for retry path")
	}
	restoreLargeObject()
	runCommand(t, cloneDir, nil, "git", "drs", "pull", "origin")
	logRepoSnapshot(t, cloneDir, "post-pull")

	got, err := os.ReadFile(filepath.Join(cloneDir, "data", "source.txt"))
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if !bytes.Equal(got, smallData) {
		t.Fatalf("pulled bytes mismatch: got %q want %q", string(got), string(smallData))
	}
	gotLarge, err := os.ReadFile(filepath.Join(cloneDir, "data", "multipart.bin"))
	if err != nil {
		t.Fatalf("read pulled multipart file: %v", err)
	}
	if !bytes.Equal(gotLarge, largeData) {
		t.Fatalf("pulled multipart bytes mismatch: got %d bytes want %d bytes", len(gotLarge), len(largeData))
	}

	smallSum := sha256.Sum256(smallData)
	smallSumHex := hex.EncodeToString(smallSum[:])
	smallHashOut := runCommand(t, cloneDir, nil, "git", "drs", "query", "--remote", "origin", "--checksum", "--pretty", smallSumHex)
	if !strings.Contains(smallHashOut, smallDid) || !strings.Contains(smallHashOut, smallSumHex) {
		t.Fatalf("small file checksum lookup mismatch: expected DID %s and hash %s in output %q", smallDid, smallSumHex, smallHashOut)
	}

	largeSum := sha256.Sum256(largeData)
	largeSumHex := hex.EncodeToString(largeSum[:])
	largeHashOut := runCommand(t, cloneDir, nil, "git", "drs", "query", "--remote", "origin", "--checksum", "--pretty", largeSumHex)
	if !strings.Contains(largeHashOut, largeDid) || !strings.Contains(largeHashOut, largeSumHex) {
		t.Fatalf("multipart file checksum lookup mismatch: expected DID %s and hash %s in output %q", largeDid, largeSumHex, largeHashOut)
	}
	t.Logf("hash verification complete for source.txt=%s multipart.bin=%s", smallSumHex, largeSumHex)
}

func TestGitDrsDockerAddURLE2E(t *testing.T) {
	if strings.TrimSpace(os.Getenv(dockerE2EEnvVar)) != "1" {
		t.Skipf("set %s=1 to run the Docker-backed MinIO integration test", dockerE2EEnvVar)
	}

	ctx := context.Background()
	t.Logf("STEP 1: Starting MinIO Docker container...")
	minioEnv, err := startMinIOContainer(ctx)
	if err != nil {
		if isDockerUnavailable(err) {
			t.Skipf("Docker is unavailable for %s: %v", dockerE2EEnvVar, err)
		}
		t.Fatalf("failed to start MinIO container: %v", err)
	}
	t.Logf("MinIO started at %s (bucket: %s)", minioEnv.endpoint, minioEnv.bucket)
	t.Logf("MinIO credentials: region=%s access_key=%s secret_key=%s", minioEnv.region, minioEnv.accessKey, "<redacted>")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := stopDockerContainer(cleanupCtx, minioEnv.containerID); err != nil {
			t.Logf("warning: failed to terminate MinIO container: %v", err)
		}
	})

	t.Logf("STEP 2: Starting Syfon server process...")
	server := startSyfonServerProcess(t, minioEnv)
	t.Logf("Syfon server listening at %s", server.url)
	t.Cleanup(func() {
		stopSyfonServerProcess(t, server)
	})
	t.Logf("Syfon server ready: healthz=%s/healthz", server.url)

	t.Logf("STEP 3: Starting Gogs Git server process...")
	gogsEnv := startGogsContainer(t, ctx)
	t.Logf("Gogs server listening at %s (repo clone URL: %s)", gogsEnv.endpoint, gogsEnv.repoCloneURL)
	t.Logf("Gogs admin user=%s repo=%s token=%t", gogsEnv.adminUser, gogsEnv.repoName, gogsEnv.apiToken != "")

	workDir := t.TempDir()
	repoDir := filepath.Join(workDir, "repo")
	cloneDir := filepath.Join(workDir, "clone")
	t.Logf("Working directories: workDir=%s repoDir=%s cloneDir=%s", workDir, repoDir, cloneDir)
	t.Logf("Git credential store: %s", gogsEnv.credentialStore)

	t.Logf("STEP 4: Creating git repository and configuring git-drs remote...")
	runCommand(t, workDir, nil, "git", "init", "-b", "main", repoDir)
	configureLocalRepo(t, repoDir, gogsEnv.credentialStore)
	runCommand(t, repoDir, nil, "git", "remote", "add", "origin", gogsEnv.repoCloneURL)
	runCommand(t, repoDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, repoDir, server.url, minioEnv)
	logRepoSnapshot(t, repoDir, "post-init")

	t.Logf("STEP 5: Seeding objects directly into MinIO...")
	knownData := []byte("known add-url payload")
	knownHash := sha256.Sum256(knownData)
	knownOID := hex.EncodeToString(knownHash[:])
	knownKey := fmt.Sprintf("%s/%s/addurl/%s", dockerE2EOrganization, dockerE2EProjectID, knownOID)
	t.Logf("known add-url object: key=%s oid=%s size=%d", knownKey, knownOID, len(knownData))
	seedMinIOObject(t, minioEnv.s3Client, minioEnv.bucket, knownKey, knownData)

	unknownData := []byte("unknown add-url payload")
	unknownHash := sha256.Sum256(unknownData)
	unknownOID := hex.EncodeToString(unknownHash[:])
	unknownKey := fmt.Sprintf("%s/%s/addurl/%s-unknown", dockerE2EOrganization, dockerE2EProjectID, unknownOID)
	t.Logf("unknown add-url object: key=%s oid=%s size=%d", unknownKey, unknownOID, len(unknownData))
	seedMinIOObject(t, minioEnv.s3Client, minioEnv.bucket, unknownKey, unknownData)

	t.Logf("STEP 6: Registering the objects via git-drs add-url...")
	knownPath := filepath.Join("data", "from-bucket.bin")
	knownOut := runCommandMust(t, repoDir, []string{
		"TEST_BUCKET_REGION=" + minioEnv.region,
		"TEST_BUCKET_ENDPOINT=" + minioEnv.endpoint,
		"TEST_BUCKET_ACCESS_KEY=" + minioEnv.accessKey,
		"TEST_BUCKET_SECRET_KEY=" + minioEnv.secretKey,
	}, "git", "drs", "add-url", "s3://"+minioEnv.bucket+"/"+knownKey, knownPath, "--sha256", knownOID)
	if strings.TrimSpace(knownOut) == "" {
		t.Log("known add-url completed")
	}
	t.Logf("known add-url output:\n%s", knownOut)

	unknownPath := filepath.Join("data", "from-bucket-unknown.bin")
	unknownOut := runCommandMust(t, repoDir, []string{
		"TEST_BUCKET_REGION=" + minioEnv.region,
		"TEST_BUCKET_ENDPOINT=" + minioEnv.endpoint,
		"TEST_BUCKET_ACCESS_KEY=" + minioEnv.accessKey,
		"TEST_BUCKET_SECRET_KEY=" + minioEnv.secretKey,
	}, "git", "drs", "add-url", "s3://"+minioEnv.bucket+"/"+unknownKey, unknownPath)
	if strings.TrimSpace(unknownOut) == "" {
		t.Log("unknown add-url completed")
	}
	t.Logf("unknown add-url output:\n%s", unknownOut)

	knownPointer := mustReadFile(t, repoDir, knownPath)
	knownPointerOID := parsePointerOID(t, knownPointer)
	if knownPointerOID != knownOID {
		t.Fatalf("known add-url pointer oid mismatch: got %s want %s", knownPointerOID, knownOID)
	}

	unknownPointer := mustReadFile(t, repoDir, unknownPath)
	unknownPointerOID := parsePointerOID(t, unknownPointer)
	if unknownPointerOID == "" {
		t.Fatalf("unknown add-url pointer oid is empty")
	}
	if unknownPointerOID == unknownOID {
		t.Fatalf("unknown add-url unexpectedly used the real sha256")
	}
	t.Logf("add-url pointer checks complete: known=%s unknown=%s", knownPointerOID, unknownPointerOID)

	runCommand(t, repoDir, nil, "git", "add", ".gitattributes", knownPath, unknownPath)
	runCommand(t, repoDir, nil, "git", "commit", "-m", "docker e2e add-url")
	runCommand(t, repoDir, nil, "git", "drs", "push", "origin")
	logRepoSnapshot(t, repoDir, "post-add-url-push")

	knownQuery := runCommand(t, repoDir, nil, "git", "drs", "query", "--remote", "origin", "--checksum", "--pretty", knownPointerOID)
	assertAccessURL(t, knownQuery, "s3://"+minioEnv.bucket+"/"+knownKey)
	assertNoGeneratedProgramProjectPath(t, knownQuery)
	unknownQuery := runCommand(t, repoDir, nil, "git", "drs", "query", "--remote", "origin", "--checksum", "--pretty", unknownPointerOID)
	assertAccessURL(t, unknownQuery, "s3://"+minioEnv.bucket+"/"+unknownKey)
	assertNoGeneratedProgramProjectPath(t, unknownQuery)

	t.Logf("STEP 7: Cloning and pulling the add-url content back through Gogs...")
	runCommand(t, workDir, nil, "git", "-c", "credential.helper=store --file "+gogsEnv.credentialStore, "clone", "--branch", "main", gogsEnv.repoCloneURL, cloneDir)
	configureLocalRepo(t, cloneDir, gogsEnv.credentialStore)
	runCommand(t, cloneDir, nil, "git", "drs", "install")
	runCommand(t, cloneDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, cloneDir, server.url, minioEnv)
	runCommand(t, cloneDir, nil, "git", "drs", "pull", "origin")
	logRepoSnapshot(t, cloneDir, "post-add-url-pull")

	gotKnown := mustReadFile(t, cloneDir, knownPath)
	if !bytes.Equal(gotKnown, knownData) {
		t.Fatalf("known add-url file mismatch: got %q want %q", string(gotKnown), string(knownData))
	}
	gotUnknown := mustReadFile(t, cloneDir, unknownPath)
	if !bytes.Equal(gotUnknown, unknownData) {
		t.Fatalf("unknown add-url file mismatch: got %q want %q", string(gotUnknown), string(unknownData))
	}
	t.Logf("add-url round-trip verification complete")
}

func TestGitDrsDockerBucketScopePathsE2E(t *testing.T) {
	if strings.TrimSpace(os.Getenv(dockerE2EEnvVar)) != "1" {
		t.Skipf("set %s=1 to run the Docker-backed MinIO integration test", dockerE2EEnvVar)
	}

	ctx := context.Background()
	t.Logf("STEP 1: Starting MinIO Docker container...")
	minioEnv, err := startMinIOContainer(ctx)
	if err != nil {
		if isDockerUnavailable(err) {
			t.Skipf("Docker is unavailable for %s: %v", dockerE2EEnvVar, err)
		}
		t.Fatalf("failed to start MinIO container: %v", err)
	}
	t.Logf("MinIO started at %s (bucket: %s)", minioEnv.endpoint, minioEnv.bucket)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := stopDockerContainer(cleanupCtx, minioEnv.containerID); err != nil {
			t.Logf("warning: failed to terminate MinIO container: %v", err)
		}
	})

	t.Logf("STEP 2: Starting Syfon server process...")
	server := startSyfonServerProcess(t, minioEnv)
	t.Cleanup(func() {
		stopSyfonServerProcess(t, server)
	})

	t.Logf("STEP 3: Starting Gogs Git server process...")
	gogsEnv := startGogsContainer(t, ctx)

	workDir := t.TempDir()
	repoDir := filepath.Join(workDir, "repo")

	t.Logf("STEP 4: Creating git repository and configuring git-drs remote...")
	runCommand(t, workDir, nil, "git", "init", "-b", "main", repoDir)
	runCommand(t, repoDir, nil, "git", "drs", "install")
	configureLocalRepo(t, repoDir, gogsEnv.credentialStore)
	runCommand(t, repoDir, nil, "git", "remote", "add", "origin", gogsEnv.repoCloneURL)
	runCommand(t, repoDir, nil, "git", "drs", "init")
	configureGitDrsRemote(t, repoDir, server.url, minioEnv)
	runCommand(t, repoDir, nil, "git", "drs", "track", "*.bin")

	t.Logf("STEP 5: Adding managed uploads for each bucket scope path case...")
	defaultOID := addTrackedPayloadCommit(t, repoDir, "data/default-root.bin", []byte("default bucket root payload"), ".gitattributes")
	defaultKey := defaultOID
	runCommand(t, repoDir, nil, "git", "drs", "push", "origin")
	verifyBucketScopeObject(t, repoDir, minioEnv, "default root", defaultOID, defaultKey)

	programPath := "program-root"
	upsertSyfonBucketScope(t, server.url, minioEnv, dockerE2EOrganization, "", "s3://"+minioEnv.bucket+"/"+programPath)
	setLocalBucketMapping(t, repoDir, dockerE2EOrganization, "", minioEnv.bucket, programPath)
	programOID := addTrackedPayloadCommit(t, repoDir, "data/program-root.bin", []byte("program path payload"))
	programKey := programPath + "/" + programOID
	runCommand(t, repoDir, nil, "git", "drs", "push", "origin")
	verifyBucketScopeObject(t, repoDir, minioEnv, "program path", programOID, programKey)

	projectSubpath := "project-subpath"
	upsertSyfonBucketScope(t, server.url, minioEnv, dockerE2EOrganization, dockerE2EProjectID, "s3://"+minioEnv.bucket+"/"+projectSubpath)
	setLocalBucketMapping(t, repoDir, dockerE2EOrganization, dockerE2EProjectID, minioEnv.bucket, projectSubpath)
	projectOID := addTrackedPayloadCommit(t, repoDir, "data/project-subpath.bin", []byte("project subpath payload"))
	projectKey := programPath + "/" + projectSubpath + "/" + projectOID
	runCommand(t, repoDir, nil, "git", "drs", "push", "origin")
	verifyBucketScopeObject(t, repoDir, minioEnv, "program plus project subpath", projectOID, projectKey)

	t.Logf("bucket scope path verification complete: default=%s program=%s project=%s", defaultKey, programKey, projectKey)
}
