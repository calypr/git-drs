package transfer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/drs"
	syclient "github.com/calypr/syfon/client"
)

func BenchmarkBulkResolvedAccessURLs(b *testing.B) {
	for _, count := range []int{7, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			objects := make([]drsapi.DrsObject, count)
			results := make([]drsapi.BulkAccessURL, count)
			for i := range objects {
				id := fmt.Sprintf("object-%d", i)
				accessID := "s3-" + id
				methods := []drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeS3, AccessId: &accessID, AccessUrl: &drsapi.AccessURL{Url: "s3://bucket/" + id}}}
				objects[i] = drsapi.DrsObject{Id: id, AccessMethods: &methods}
				results[i] = drsapi.BulkAccessURL{DrsObjectId: &id, DrsAccessId: &accessID, Url: "https://bucket.s3.amazonaws.com/" + id + "?X-Amz-Signature=" + strings.Repeat("a", 256)}
			}
			body, err := json.Marshal(drsapi.N200OkAccesses{ResolvedDrsObjectAccessUrls: &results})
			if err != nil {
				b.Fatal(err)
			}
			httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/ga4gh/drs/v1/objects/access" {
					return nil, fmt.Errorf("unexpected individual signing request: %s", r.URL.Path)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body))), Header: http.Header{"Content-Type": []string{"application/json"}}, Request: r}, nil
			})}
			client, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
			if err != nil {
				b.Fatal(err)
			}
			ctx := &remoteruntime.GitContext{Client: client}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				access, err := BulkResolvedAccessURLsForObjects(b.Context(), ctx, objects)
				if err != nil || len(access) != count {
					b.Fatalf("resolved %d/%d objects: %v", len(access), count, err)
				}
			}
		})
	}
}
