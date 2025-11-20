// Integration tests for add-url.go functions that test real config loading and integration
// with multiple components while mocking the helper methods that are already tested elsewhere.
//
// This test suite focuses on:
// 1. getBucketDetails - Integration testing of config loading, endpoint validation, and error handling
// 2. getBucketDetailsWithAuth - Full integration testing of HTTP requests, auth handling, and response parsing
// 3. fetchS3Metadata - Integration testing with real S3 client creation and bucket details integration
// 4. fetchS3MetadataWithBucketDetails - Testing S3 client integration with mock S3 servers
//
// The helper methods fetchS3MetadataWithBucketDetails and getBucketDetailsWithAuth are already
// unit tested in other files, so here we focus on integration scenarios and real config usage.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/internal/testutils"
	"github.com/calypr/git-drs/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAuthHandlerForIntegration is a test auth handler for integration tests
type mockAuthHandlerForIntegration struct {
	shouldFail bool
	token      string
}

func (m *mockAuthHandlerForIntegration) AddAuthHeader(req *http.Request, profile string) error {
	if m.shouldFail {
		return errors.New("mock auth failure")
	}
	if m.token != "" {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}
	return nil
}

// mockS3ServerForIntegration creates a mock S3 server for integration tests
func createMockS3ServerForIntegration(shouldFail bool, size int64, lastModified time.Time) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if shouldFail {
			http.Error(w, "Mock S3 failure", http.StatusInternalServerError)
			return
		}

		// Handle HeadObject requests
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
			w.Header().Set("Last-Modified", lastModified.Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)
			return
		}

		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}))
}

// TestGetBucketDetails_Integration tests getBucketDetails with real config loading
// Since getBucketDetails has a hard dependency on Gen3 authentication, we focus on
// config loading and error scenarios that occur before auth is required.
func TestGetBucketDetails_Integration(t *testing.T) {
	tests := []struct {
		name        string
		bucket      string
		setupConfig func(tmpDir string) *config.Config
		wantErr     bool
		errContains string
	}{
		{
			name:   "config missing - no .drs directory",
			bucket: "test-bucket",
			setupConfig: func(tmpDir string) *config.Config {
				// Don't create any config - this will cause config load failure
				return nil
			},
			wantErr:     true,
			errContains: "config file does not exist",
		},
		{
			name:   "invalid gen3 server config - empty endpoint",
			bucket: "test-bucket",
			setupConfig: func(tmpDir string) *config.Config {
				return &config.Config{
					CurrentServer: config.Gen3ServerType,
					Servers: config.ServersMap{
						Gen3: &config.Gen3Server{
							Endpoint: "", // Invalid empty endpoint
							Auth: config.Gen3Auth{
								Profile:   "test-profile",
								ProjectID: "test-program-test-project",
								Bucket:    "test-bucket",
							},
						},
					},
				}
			},
			wantErr: true,
			// The auth failure happens before endpoint validation in the current implementation
			errContains: "Profile not in config file",
		},
		{
			name:   "anvil server type error - no gen3 config",
			bucket: "test-bucket",
			setupConfig: func(tmpDir string) *config.Config {
				return &config.Config{
					CurrentServer: config.AnvilServerType,
					Servers: config.ServersMap{
						Gen3: nil, // No Gen3 config when current is anvil
					},
				}
			},
			wantErr:     true,
			errContains: "Gen3 server endpoint is not configured",
		},
		{
			name:   "gen3 server nil - tests endpoint validation",
			bucket: "test-bucket",
			setupConfig: func(tmpDir string) *config.Config {
				return &config.Config{
					CurrentServer: "other", // Not gen3, but with nil Gen3 config
					Servers: config.ServersMap{
						Gen3: nil, // Nil Gen3 config
					},
				}
			},
			wantErr:     true,
			errContains: "Gen3 server endpoint is not configured",
		},
		{
			name:   "invalid base url parsing",
			bucket: "test-bucket",
			setupConfig: func(tmpDir string) *config.Config {
				return &config.Config{
					CurrentServer: config.Gen3ServerType,
					Servers: config.ServersMap{
						Gen3: &config.Gen3Server{
							Endpoint: "://invalid-url", // Invalid URL format
							Auth: config.Gen3Auth{
								Profile:   "test-profile",
								ProjectID: "test-program-test-project",
								Bucket:    "test-bucket",
							},
						},
					},
				}
			},
			wantErr:     true,
			errContains: "failed to parse base URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup temporary git repo
			tmpDir := testutils.SetupTestGitRepo(t)

			// Create config if specified
			if tt.setupConfig != nil {
				testConfig := tt.setupConfig(tmpDir)
				if testConfig != nil {
					testutils.CreateTestConfig(t, tmpDir, testConfig)
				}
			}

			// Test getBucketDetails
			ctx := context.Background()
			_, err := getBucketDetails(ctx, tt.bucket, &http.Client{})

			// All test cases expect errors since we're testing error scenarios
			assert.Error(t, err)
			if tt.errContains != "" {
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}

// TestFetchS3Metadata_Integration tests fetchS3Metadata with various config scenarios
func TestFetchS3Metadata_Integration(t *testing.T) {
	tests := []struct {
		name         string
		s3URL        string
		awsAccessKey string
		awsSecretKey string
		region       string
		endpoint     string
		setupConfig  func(tmpDir string) *config.Config
		mockBucket   S3Bucket
		s3ServerFail bool
		s3Size       int64
		s3Modified   time.Time
		serverStatus int
		wantSize     int64
		wantModified string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "success with bucket details from gen3 and working s3",
			s3URL:        "s3://test-bucket/sample.bam",
			awsAccessKey: "test-access-key",
			awsSecretKey: "test-secret-key",
			setupConfig: func(tmpDir string) *config.Config {
				return testutils.CreateDefaultTestConfig(t, tmpDir)
			},
			mockBucket: S3Bucket{
				Region:      "us-west-2",
				EndpointURL: "will-be-replaced-with-mock",
				Programs:    []string{"test-program"},
			},
			s3ServerFail: false,
			s3Size:       1024,
			s3Modified:   time.Date(2023, 10, 15, 12, 0, 0, 0, time.UTC),
			serverStatus: http.StatusOK,
			wantSize:     1024,
			wantModified: "2023-10-15T12:00:00Z",
			wantErr:      false,
		},
		{
			name:         "invalid s3 url",
			s3URL:        "invalid://bucket/file",
			awsAccessKey: "test-access-key",
			awsSecretKey: "test-secret-key",
			setupConfig: func(tmpDir string) *config.Config {
				return testutils.CreateDefaultTestConfig(t, tmpDir)
			},
			wantErr:     true,
			errContains: "failed to parse S3 URL",
		},
		{
			name:         "getBucketDetails fails",
			s3URL:        "s3://test-bucket/sample.bam",
			awsAccessKey: "test-access-key",
			awsSecretKey: "test-secret-key",
			setupConfig: func(tmpDir string) *config.Config {
				cfg := testutils.CreateDefaultTestConfig(t, tmpDir)
				cfg.Servers.Gen3.Endpoint = "" // Invalid endpoint to force error
				return cfg
			},
			wantErr:     true,
			errContains: "Unable to get bucket details",
		},
		{
			name:         "s3 server failure",
			s3URL:        "s3://test-bucket/sample.bam",
			awsAccessKey: "test-access-key",
			awsSecretKey: "test-secret-key",
			setupConfig: func(tmpDir string) *config.Config {
				return testutils.CreateDefaultTestConfig(t, tmpDir)
			},
			mockBucket: S3Bucket{
				Region:      "us-west-2",
				EndpointURL: "will-be-replaced-with-mock",
				Programs:    []string{"test-program"},
			},
			s3ServerFail: true,
			serverStatus: http.StatusOK,
			wantErr:      true,
			errContains:  "failed to head object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup temporary git repo
			tmpDir := testutils.SetupTestGitRepo(t)

			// Create config
			testConfig := tt.setupConfig(tmpDir)

			// Setup mock S3 server
			var mockS3Server *httptest.Server
			if !tt.wantErr || tt.errContains == "failed to head object" {
				mockS3Server = createMockS3ServerForIntegration(tt.s3ServerFail, tt.s3Size, tt.s3Modified)
				defer mockS3Server.Close()
			}

			// Setup mock Gen3 server for bucket details (if config is valid)
			var mockGen3Server *httptest.Server
			if testConfig.Servers.Gen3 != nil && testConfig.Servers.Gen3.Endpoint != "" {
				mockGen3Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/user/data/buckets", r.URL.Path)
					w.WriteHeader(tt.serverStatus)

					if tt.serverStatus == http.StatusOK {
						w.Header().Set("Content-Type", "application/json")

						resp := S3BucketsResponse{
							S3Buckets: map[string]S3Bucket{},
						}

						if tt.mockBucket.Region != "" || tt.mockBucket.EndpointURL != "" {
							// Extract bucket name from S3 URL
							bucket, _, err := utils.ParseS3URL(tt.s3URL)
							if err == nil {
								bucketDetails := tt.mockBucket
								if mockS3Server != nil {
									bucketDetails.EndpointURL = mockS3Server.URL
								}
								resp.S3Buckets[bucket] = bucketDetails
							}
						}

						err := json.NewEncoder(w).Encode(resp)
						require.NoError(t, err)
					}
				}))
				defer mockGen3Server.Close()

				// Update config with mock Gen3 server URL
				testConfig.Servers.Gen3.Endpoint = mockGen3Server.URL
				testutils.CreateTestConfig(t, tmpDir, testConfig)
			}

			// Test fetchS3Metadata with bucket details integration
			if !tt.wantErr || tt.errContains == "failed to head object" {
				// Test using fetchS3MetadataWithBucketDetails with real S3 client
				ctx := context.Background()

				// Create a real S3 client pointing to our mock S3 server
				cfg, err := awsConfig.LoadDefaultConfig(ctx,
					awsConfig.WithRegion("us-west-2"),
					awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test-key", "test-secret", "")),
				)
				require.NoError(t, err)

				s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
					if mockS3Server != nil {
						o.BaseEndpoint = aws.String(mockS3Server.URL)
					}
					o.UsePathStyle = true
				})

				bucketDetails := tt.mockBucket
				if mockS3Server != nil {
					bucketDetails.EndpointURL = mockS3Server.URL
				}

				size, modifiedDate, err := fetchS3MetadataWithBucketDetails(
					ctx,
					tt.s3URL,
					tt.awsAccessKey,
					tt.awsSecretKey,
					tt.region,
					tt.endpoint,
					bucketDetails,
					s3Client,
					&NoOpLogger{},
				)

				if tt.wantErr {
					assert.Error(t, err)
					if tt.errContains != "" {
						assert.Contains(t, err.Error(), tt.errContains)
					}
				} else {
					assert.NoError(t, err)
					assert.Equal(t, tt.wantSize, size)
					assert.Equal(t, tt.wantModified, modifiedDate)
				}
			} else {
				// Test the full fetchS3Metadata function for error cases
				ctx := context.Background()
				_, _, err := fetchS3Metadata(
					ctx,
					tt.s3URL,
					tt.awsAccessKey,
					tt.awsSecretKey,
					tt.region,
					tt.endpoint,
					nil, // Let it create its own S3 client
					&http.Client{},
					&NoOpLogger{},
				)

				if tt.wantErr {
					assert.Error(t, err)
					if tt.errContains != "" {
						assert.Contains(t, err.Error(), tt.errContains)
					}
				}
			}
		})
	}
}

// TestFetchS3Metadata_Integration_ConfigLoading tests config loading edge cases
func TestFetchS3Metadata_Integration_ConfigLoading(t *testing.T) {
	t.Run("no config file", func(t *testing.T) {
		// Setup temporary git repo but don't create config
		tmpDir := testutils.SetupTestGitRepo(t)

		// Remove any existing config
		configPath := filepath.Join(tmpDir, config.DRS_DIR, config.CONFIG_YAML)
		os.RemoveAll(filepath.Dir(configPath))

		ctx := context.Background()
		_, _, err := fetchS3Metadata(
			ctx,
			"s3://test-bucket/sample.bam",
			"test-access-key",
			"test-secret-key",
			"us-west-2",
			"https://s3.amazonaws.com",
			nil,
			&http.Client{},
			&NoOpLogger{},
		)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Unable to get bucket details")
		assert.Contains(t, err.Error(), "config file does not exist")
	})
}

// TestGetBucketDetailsWithAuth_Integration tests the auth integration flow
// This tests getBucketDetailsWithAuth which is the core logic without config loading dependencies
func TestGetBucketDetailsWithAuth_Integration(t *testing.T) {
	tests := []struct {
		name           string
		bucket         string
		authHandler    *mockAuthHandlerForIntegration
		mockServerResp S3BucketsResponse
		serverStatus   int
		wantBucket     S3Bucket
		wantErr        bool
		errContains    string
	}{
		{
			name:   "successful auth and bucket retrieval",
			bucket: "test-bucket",
			authHandler: &mockAuthHandlerForIntegration{
				shouldFail: false,
				token:      "valid-token",
			},
			mockServerResp: S3BucketsResponse{
				S3Buckets: map[string]S3Bucket{
					"test-bucket": {
						Region:      "us-west-2",
						EndpointURL: "https://s3.amazonaws.com",
						Programs:    []string{"test-program"},
					},
				},
			},
			serverStatus: http.StatusOK,
			wantBucket: S3Bucket{
				Region:      "us-west-2",
				EndpointURL: "https://s3.amazonaws.com",
				Programs:    []string{"test-program"},
			},
			wantErr: false,
		},
		{
			name:   "auth handler failure",
			bucket: "test-bucket",
			authHandler: &mockAuthHandlerForIntegration{
				shouldFail: true,
			},
			wantErr:     true,
			errContains: "failed to add authentication",
		},
		{
			name:   "server returns error status",
			bucket: "test-bucket",
			authHandler: &mockAuthHandlerForIntegration{
				shouldFail: false,
				token:      "valid-token",
			},
			serverStatus: http.StatusInternalServerError,
			wantErr:      true,
			errContains:  "unexpected status code: 500",
		},
		{
			name:   "bucket not found in response",
			bucket: "missing-bucket",
			authHandler: &mockAuthHandlerForIntegration{
				shouldFail: false,
				token:      "valid-token",
			},
			mockServerResp: S3BucketsResponse{
				S3Buckets: map[string]S3Bucket{
					"different-bucket": {
						Region:      "us-west-2",
						EndpointURL: "https://s3.amazonaws.com",
						Programs:    []string{"test-program"},
					},
				},
			},
			serverStatus: http.StatusOK,
			wantBucket:   S3Bucket{}, // Empty bucket expected when not found
			wantErr:      false,
		},
		{
			name:   "bucket missing required fields",
			bucket: "incomplete-bucket",
			authHandler: &mockAuthHandlerForIntegration{
				shouldFail: false,
				token:      "valid-token",
			},
			mockServerResp: S3BucketsResponse{
				S3Buckets: map[string]S3Bucket{
					"incomplete-bucket": {
						Region:   "us-west-2",
						Programs: []string{"test-program"},
						// EndpointURL missing
					},
				},
			},
			serverStatus: http.StatusOK,
			wantErr:      true,
			errContains:  "endpoint_url or region not found for bucket",
		},
		{
			name:   "invalid json response",
			bucket: "test-bucket",
			authHandler: &mockAuthHandlerForIntegration{
				shouldFail: false,
				token:      "valid-token",
			},
			serverStatus: http.StatusOK,
			// mockServerResp is empty, will be handled specially in test
			wantErr:     true,
			errContains: "failed to decode bucket information",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock server
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify correct endpoint path
				assert.Equal(t, "/user/data/buckets", r.URL.Path)

				// Verify auth header if auth should succeed
				if tt.authHandler != nil && !tt.authHandler.shouldFail && tt.authHandler.token != "" {
					authHeader := r.Header.Get("Authorization")
					assert.Equal(t, "Bearer "+tt.authHandler.token, authHeader)
				}

				w.WriteHeader(tt.serverStatus)

				if tt.serverStatus == http.StatusOK {
					w.Header().Set("Content-Type", "application/json")

					// Handle invalid JSON case specially
					if tt.errContains == "failed to decode bucket information" {
						w.Write([]byte("invalid json"))
					} else {
						err := json.NewEncoder(w).Encode(tt.mockServerResp)
						require.NoError(t, err)
					}
				}
			}))
			defer mockServer.Close()

			// Test getBucketDetailsWithAuth
			ctx := context.Background()
			result, err := getBucketDetailsWithAuth(
				ctx,
				tt.bucket,
				mockServer.URL+"/user/data/buckets",
				"test-profile",
				tt.authHandler,
				&http.Client{},
			)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantBucket, result)
			}
		})
	}
}
