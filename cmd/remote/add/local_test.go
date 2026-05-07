package add

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddLocalRemote(t *testing.T) {
	assert.NotNil(t, LocalCmd)
	assert.Equal(t, "local <remote-name> <url> <organization/project>", LocalCmd.Use)
	assert.NotNil(t, LocalCmd.Flag("username"))
	assert.NotNil(t, LocalCmd.Flag("password"))
	assert.Nil(t, LocalCmd.Flag("organization"))
	assert.Nil(t, LocalCmd.Flag("project"))
	assert.Nil(t, LocalCmd.Flag("bucket"))
}

func TestResolveBucketScopeFromLocalServer(t *testing.T) {
	t.Run("matches project resource", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/buckets" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			user, pass, ok := r.BasicAuth()
			if !ok || user != "drs-user" || pass != "drs-pass" {
				t.Fatalf("unexpected basic auth: ok=%v user=%q pass=%q", ok, user, pass)
			}
			_, _ = w.Write([]byte(`{"S3_BUCKETS":{"cbds":{"programs":["/organization/calypr/project/end_to_end_test"]}}}`))
		}))
		defer srv.Close()

		scope, err := resolveBucketScopeFromLocalServer(context.Background(), srv.URL, "drs-user", "drs-pass", "calypr", "end_to_end_test")
		if err != nil {
			t.Fatalf("resolveBucketScopeFromLocalServer returned error: %v", err)
		}
		if scope.Bucket != "cbds" {
			t.Fatalf("unexpected bucket: %+v", scope)
		}
	})
}
