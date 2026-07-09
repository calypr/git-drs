//go:build integration

package ping

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestIntegrationPingTerraDRSServer(t *testing.T) {
	endpoint := os.Getenv("GIT_DRS_TERRA_DRS_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://data.terra.bio"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	if err := pingTerraServiceInfo(ctx, endpoint); err != nil {
		t.Fatalf("ping Terra DRS service-info endpoint %q: %v", endpoint, err)
	}
}
